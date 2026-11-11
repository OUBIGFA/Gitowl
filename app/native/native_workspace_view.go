package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func (a *app) workspaceTitleControls(c *ui.Context) {
	workspace := a.nativeWorkspace()
	if workspace == nil || workspace.Repository == nil {
		return
	}
	branch := workspaceLabel(c.Locale(), "workspace.detached")
	if a.coreSnapshot.Selected != nil && a.coreSnapshot.Selected.Branch != nil {
		branch = *a.coreSnapshot.Selected.Branch
	}
	renderWorkspaceTitleControls(c, &a.nativeWorkspaceState, workspace, branch, !a.ready || a.workspacePending > 0, a.workspaceAction)
}

// Open/close go through the original foldout state too, so App menu actions and
// shortcuts address these same native popovers rather than an invisible foldout.
func toggleWorkspaceNavigation(state *nativeWorkspaceState, target string, send func(backend.KernelWorkspaceAction)) {
	open := false
	switch target {
	case "branches":
		open = !state.workspaceBranchOpen
	case "worktrees":
		open = !state.workspaceWorktreeOpen
	case "pullRequests":
		open = !state.workspacePROpen
	}
	state.workspaceBranchOpen, state.workspaceWorktreeOpen, state.workspacePROpen = false, false, false
	kind := "navigation.close"
	if open {
		kind = "navigation.open"
		switch target {
		case "branches":
			state.workspaceBranchOpen = true
			state.workspaceBranchFilter = ""
		case "worktrees":
			state.workspaceWorktreeOpen = true
			state.workspaceWorktreeFilter = ""
		case "pullRequests":
			state.workspacePROpen = true
			state.workspacePRFilter = ""
		}
	}
	send(backend.KernelWorkspaceAction{Type: kind, Option: target})
}

