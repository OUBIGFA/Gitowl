package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestNativeOwnerGitWorkflowWithoutNode(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
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
	if err := os.WriteFile(filepath.Join(repo, "中文.txt"), []byte("contents\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := k.RefreshRepository(ctx, ref)
	if err != nil || len(state.Snapshot.Selected.Files) != 1 {
		t.Fatalf("refresh %+v %v", state, err)
	}
	state, err = k.SetMessage(ctx, ref, KernelMessage{Summary: "Create file"})
	if err != nil || !state.Snapshot.Selected.CommitView.CanCommit {
		t.Fatalf("message %v", err)
	}
	state, err = k.CommitIncludedChanges(ctx, ref, KernelCommitContext{Summary: "Create file"})
	if err != nil || state.Committed == nil || !*state.Committed {
		t.Fatalf("commit %v", err)
	}
	state, err = k.ChangeSection(ctx, ref, "history")
	if err != nil || len(state.Snapshot.Selected.History) != 1 {
		t.Fatalf("history %v", err)
	}
	sha := state.Snapshot.Selected.History[0].SHA
	state, err = k.SelectHistory(ctx, ref, []string{sha}, true)
	if err != nil || len(state.Snapshot.Selected.HistoryFiles) != 1 {
		t.Fatalf("files %v", err)
	}
	state, err = k.SelectHistoryFile(ctx, ref, "中文.txt")
	if err != nil || state.Snapshot.Selected.Diff == nil {
		t.Fatalf("diff %v", err)
	}
	if k.client != nil || k.service != nil {
		t.Fatal("native owner launched a service")
	}
}

// Unit tests retain recycled data in an isolated directory. The real Windows
// operation is exercised by the private-desktop acceptance host.
func useNativeTestRecycle(t *testing.T, k *Kernel) {
	t.Helper()
	retired := t.TempDir()
	count := 0
	k.native.recycle = func(path string) error { count++; return os.Rename(path, filepath.Join(retired, fmt.Sprint(count))) }
}

func TestNativeRefreshDoesNotPublishUnchangedState(t *testing.T) {
	ctx := context.Background()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(git, "init", "-b", "main")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	var pushes atomic.Int64
	k, err := NewNativeKernel(ctx, git, filepath.Join(root, "profile"), func(event KernelEvent) {
		if event.Snapshot != nil {
			pushes.Add(1)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	added, err := k.AddRepositories(ctx, []string{repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.SelectRepository(ctx, added.AddedRepositories[0]); err != nil {
		t.Fatal(err)
	}
	k.native.mu.Lock()
	defer k.native.mu.Unlock()
	k.native.watchCancel()
	baseline := pushes.Load()
	commands := k.native.git.Stats().Commands
	for i := 0; i < 5; i++ {
		changed, err := k.native.refreshAndPublish(ctx, false)
		if err != nil || changed {
			t.Fatalf("unchanged %v %v", changed, err)
		}
	}
	if pushes.Load() != baseline {
		t.Fatal("unchanged state republished")
	}
	if got := k.native.git.Stats().Commands - commands; got != 5 {
		t.Fatalf("unchanged refresh repeated metadata/diff reads: %d", got)
	}
	file := filepath.Join(repo, "edit.txt")
	if err := os.WriteFile(file, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := k.native.refreshAndPublish(ctx, false)
	if err != nil || !changed {
		t.Fatalf("new file %v %v", changed, err)
	}
	before := k.native.status.Fingerprint
	if err := os.WriteFile(file, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := k.native.refreshAndPublish(ctx, false); err != nil {
		t.Fatal(err)
	}
	if k.native.status.Fingerprint == before {
		t.Fatal("same-sized edit to already modified file was missed")
	}
}
