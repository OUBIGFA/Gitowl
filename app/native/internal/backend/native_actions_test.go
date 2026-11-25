package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeMenusDiffDiscardTagsAndReopen(t *testing.T) {
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
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "empty.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command(git, args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	profile := filepath.Join(root, "profile")
	k, err := NewNativeKernel(ctx, git, profile, nil)
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
	file := filepath.Join(repo, "lines")
	base := strings.Repeat("context\n", 20) + "old\n" + strings.Repeat("end\n", 10)
	if err := os.WriteFile(file, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CommitIncludedChanges(ctx, ref, KernelCommitContext{Summary: "base"}); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(base, "old\n", "new\n", 1)
	if err := os.WriteFile(file, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := k.RefreshRepository(ctx, ref); err != nil {
		t.Fatal(err)
	}
	result, err := k.SelectFiles(ctx, ref, []string{"lines"})
	if err != nil {
		t.Fatal(err)
	}
	selection := result.Snapshot.Selected.DiffSelection
	if selection == nil || len(selection.Lines) != 2 {
		t.Fatal("diff selection missing")
	}
	result, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "diff.expand", FileID: "lines", Identity: selection.Identity})
	if err != nil {
		t.Fatal(err)
	}
	expanded := result.Snapshot.Workspace.TextDiff.Selection
	if expanded == nil || *expanded.Lines[0].OldBufferLine != 20 || expanded.Lines[0].ID != selection.Lines[0].ID {
		t.Fatal("expanded view changed selected line identity")
	}
	menu := result.Snapshot.Workspace.Menus[0]
	for _, m := range result.Snapshot.Workspace.Menus {
		if m.Target == (KernelWorkspaceMenuTarget{Kind: "file", ID: "lines"}) {
			menu = m
			break
		}
	}
	result, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "menu", Target: menu.Target, Lease: menu.Lease, MenuPath: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.CurrentPopup == nil {
		t.Fatal("discard did not ask for confirmation")
	}
	result, err = k.ReplyPopup(ctx, KernelPopupReply{PopupID: *result.Snapshot.CurrentPopup.ID, Action: "save"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.ReadFile(file)
	if err != nil || string(work) != base {
		t.Fatal("discard did not restore tracked file")
	}
	result, err = k.ChangeSection(ctx, ref, "history")
	if err != nil {
		t.Fatal(err)
	}
	sha := result.Snapshot.Selected.History[0].SHA
	for _, m := range result.Snapshot.Workspace.Menus {
		if m.Target == (KernelWorkspaceMenuTarget{Kind: "commit", ID: sha}) {
			menu = m
			break
		}
	}
	result, err = k.WorkspaceAction(ctx, ref, KernelWorkspaceAction{Type: "menu", Target: menu.Target, Lease: menu.Lease, MenuPath: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	result, err = k.ReplyPopup(ctx, KernelPopupReply{PopupID: *result.Snapshot.CurrentPopup.ID, Action: "save", Values: map[string]any{"name": "v-fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Selected.History[0].Tags) != 1 || result.Snapshot.Selected.History[0].Tags[0] != "v-fixture" {
		t.Fatal("created tag missing from history")
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewNativeKernel(ctx, git, profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.native.snapshot().Selected == nil || reopened.native.snapshot().Selected.Repository.Path != repo {
		t.Fatal("last repository not restored")
	}
}