func renderWorkspaceTitleControls(c *ui.Context, state *nativeWorkspaceState, workspace *backend.KernelWorkspace, branch string, busy bool, send func(backend.KernelWorkspaceAction)) {
	label := func(key string) string { return workspaceLabel(c.Locale(), "workspace."+key) }
	disabled := busy || workspace.Locked || workspace.CheckoutInProgress
	branchAnchor := ui.ButtonBase(c.Key("workspace-branches")).Height(32).MaxWidth(200).Padding(6, 8).Gap(8).Label(label("branches")).Tooltip(branch).Disabled(disabled)
	quietSurface(c, branchAnchor).Children(func() {
		ui.Icon(c, icons["git-branch"]).FontSize(18).Shrink(0)
		ui.Text(c, branch).SingleLine().MinWidth(0).Grow(1)
		ui.Icon(c, icons["chevron-down"]).FontSize(14).Shrink(0)
	}).OnClick(func() { toggleWorkspaceNavigation(state, "branches", send) })
	branchAnchor.PopoverMenu(func(m *ui.Menu) {
		if workspace.CurrentBranchRef != nil {
			renderWorkspaceMenu(m, c.Locale(), workspaceMenu(workspace, "branch", *workspace.CurrentBranchRef), send)
		}
	})
	if ui.Popover(c.Key("workspace-branch-panel"), branchAnchor, &state.workspaceBranchOpen, func() {
		ui.TextInput(c.Key("branch-filter"), &state.workspaceBranchFilter).Label(label("searchBranches")).Placeholder(label("searchBranches")).FillWidth().OnChange(func() {
			send(backend.KernelWorkspaceAction{Type: "navigation.filter", Option: "branches", Path: state.workspaceBranchFilter})
		})
		type row struct {
			group  string
			branch *backend.KernelWorkspaceBranch
		}
		var rows []row
		for _, group := range workspace.Branches {
			if len(group.Items) == 0 {
				continue
			}
			rows = append(rows, row{group: group.Group})
			for i := range group.Items {
				b := group.Items[i]
				rows = append(rows, row{branch: &b})
			}
		}
		state.workspaceBranchList.list().Key = func(i int) any {
			if rows[i].branch == nil {
				return rows[i].group
			}
			return rows[i].branch.Ref
		}
		state.workspaceBranchList.list().Header = func(i int) bool { return rows[i].branch == nil }
		ui.List(c.Key("workspace-branch-list"), state.workspaceBranchList.list(), len(rows), func(i int) {
			r := rows[i]
			if r.branch == nil {
				ui.Text(c, label(r.group)).FontSize(c.Theme().Rem(14.0/15)).TextColor(c.Theme().TextMuted).Padding(12, 8, 4, 8)
				return
			}
			b := *r.branch
			button := ui.ButtonBase(c.Key(b.Ref)).Height(36).FillWidth().Padding(6, 8).Gap(8).Label(b.Name).Tooltip(b.Name).Disabled(disabled)
			quietSurface(c, button).Children(func() {
				if b.Current {
					ui.Icon(c, icons["check"]).FontSize(16)
				}
				ui.Text(c, b.Name).SingleLine().Grow(1).MinWidth(0)
				if b.Remote {
					ui.Text(c, label("remote")).TextColor(c.Theme().TextMuted).FontSize(c.Theme().Rem(14.0 / 15))
				}
			}).OnClick(func() {
				state.workspaceBranchOpen = false
				send(backend.KernelWorkspaceAction{Type: "branch.checkout", Ref: b.Ref})
			})
			menu := workspaceMenu(workspace, "branch", b.Ref)
			button.PopoverMenu(func(m *ui.Menu) { renderWorkspaceMenu(m, c.Locale(), menu, send) })
		}).Height(280).FillWidth().Children(func() {
			if len(rows) == 0 {
				ui.Text(c, label("empty")).Padding(12)
			}
		})
		quietButton(c.Key("workspace-new-branch"), label("newBranch"), "plus").FillWidth().Disabled(disabled).OnClick(func() {
			state.workspaceBranchOpen = false
			send(backend.KernelWorkspaceAction{Type: "branch.create", InitialName: state.workspaceBranchFilter})
		})
	}).Width(380).Padding(12).Gap(8).Dismissed() {
		send(backend.KernelWorkspaceAction{Type: "navigation.close", Option: "branches"})
	}
	treeAnchor := quietButton(c.Key("workspace-worktrees"), label("worktrees"), "folder").Height(32).Disabled(disabled).OnClick(func() { toggleWorkspaceNavigation(state, "worktrees", send) })
	treeAnchor.PopoverMenu(func(m *ui.Menu) {
		if m.Item(label("newWorktree")).Chosen() {
			send(backend.KernelWorkspaceAction{Type: "worktree.create"})
		}
		m.Separator()
		if workspace.Repository != nil {
			renderWorkspaceMenu(m, c.Locale(), workspaceMenu(workspace, "worktree", workspace.Repository.Path), send)
		}
	})
	if ui.Popover(c.Key("workspace-worktree-panel"), treeAnchor, &state.workspaceWorktreeOpen, func() {
		ui.TextInput(c.Key("worktree-filter"), &state.workspaceWorktreeFilter).Label(label("searchWorktrees")).Placeholder(label("searchWorktrees")).FillWidth().OnChange(func() {
			send(backend.KernelWorkspaceAction{Type: "navigation.filter", Option: "worktrees", Path: state.workspaceWorktreeFilter})
		})
		state.workspaceTreeList.list().Key = func(i int) any { return workspace.Worktrees[i].Path }
		ui.List(c.Key("workspace-worktree-list"), state.workspaceTreeList.list(), len(workspace.Worktrees), func(i int) {
			w := workspace.Worktrees[i]
			button := ui.ButtonBase(c.Key(w.Path)).MinHeight(54).FillWidth().Padding(8).Gap(8).Label(w.Name).Tooltip(w.Path).Disabled(disabled)
			quietSurface(c, button).Children(func() {
				if w.Current {
					ui.Icon(c, icons["check"]).FontSize(16)
				}
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, w.Name).SingleLine()
					ui.Text(c, w.Description).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
					if w.Missing {
						ui.Text(c, label("missing")).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Warning)
					}
				})
				if w.Main {
					ui.Text(c, label("main")).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
				}
			}).OnClick(func() {
				state.workspaceWorktreeOpen = false
				send(backend.KernelWorkspaceAction{Type: "worktree.select", Path: w.Path})
			})
			button.PopoverMenu(func(m *ui.Menu) {
				renderWorkspaceMenu(m, c.Locale(), workspaceMenu(workspace, "worktree", w.Path), send)
			})
		}).Height(280).FillWidth().Children(func() {
			if len(workspace.Worktrees) == 0 {
				ui.Text(c, label("empty")).Padding(12)
			}
		})
		quietButton(c.Key("workspace-new-worktree"), label("newWorktree"), "plus").FillWidth().Disabled(disabled).OnClick(func() {
			state.workspaceWorktreeOpen = false
			send(backend.KernelWorkspaceAction{Type: "worktree.create"})
		})
	}).Width(400).Padding(12).Gap(8).Dismissed() {
		send(backend.KernelWorkspaceAction{Type: "navigation.close", Option: "worktrees"})
	}
	if !workspace.PullRequestsAvailable {
		return
	}
	prAnchor := quietButton(c.Key("workspace-prs"), label("pullRequests"), "").Height(32).Disabled(busy).OnClick(func() { toggleWorkspaceNavigation(state, "pullRequests", send) })
	if ui.Popover(c.Key("workspace-pr-panel"), prAnchor, &state.workspacePROpen, func() {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.TextInput(c.Key("pr-filter"), &state.workspacePRFilter).Label(label("searchPRs")).Placeholder(label("searchPRs")).Grow(1).MinWidth(0).OnChange(func() {
				send(backend.KernelWorkspaceAction{Type: "navigation.filter", Option: "pullRequests", Path: state.workspacePRFilter})
			})
			iconButton(c.Key("refresh-prs"), label("refreshPRs"), "refresh-cw").Disabled(workspace.LoadingPullRequests || busy).OnClick(func() { send(backend.KernelWorkspaceAction{Type: "pullRequest.refresh"}) })
		})
		if workspace.LoadingPullRequests {
			ui.Text(c, label("loadingPRs")).Role(ui.RoleStatus).FontSize(c.Theme().Rem(14.0 / 15))
		}
		state.workspacePRList.list().Key = func(i int) any { return workspace.PullRequests[i].Number }
		ui.List(c.Key("workspace-pr-list"), state.workspacePRList.list(), len(workspace.PullRequests), func(i int) {
			pr := workspace.PullRequests[i]
			button := ui.ButtonBase(c.Key(pr.Number)).MinHeight(64).FillWidth().Padding(8).Label(pr.Title).Tooltip(pr.Title).Disabled(disabled || !pr.CheckoutAvailable)
			quietSurface(c, button).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, pr.Title).SingleLine()
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, fmt.Sprintf("#%d · %s", pr.Number, pr.Author)).SingleLine().Grow(1).MinWidth(0).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
						if pr.Draft {
							ui.Text(c, label("draft")).FontSize(c.Theme().Rem(14.0 / 15))
						}
					})
				})
			}).OnClick(func() {
				state.workspacePROpen = false
				send(backend.KernelWorkspaceAction{Type: "pullRequest.checkout", Number: pr.Number})
			})
			button.PopoverMenu(func(m *ui.Menu) {
				renderWorkspaceMenu(m, c.Locale(), workspaceMenu(workspace, "pullRequest", fmt.Sprint(pr.Number)), send)
			})
		}).Height(320).FillWidth().Children(func() {
			if len(workspace.PullRequests) == 0 && !workspace.LoadingPullRequests {
				ui.Text(c, label("empty")).Padding(12)
			}
		})
	}).Width(440).Padding(12).Gap(8).Dismissed() {
		send(backend.KernelWorkspaceAction{Type: "navigation.close", Option: "pullRequests"})
	}
}

