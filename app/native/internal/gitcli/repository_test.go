package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) (*Engine, *Repository) {
	t.Helper()
	git := os.Getenv("GITOWL_TEST_GIT")
	if git == "" {
		var err error
		git, err = exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, "empty.gitconfig"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "Fixture")
	run("config", "user.email", "fixture@example.invalid")
	engine := New(git, 1024)
	repo, err := engine.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	repo.Environment = []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(dir, "empty.gitconfig")}
	return engine, repo
}

func TestConcurrentImmutableReadsShareOneGitRead(t *testing.T) {
	e, r := fixture(t)
	e.cache = newCache(1 << 20)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(r.Path, "shared.txt"), []byte(strings.Repeat("immutable", 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit(ctx, []string{"shared.txt"}, "first", false, false)
	if err != nil {
		t.Fatal(err)
	}
	before := e.Stats().Commands
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			data, err := r.Blob(ctx, sha, "shared.txt", 0)
			if err != nil || len(data) != 9000 {
				t.Errorf("read %d %v", len(data), err)
			}
		}()
	}
	group.Wait()
	if got := e.Stats().Commands - before; got != 2 {
		t.Fatalf("expected one ID resolution and one content read, got %d", got)
	}
}

func TestCommitLeavesUnselectedStagingAndExternalChanges(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"chosen", "staged"} {
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte("base"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Commit(ctx, []string{"chosen", "staged"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "staged"), []byte("stage version"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, nil, "add", "--", "staged"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "staged"), []byte("work version"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "chosen"), []byte("selected edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"chosen"}, "selected", false, false); err != nil {
		t.Fatal(err)
	}
	index, err := r.Run(ctx, nil, "show", ":staged")
	if err != nil || string(index) != "stage version" {
		t.Fatalf("staged bytes changed %q %v", index, err)
	}
	head, err := r.Blob(ctx, "HEAD", "staged", 0)
	if err != nil || string(head) != "base" {
		t.Fatalf("unselected staged file committed %q %v", head, err)
	}
	work, err := os.ReadFile(filepath.Join(r.Path, "staged"))
	if err != nil || string(work) != "work version" {
		t.Fatal("unselected working change lost")
	}
}

func TestPartialCommitRetainsUnselectedLinesAndIndex(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	path := filepath.Join(r.Path, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"lines.txt"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("ONE\ntwo\nTHREE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	patch, err := r.Diff(ctx, "lines.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	identity, lines := PatchLines(patch)
	selected := make(map[int]bool)
	for _, line := range lines {
		if line.Old != nil && *line.Old == 0 || line.New != nil && *line.New == 0 {
			selected[line.ID] = true
		}
	}
	sha, err := r.CommitSelected(ctx, []FileSelection{{Path: "lines.txt", Identity: identity, Patch: patch, Lines: selected}}, "partial", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	content, err := r.Blob(ctx, sha, "lines.txt", 0)
	if err != nil || string(content) != "ONE\ntwo\nthree\n" {
		t.Fatalf("partial contents %q %v", content, err)
	}
	work, err := os.ReadFile(path)
	if err != nil || string(work) != "ONE\ntwo\nTHREE\n" {
		t.Fatal("working file modified")
	}
	status, err := r.Status(ctx)
	if err != nil || len(status.Files) != 1 {
		t.Fatalf("remaining changes %+v %v", status, err)
	}
}

func TestSelectedCommitIncludesBothSidesOfRename(t *testing.T) {
	_, r := fixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(r.Path, "old"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"old"}, "base", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, nil, "mv", "old", "new"); err != nil {
		t.Fatal(err)
	}
	sha, err := r.CommitSelected(ctx, []FileSelection{{Path: "new", OldPath: "old"}}, "rename", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Run(ctx, nil, "ls-tree", "--name-only", sha)
	if err != nil || string(files) != "new\n" {
		t.Fatalf("rename tree %q %v", files, err)
	}
	status, err := r.Status(ctx)
	if err != nil || len(status.Files) != 0 {
		t.Fatalf("rename index %+v %v", status, err)
	}
}

func TestDirectGitReadsObjectIDsAndChanges(t *testing.T) {
	e, r := fixture(t)
	ctx := context.Background()
	path := "中文 space.txt"
	if err := os.WriteFile(filepath.Join(r.Path, path), []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, err := r.Status(ctx)
	if err != nil || len(status.Files) != 1 || status.Files[0].Path != path {
		t.Fatalf("status %+v %v", status, err)
	}
	sha, err := r.Commit(ctx, []string{path}, "initial", false, false)
	if err != nil || len(sha) != 40 {
		t.Fatalf("commit %q %v", sha, err)
	}
	first, err := r.Blob(ctx, sha, path, 0)
	if err != nil || string(first) != "first\n" {
		t.Fatalf("blob %q %v", first, err)
	}
	before := e.Stats()
	first[0] = 'X'
	again, err := r.Blob(ctx, sha, path, 0)
	if err != nil || string(again) != "first\n" || e.Stats().Commands != before.Commands {
		t.Fatalf("immutable cache %q %v %+v", again, err, e.Stats())
	}
	if err := os.WriteFile(filepath.Join(r.Path, path), []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sha2, err := r.Commit(ctx, []string{path}, "second", false, false)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.Blob(ctx, "HEAD", path, 0)
	if err != nil || string(current) != "second\n" {
		t.Fatalf("moved HEAD stale %q %v", current, err)
	}
	history, err := r.History(ctx, "HEAD", 0, 20)
	if err != nil || len(history) != 2 || history[0].ID != sha2 {
		t.Fatalf("history %+v %v", history, err)
	}
	status, err = r.Status(ctx)
	if err != nil || len(status.Files) != 0 {
		t.Fatalf("clean status %+v %v", status, err)
	}
}

func TestSharedCacheBudgetAndHooksPreserveFailedChanges(t *testing.T) {
	e, r := fixture(t)
	ctx := context.Background()
	for n := 0; n < 6; n++ {
		name := string(rune('a'+n)) + ".txt"
		if err := os.WriteFile(filepath.Join(r.Path, name), []byte(strings.Repeat(name, 150)), 0600); err != nil {
			t.Fatal(err)
		}
		sha, err := r.Commit(ctx, []string{name}, name, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Blob(ctx, sha, name, 0); err != nil {
			t.Fatal(err)
		}
		if e.Stats().CacheBytes > 1024 {
			t.Fatalf("cache budget exceeded %+v", e.Stats())
		}
	}
	other := t.TempDir()
	if out, err := exec.Command(e.git, "clone", "--no-hardlinks", r.Path, other).CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	r2, err := e.Open(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r2.Blob(ctx, "HEAD", "f.txt", 0); err != nil {
		t.Fatal(err)
	}
	if e.Stats().CacheBytes > 1024 {
		t.Fatal("per-repository cache exceeded shared cap")
	}
	if err := os.WriteFile(filepath.Join(r.GitDir, "hooks/pre-commit"), []byte("#!/bin/sh\necho fixture-hook-blocked >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.Path, "blocked.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"blocked.txt"}, "must fail", false, false); err == nil || !strings.Contains(err.Error(), "fixture-hook-blocked") {
		t.Fatalf("hook ignored: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(r.Path, "blocked.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatal("failed commit lost file")
	}
}

func TestStatusRenameAndLinkedWorktree(t *testing.T) {
	e, r := fixture(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(r.Path, "original.txt"), []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, []string{"original.txt"}, "first", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx, nil, "mv", "original.txt", "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	s, err := r.Status(ctx)
	if err != nil || len(s.Files) != 1 || s.Files[0].OldPath != "original.txt" || s.Files[0].Path != "renamed.txt" {
		t.Fatalf("rename %+v %v", s, err)
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	if _, err := r.Run(ctx, nil, "worktree", "add", "-b", "other", worktree); err != nil {
		t.Fatal(err)
	}
	linked, err := e.Open(ctx, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if linked.GitDir == r.GitDir || linked.CommonDir != r.CommonDir {
		t.Fatalf("worktree identity %+v %+v", r, linked)
	}
}
