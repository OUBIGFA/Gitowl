package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func workspaceFixture() *backend.KernelWorkspace {
	return &backend.KernelWorkspace{
		Repository: &backend.KernelRepositoryReference{ID: 7, Path: "fixture/repository"},
		Branches: []backend.KernelWorkspaceBranchGroup{
			{Group: "default", Items: []backend.KernelWorkspaceBranch{{Ref: "refs/heads/main", Name: "main", Current: true}}},
			{Group: "recent", Items: []backend.KernelWorkspaceBranch{{Ref: "refs/heads/feature", Name: "feature"}}},
			{Group: "other", Items: []backend.KernelWorkspaceBranch{{Ref: "refs/remotes/origin/remote", Name: "origin/remote", Remote: true}}},
		},
		Worktrees:             []backend.KernelWorkspaceWorktree{{Path: "fixture/repository", Name: "repository", Description: "main", Main: true, Current: true}, {Path: "fixture/linked", Name: "linked", Description: "feature", Missing: true}},
		PullRequestsAvailable: true,
		PullRequests:          []backend.KernelWorkspacePullRequest{{Number: 42, Title: "Review native workspace", Author: "fixture-author", Head: "feature", Base: "main", Draft: true, CheckoutAvailable: true}},
		Menus:                 []backend.KernelWorkspaceMenu{{Target: backend.KernelWorkspaceMenuTarget{Kind: "branch", ID: "refs/heads/feature"}, Lease: "opaque-current-lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.rename", Enabled: true}, {LabelKey: "workspace.delete", Enabled: false}}}},
	}
}

func workspaceControlsTester(state *nativeWorkspaceState, workspace *backend.KernelWorkspace, locale string, dark bool, actions *[]backend.KernelWorkspaceAction) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Padding(24).Children(func() {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				renderWorkspaceTitleControls(c, state, workspace, "main", false, func(action backend.KernelWorkspaceAction) { *actions = append(*actions, action) })
			})
		})
	}, 900, 620)
	tt.SetLocale(locale)
	tt.SetDark(dark)
	tt.Advance(time.Second)
	return tt
}

func TestWorkspaceOriginalFoldoutStateControlsNativePopovers(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	workspace := workspaceFixture()
	workspace.Navigation = "worktrees"
	a.applyWorkspaceSnapshot(backend.KernelSnapshot{Workspace: workspace}, false)
	var actions []backend.KernelWorkspaceAction
	tt := workspaceControlsTester(&a.nativeWorkspaceState, workspace, "en", false, &actions)
	if !tt.HasText(workspaceLabel("en", "workspace.searchWorktrees")) {
		t.Fatal("original Worktree foldout did not open native UI")
	}
	workspace.Navigation = "branches"
	a.applyWorkspaceSnapshot(backend.KernelSnapshot{Workspace: workspace}, false)
	tt.Frame()
	if !tt.HasText(workspaceLabel("en", "workspace.searchBranches")) || tt.HasText(workspaceLabel("en", "workspace.searchWorktrees")) {
		t.Fatal("original Branch foldout did not replace Worktree")
	}
	a.workspacePending = 1
	workspace.Navigation = ""
	a.applyWorkspaceSnapshot(backend.KernelSnapshot{Workspace: workspace}, false)
	tt.Frame()
	if !a.workspaceBranchOpen {
		t.Fatal("an intermediate response erased an outstanding native navigation intent")
	}
	a.workspacePending = 0
	a.applyWorkspaceSnapshot(backend.KernelSnapshot{Workspace: workspace}, false)
	tt.Frame()
	if a.workspaceBranchOpen || tt.HasText(workspaceLabel("en", "workspace.searchBranches")) {
		t.Fatal("original foldout close did not close native UI")
	}
}

func TestWorkspaceNativeBranchWorktreeAndPRIntents(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			state := &nativeWorkspaceState{}
			var actions []backend.KernelWorkspaceAction
			tt := workspaceControlsTester(state, workspaceFixture(), locale, false, &actions)
			if err := tt.Click(workspaceLabel(locale, "workspace.branches")); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(workspaceLabel(locale, "workspace.recent")) {
				t.Fatal("original recent-branch group is absent")
			}
			if err := tt.Click("feature"); err != nil {
				t.Fatal(err)
			}
			if len(actions) != 2 || actions[0].Type != "navigation.open" || actions[0].Option != "branches" || actions[1].Type != "branch.checkout" || actions[1].Ref != "refs/heads/feature" {
				t.Fatalf("wrong branch intent: %+v", actions)
			}
			if err := tt.Click(workspaceLabel(locale, "workspace.worktrees")); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(workspaceLabel(locale, "workspace.missing")) {
				t.Fatal("missing worktree is misleadingly presented as available")
			}
			if err := tt.Click("linked"); err != nil {
				t.Fatal(err)
			}
			if len(actions) != 4 || actions[2].Type != "navigation.open" || actions[2].Option != "worktrees" || actions[3].Type != "worktree.select" || actions[3].Path != "fixture/linked" {
				t.Fatalf("missing worktree bypassed original request-delete path: %+v", actions)
			}
			if err := tt.Click(workspaceLabel(locale, "workspace.pullRequests")); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("Review native workspace"); err != nil {
				t.Fatal(err)
			}
			if len(actions) != 6 || actions[4].Type != "navigation.open" || actions[4].Option != "pullRequests" || actions[5].Type != "pullRequest.checkout" || actions[5].Number != 42 {
				t.Fatalf("wrong PR identity: %+v", actions)
			}
		})
	}
}