func (a *app) workspaceOverlayView(c *ui.Context) {
	workspace := a.nativeWorkspace()
	if workspace == nil {
		return
	}
	if a.workspaceErr != nil {
		showError(c, a.workspaceErr.Error())
	}
	if workspace.CheckoutInProgress {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.checkoutBusy")).Role(ui.RoleStatus).TextColor(c.Theme().TextMuted).Margin(0, 0, 12, 0)
	}
	renderWorkspaceBanner(c, &a.nativeWorkspaceState, workspace.Banner, a.workspaceAction)
}

func renderWorkspaceBanner(c *ui.Context, state *nativeWorkspaceState, banner *backend.KernelWorkspaceBanner, send func(backend.KernelWorkspaceAction)) {
	if banner == nil {
		state.workspaceBannerID = 0
		return
	}
	if state.workspaceBannerID != banner.ID {
		state.workspaceBannerID = banner.ID
		state.workspaceBannerStarted = c.Now()
		state.workspaceBannerDismissPending = false
		state.workspaceBannerFocusPlaced, state.workspaceBannerFocused = false, false
		state.workspaceBannerDismissAt = time.Time{}
	}
	// Original Banner focuses its first action after 200ms. Its timeout begins
	// only after focus leaves and is cancelled on focus-in, not on mount.
	var focusTarget ui.Element
	row := ui.Row(c.Key("workspace-banner")).FillWidth().Gap(12).Wrap().Padding(10, 12).Margin(0, 0, 12, 0).Background(c.Theme().Surface).Radius(8).Role(ui.RoleStatus).Children(func() {
		ui.Text(c, workspaceLabel(c.Locale(), banner.LabelKey, banner.Data...)).Grow(1).MinWidth(160)
		for index, action := range banner.Actions {
			key := action
			if action == "open" && banner.Type == "OpenThankYouCard" {
				key = "openCard"
			}
			button := quietButton(c.Key(action), workspaceLabel(c.Locale(), "workspace.banner."+key), "").Disabled(state.workspaceBannerDismissPending).OnClick(func() { send(backend.KernelWorkspaceAction{Type: "banner", ID: banner.ID, Action: action}) })
			if index == 0 {
				focusTarget = button
			}
		}
	})
	if !state.workspaceBannerFocusPlaced && len(banner.Actions) > 0 {
		remaining := 200*time.Millisecond - c.Now().Sub(state.workspaceBannerStarted)
		if remaining > 0 {
			c.After(remaining)
		} else {
			focusTarget.Focus()
			state.workspaceBannerFocusPlaced = true
		}
	}
	focused := row.FocusWithin()
	if focused {
		state.workspaceBannerDismissAt = time.Time{}
	} else if state.workspaceBannerFocused && banner.Dismissable && banner.Timeout > 0 {
		state.workspaceBannerDismissAt = c.Now().Add(time.Duration(banner.Timeout) * time.Millisecond)
	}
	state.workspaceBannerFocused = focused
	if !state.workspaceBannerDismissAt.IsZero() && !state.workspaceBannerDismissPending {
		remaining := state.workspaceBannerDismissAt.Sub(c.Now())
		if remaining > 0 {
			c.After(remaining)
		} else {
			state.workspaceBannerDismissPending = true
			send(backend.KernelWorkspaceAction{Type: "banner", ID: banner.ID, Action: "dismiss"})
		}
	}
}

