package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeGitSyncBranchStashAndConflictRecovery(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = path
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	run(repo, "init", "-b", "main")
	run(repo, "config", "user.name", "Fixture")
	run(repo, "config", "user.email", "fixture@example.invalid")
	file := filepath.Join(repo, "file.txt")
	if err := os.WriteFile(file, []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", ".")
	run(repo, "commit", "-m", "base")
	remote := filepath.Join(root, "remote.git")
	run(root, "clone", "--bare", repo, remote)
	run(repo, "remote", "add", "origin", remote)
	run(repo, "fetch", "origin")
	run(repo, "branch", "--set-upstream-to=origin/main", "main")
	k, err := NewNativeKernel(ctx, git, filepath.Join(root, "profile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	useNativeTestRecycle(t, k)
	added, err := k.AddRepositories(ctx, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	ref := added.AddedRepositories[0]
	if _, err := k.SelectRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Fetch(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Pull(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("edited\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	result, err := k.CommitIncludedChanges(ctx, ref, KernelCommitContext{Summary: "pushed edit", PushAfterCommit: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Committed == nil || !*result.Committed {
		t.Fatal("commit not confirmed")
	}
	if run(repo, "rev-parse", "HEAD") != run(remote, "rev-parse", "refs/heads/main") {
		t.Fatal("save-and-push did not push saved commit")
	}
	if err := os.WriteFile(file, []byte("stashed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "stash.create"})
	if err != nil || result.Snapshot.Workspace.Stash == nil {
		t.Fatalf("stash %v", err)
	}
	sha := result.Snapshot.Workspace.Stash.SHA
	if _, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "stash.view"}); err != nil {
		t.Fatal(err)
	}
	if _, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "stash.restore", SHA: sha}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); string(data) != "stashed\n" {
		t.Fatalf("stash content not restored: %q", data)
	}
	run(repo, "restore", "--", "file.txt")
	run(repo, "switch", "-c", "other")
	if err := os.WriteFile(file, []byte("other branch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(repo, "commit", "-am", "other")
	run(repo, "switch", "main")
	if err := os.WriteFile(file, []byte("main branch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run(repo, "commit", "-am", "main change")
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "compare.branch", Ref: "refs/heads/other", Mode: "behind"}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "compare.merge"}); err == nil {
		t.Fatal("conflict reported as success")
	}
	result, err = k.RefreshRepository(ctx, ref)
	if err != nil || result.Snapshot.Workspace.Conflict == nil {
		t.Fatalf("conflict state missing %v", err)
	}
	if _, err := k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "conflicts.abort", Identity: result.Snapshot.Workspace.Conflict.Identity}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); string(data) != "main branch\n" {
		t.Fatal("abort did not restore main branch worktree")
	}
}