func TestWorkspaceNativeMenusCarryLeaseAndRetainDisabledRareActions(t *testing.T) {
	state := &nativeWorkspaceState{}
	var actions []backend.KernelWorkspaceAction
	tt := workspaceControlsTester(state, workspaceFixture(), "en", false, &actions)
	if err := tt.Click(workspaceLabel("en", "workspace.branches")); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("feature"); err != nil {
		t.Fatal(err)
	}
	label := workspaceLabel("en", "workspace.delete")
	entry, ok := tt.AccessByRole(ui.RoleMenuItem, label)
	if !ok || !entry.Disabled {
		t.Fatalf("disabled original delete entry missing or actionable: %+v", entry)
	}
	if err := tt.ChooseMenuItem(workspaceLabel("en", "workspace.rename")); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[1].Type != "menu" || actions[1].Lease != "opaque-current-lease" || len(actions[1].MenuPath) != 1 || actions[1].MenuPath[0] != 0 {
		t.Fatalf("menu lost its original lease/path: %+v", actions)
	}
}

func TestWorkspaceNativePopoverKeyboardLanguageAndLayoutMatrix(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", locale, dark), func(t *testing.T) {
				state := &nativeWorkspaceState{}
				var actions []backend.KernelWorkspaceAction
				fixture := workspaceFixture()
				fixture.Branches[2].Items[0].Name = strings.Repeat("long-branch-", 20)
				tt := workspaceControlsTester(state, fixture, locale, dark, &actions)
				for _, key := range []string{"workspace.branches", "workspace.worktrees", "workspace.pullRequests"} {
					label := workspaceLabel(locale, key)
					if label == key || strings.HasPrefix(label, "dialogLabel_") {
						t.Fatalf("missing workspace translation: %s", key)
					}
					visibleControl(t, tt, label, 900, 620)
				}
				if err := tt.Click(workspaceLabel(locale, "workspace.branches")); err != nil {
					t.Fatal(err)
				}
				visibleControl(t, tt, workspaceLabel(locale, "workspace.searchBranches"), 900, 620)
				visibleControl(t, tt, workspaceLabel(locale, "workspace.newBranch"), 900, 620)
				saveWorkspaceControlImage(t, tt, fmt.Sprintf("workspace-branches-%s-%v.png", locale, dark))
				if err := tt.Click(workspaceLabel(locale, "workspace.searchBranches")); err != nil {
					t.Fatal(err)
				}
				tt.Type("feat")
				if len(actions) == 0 || actions[len(actions)-1].Type != "navigation.filter" {
					t.Fatal("filter bypasses original fuzzy matching")
				}
				tt.Key(0, ui.KeyEscape)
				tt.Advance(time.Millisecond * 200)
				if state.workspaceBranchOpen {
					t.Fatal("Escape did not dismiss the native popover")
				}
				if actions[len(actions)-1].Type != "navigation.close" || actions[len(actions)-1].Option != "branches" {
					t.Fatal("Escape did not close the original foldout")
				}
				saveWorkspaceControlImage(t, tt, fmt.Sprintf("workspace-controls-%s-%v.png", locale, dark))
			})
		}
	}
}

// Real production primitive render, not a desktop acceptance screenshot.
func saveWorkspaceControlImage(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if dir := os.Getenv("GITOWL_NATIVE_UI_ARTIFACTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, tt.Image())
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

func TestWorkspaceNativeBannerDeadlineAndConflictActions(t *testing.T) {
	state := &nativeWorkspaceState{}
	var actions []backend.KernelWorkspaceAction
	banner := &backend.KernelWorkspaceBanner{ID: 8, Type: "SuccessfulSquash", LabelKey: "workspace.banner.SuccessfulSquash", Data: []string{"2"}, Actions: []string{"undo", "dismiss"}, Timeout: 15000, Dismissable: true}
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() {
			renderWorkspaceBanner(c, state, banner, func(action backend.KernelWorkspaceAction) { actions = append(actions, action) })
			ui.Button(c, "Outside banner")
		})
	}, 900, 620)
	tt.Advance(16 * time.Second)
	if len(actions) != 0 {
		t.Fatal("focused banner must not auto-dismiss")
	}
	if !tt.Focused(workspaceLabel("en", "workspace.banner.undo")) {
		t.Fatal("banner did not focus its first original action")
	}
	if err := tt.Click("Outside banner"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(14 * time.Second)
	if len(actions) != 0 {
		t.Fatal("undo window dismissed too early")
	}
	tt.Advance(2 * time.Second)
	if len(actions) != 1 || actions[0].ID != 8 || actions[0].Action != "dismiss" {
		t.Fatalf("banner timeout did not use its original identity: %+v", actions)
	}
	banner = &backend.KernelWorkspaceBanner{ID: 9, Type: "ConflictsFound", LabelKey: "workspace.banner.ConflictsFound", Actions: []string{"open"}}
	tt.Frame()
	tt.Advance(time.Minute)
	if len(actions) != 1 {
		t.Fatal("unresolved conflicts auto-dismissed")
	}
	if tt.HasText(workspaceLabel("en", "workspace.banner.dismiss")) {
		t.Fatal("non-dismissable conflict exposes generic cancel")
	}
	if err := tt.Click(workspaceLabel("en", "workspace.banner.open")); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[1].ID != 9 || actions[1].Action != "open" {
		t.Fatal("conflict did not reopen the original continuation")
	}
}