func (a *app) workspaceConflictControls(c *ui.Context) {
	w := a.nativeWorkspace()
	if w == nil || w.Conflict == nil {
		return
	}
	conflict := w.Conflict
	ui.Row(c.Key("workspace-conflict-recovery")).FillWidth().Gap(8).Wrap().Padding(12).Margin(0, 0, 12, 0).Background(c.Theme().Surface).Radius(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(160).Gap(4).Children(func() {
			ui.Text(c, workspaceLabel(c.Locale(), "workspace.conflicts."+conflict.Kind)).FontWeight(500).Role(ui.RoleStatus)
			ui.Text(c, workspaceLabel(c.Locale(), "workspace.conflicts.remaining", fmt.Sprint(conflict.Remaining))).TextColor(c.Theme().TextMuted)
			if conflict.UntrackedExcluded {
				ui.Text(c, workspaceLabel(c.Locale(), "workspace.conflicts.untracked")).TextColor(c.Theme().Warning)
			}
		})
		quietButton(c.Key("reopen-conflicts"), workspaceLabel(c.Locale(), "workspace.conflicts.open"), "").Disabled(a.workspacePending > 0).OnClick(func() {
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "conflicts.open", Identity: conflict.Identity})
		})
		quietButton(c.Key("continue-conflicts"), workspaceLabel(c.Locale(), "workspace.conflicts.continue"), "").Disabled(!conflict.CanContinue || a.workspacePending > 0).OnClick(func() {
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "conflicts.continue", Identity: conflict.Identity})
		})
		quietButton(c.Key("abort-conflicts"), workspaceLabel(c.Locale(), "workspace.conflicts.abort"), "").Disabled(!conflict.CanAbort || a.workspacePending > 0).OnClick(func() {
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "conflicts.abort", Identity: conflict.Identity})
		})
	})
}

func (a *app) workspaceNavigationView(c *ui.Context) {
	w := a.nativeWorkspace()
	if w == nil || w.Repository == nil {
		return
	}
	label := func(key string) string { return workspaceLabel(c.Locale(), "workspace."+key) }
	if a.tab == historyTab {
		ui.Row(c.Key("workspace-compare-controls")).FillWidth().Gap(8).Wrap().Margin(0, 0, 12, 0).Children(func() {
			// Keep the title and comparison entry on one baseline, leaving the
			// vertical space to the records rather than a second toolbar.
			ui.Text(c, locales.In(c.Locale()).History()).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Grow(1).Role(ui.RoleHeading).Level(1)
			anchor := quietButton(c.Key("workspace-compare"), label("compare"), "git-branch").OnClick(func() {
				a.workspaceCompareOpen = !a.workspaceCompareOpen
				a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.list", Value: a.workspaceCompareOpen})
			})
			if ui.Popover(c.Key("workspace-compare-panel"), anchor, &a.workspaceCompareOpen, func() {
				ui.TextInput(c.Key("compare-filter"), &a.workspaceCompareFilter).Label(label("searchBranches")).Placeholder(label("searchBranches")).FillWidth().OnChange(func() {
					a.workspaceAction(backend.KernelWorkspaceAction{Type: "navigation.filter", Option: "compare", Path: a.workspaceCompareFilter})
				})
				a.workspaceCompareList.list().Key = func(i int) any { return w.Compare.Branches[i].Ref }
				ui.List(c.Key("workspace-comparison-branches"), a.workspaceCompareList.list(), len(w.Compare.Branches), func(i int) {
					b := w.Compare.Branches[i]
					quietButton(c.Key(b.Ref), b.Name, "").FillWidth().OnClick(func() {
						a.workspaceCompareOpen = false
						a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.branch", Ref: b.Ref, Mode: "behind"})
					})
				}).Height(240).FillWidth()
			}).Width(360).Padding(12).Gap(8).Dismissed() {
				a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.list", Value: false})
			}
			if w.Compare.Mode != "history" && w.Compare.Ref != nil {
				for _, mode := range []string{"ahead", "behind"} {
					count := w.Compare.Ahead
					if mode == "behind" {
						count = w.Compare.Behind
					}
					quietButton(c.Key(mode), fmt.Sprintf("%s %d", label(mode), count), "").Disabled(w.Compare.Mode == mode).OnClick(func() {
						a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.branch", Ref: *w.Compare.Ref, Mode: mode})
					})
				}
				quietButton(c.Key("comparison-operation"), label("compareOperation."+w.Compare.Operation), "chevron-down").Disabled(a.workspacePending > 0).PopoverMenuButton(func(m *ui.Menu) {
					for _, operation := range []string{"merge", "squash", "rebase"} {
						if m.Item(label("compareOperation." + operation)).Radio(w.Compare.Operation == operation).Chosen() {
							a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.operation", Option: operation})
						}
					}
				})
				quietButton(c.Key("merge-comparison"), label("compareApply"), "").Disabled(w.Locked || w.Compare.Mode != "behind" || w.Compare.OperationDisabled || a.workspacePending > 0).OnClick(func() { a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.merge"}) })
				quietButton(c.Key("end-comparison"), label("history"), "x").OnClick(func() { a.workspaceAction(backend.KernelWorkspaceAction{Type: "compare.history"}) })
			}
		})
		return
	}
	ui.Row(c.Key("workspace-changes-tools")).FillWidth().Gap(8).Wrap().Margin(0, 0, 12, 0).Children(func() {
		if w.Stash != nil {
			quietButton(c.Key("workspace-stash"), label("viewStash"), "history").Disabled(a.workspacePending > 0).OnClick(func() { a.reviewingFile = false; a.workspaceAction(backend.KernelWorkspaceAction{Type: "stash.view"}) })
		}
		menu := workspaceMenu(w, "changes", "")
		if menu != nil {
			quietButton(c.Key("workspace-changes-actions"), label("actions"), "ellipsis").PopoverMenuButton(func(m *ui.Menu) { renderWorkspaceMenu(m, c.Locale(), menu, a.workspaceAction) })
		}
		if w.ShowFilesFilter {
			identity := w.Repository.Path + fmt.Sprint(w.FilesFilter)
			if a.workspaceFilterIdentity != identity && a.workspacePending == 0 {
				a.workspaceFilesFilter = w.FilesFilter
				a.workspaceFilterIdentity = identity
			}
			ui.TextInput(c.Key("workspace-filter-files"), &a.workspaceFilesFilter.FilterText).Label(label("filterFiles")).Placeholder(label("filterFiles")).Grow(1).MinWidth(160).OnChange(func() {
				a.workspaceAction(backend.KernelWorkspaceAction{Type: "files.filter", Filter: a.workspaceFilesFilter})
			})
			var active []string
			for _, option := range []struct {
				key     string
				enabled bool
			}{{"filterIncluded", w.FilesFilter.Included}, {"filterExcluded", w.FilesFilter.Excluded}, {"filterNew", w.FilesFilter.New}, {"filterModified", w.FilesFilter.Modified}, {"filterDeleted", w.FilesFilter.Deleted}} {
				if option.enabled {
					active = append(active, label(option.key))
				}
			}
			filterLabel := label("filterStatus")
			if len(active) > 0 {
				filterLabel = strings.Join(active, " · ")
			}
			quietButton(c.Key("workspace-filter-options"), filterLabel, "chevron-down").PopoverMenuButton(func(m *ui.Menu) {
				for _, option := range []struct {
					key   string
					value *bool
				}{{"filterIncluded", &a.workspaceFilesFilter.Included}, {"filterExcluded", &a.workspaceFilesFilter.Excluded}, {"filterNew", &a.workspaceFilesFilter.New}, {"filterModified", &a.workspaceFilesFilter.Modified}, {"filterDeleted", &a.workspaceFilesFilter.Deleted}} {
					if m.Item(label(option.key)).Checked(*option.value).Chosen() {
						*option.value = !*option.value
						a.workspaceAction(backend.KernelWorkspaceAction{Type: "files.filter", Filter: a.workspaceFilesFilter})
					}
				}
			})
		}
	})
	if w.ShowFilesFilter && (w.FilesFilter != backend.KernelWorkspaceFileFilter{}) && a.coreSnapshot.Selected != nil {
		tx := locales.In(c.Locale())
		ui.Row(c).FillWidth().Wrap().Gap(12).Shrink(0).Margin(0, 0, 8, 0).Children(func() {
			message := tx.MatchedFiles(fmt.Sprint(len(w.VisibleFileIDs)), fmt.Sprint(len(a.coreSnapshot.Selected.Files)))
			if len(w.VisibleFileIDs) == 0 {
				message = tx.NoMatchingFiles()
			}
			ui.Text(c, message).Grow(1).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
			quietButton(c, tx.ClearFileFilters(), "x").OnClick(func() {
				a.workspaceFilesFilter = backend.KernelWorkspaceFileFilter{}
				a.workspaceAction(backend.KernelWorkspaceAction{Type: "files.filter", Filter: a.workspaceFilesFilter})
			})
		})
	}
}

// History selection updates content on this page; it never hides the list.
func (a *app) workspaceHistoryList(c *ui.Context) {
	w := a.nativeWorkspace()
	if w == nil || a.coreSnapshot.Selected == nil {
		return
	}
	selected := a.coreSnapshot.Selected
	if len(selected.HistorySelection) == 1 && !a.loadingCommit {
		if a.workspaceHistorySummaries == nil {
			a.workspaceHistorySummaries = make(map[string]backend.KernelHistorySummary)
		}
		sha := selected.HistorySelection[0]
		a.workspaceHistorySummaries[sha] = backend.KernelHistorySummary{SHA: sha, FileCount: len(selected.HistoryFiles), LinesAdded: selected.HistoryLinesAdded, LinesDeleted: selected.HistoryLinesDeleted}
	}
	renderWorkspaceHistoryList(c, &a.nativeWorkspaceState, a.commitsList.list(), w, a.commits, a.coreSnapshot.Selected.HistorySelection, a.historyLoaded && !a.loadingHistory && a.historyErr == nil, a.workspaceAction, func(c *ui.Context) { a.workspaceHistoryReview(c) })
	a.loadVisibleHistorySummaries()
	if a.workspaceHistorySummaryError != nil {
		ui.Text(c, a.workspaceHistorySummaryError.Error()).TextColor(c.Theme().Danger)
		quietButton(c, locales.In(c.Locale()).Refresh(), "refresh-cw").OnClick(func() { a.workspaceHistorySummaryError = nil; a.loadVisibleHistorySummaries() })
	}
	if w.Compare.Mode == "history" && a.historyMore {
		tx := locales.In(c.Locale())
		quietButton(c.Key("load-history"), tx.LoadMore(), "chevron-down").AlignSelf(ui.Center).Margin(16, 0, 0, 0).Disabled(a.loadingHistory || !a.ready).OnClick(a.loadCoreHistory)
	}
}

func (a *app) workspaceStashView(c *ui.Context) bool {
	w := a.nativeWorkspace()
	if w == nil || w.Stash == nil || !w.Stash.Selected || a.tab != changesTab || a.reviewingFile {
		return false
	}
	stash := w.Stash
	ui.Row(c.Key("workspace-stash-header")).FillWidth().Gap(8).Wrap().Margin(0, 0, 16, 0).Children(func() {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.stash")).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Grow(1)
		for _, action := range []struct{ key, kind string }{{"restoreStash", "stash.restore"}, {"discardStash", "stash.discard"}, {"hideStash", "stash.hide"}} {
			quietButton(c.Key(action.kind), workspaceLabel(c.Locale(), "workspace."+action.key), "").Disabled(a.workspacePending > 0).OnClick(func() {
				a.reviewingFile = false
				a.workspaceAction(backend.KernelWorkspaceAction{Type: action.kind, SHA: stash.SHA})
			})
		}
	})
	if stash.LoadState != "Loaded" {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.loadingStash")).Role(ui.RoleStatus)
		return true
	}
	a.workspaceStashList.list().Key = func(i int) any { return stash.Files[i].ID }
	ui.List(c.Key("workspace-stash-files"), a.workspaceStashList.list(), len(stash.Files), func(i int) {
		file := stash.Files[i]
		quietButton(c.Key(file.ID), file.Path, fileIcon(file.Path, false)).FillWidth().Height(44).OnClick(func() {
			a.reviewingFile = true
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "stash.file", SHA: stash.SHA, FileID: file.ID})
		})
	}).Grow(1).MinHeight(0).FillWidth()
	return true
}

func (a *app) workspaceDiffContextMenu(c *ui.Context, element ui.Element) {
	w := a.nativeWorkspace()
	if w == nil {
		return
	}
	if w.TextDiff != nil && !w.TextDiff.Prepared && a.workspacePreparedDiffID != w.TextDiff.Identity {
		a.workspacePreparedDiffID = w.TextDiff.Identity
		a.workspaceAction(backend.KernelWorkspaceAction{Type: "diff.prepare", FileID: w.TextDiff.FileID, Identity: w.TextDiff.Identity})
	}
	if element.FocusWithin() {
		a.workspaceDiffFocused = true
	}
	a.workspaceDiffWidth = element.Bounds().W
	locale := c.Locale()
	// The finite MyGo adapter exposes the same menu-extension
	// contract as editor.State. Bind data/callbacks, never a frame Context.
	if a.preview != nil {
		for _, patch := range a.preview.patches {
			patch.state.ContextMenu = func(m *ui.Menu) { a.workspaceDiffMenuItems(m, locale) }
		}
	}
	element.PopoverMenu(func(m *ui.Menu) {
		m.EditItems()
		a.workspaceDiffMenuItems(m, locale)
	})
}

// The inner DiffState editor needs this same extension through MyGo's menu
// hook; attaching only to the outer DiffView does not override its inner menu.
// A toolkit adapter must retain the locale string, never a Context/Element.
func (a *app) workspaceDiffMenuItems(m *ui.Menu, locale string) {
	w := a.nativeWorkspace()
	if w == nil {
		return
	}
	if diff := w.TextDiff; diff != nil {
		key, action := "workspace.expandWhole", "diff.expand"
		if diff.ExpandedText != nil {
			key, action = "workspace.collapseExpanded", "diff.collapse"
		}
		if m.Item(workspaceLabel(locale, key)).Disabled(!diff.Prepared || !diff.CanExpand || a.workspacePending > 0).Chosen() {
			a.workspaceAction(backend.KernelWorkspaceAction{Type: action, FileID: diff.FileID, Identity: diff.Identity})
		}
	}
	m.Separator()
	ids := a.selectedNativeLineIDs()
	selection := a.nativeDiffSelection()
	if m.Item(workspaceLabel(locale, "workspace.discardLines")).Disabled(selection == nil || len(ids) == 0 || w.HideWhitespace || a.composerLocked()).Chosen() {
		a.workspaceAction(backend.KernelWorkspaceAction{Type: "diff.discard", FileID: selection.FileID, Identity: selection.Identity, LineIDs: ids})
	}
	m.Separator()
	for _, option := range []struct {
		key, name string
		value     bool
	}{{"whitespace", "whitespace", w.HideWhitespace}, {"sideBySide", "sideBySide", w.SideBySide}, {"checkMarks", "checkMarks", w.CheckMarks}} {
		if m.Item(workspaceLabel(locale, "workspace."+option.key)).Checked(option.value).Chosen() {
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "diff.option", Option: option.name, Value: !option.value})
		}
	}
}
