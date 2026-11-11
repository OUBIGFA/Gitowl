package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// The record is readable without selecting it. File names expand only from
// its count; selecting a row is reserved for history operations.
// MyGo owns the same Ctrl/Shift/arrow selection and original context menus.
func renderWorkspaceHistoryList(c *ui.Context, state *nativeWorkspaceState, listState *ui.ListState, workspace *backend.KernelWorkspace, commits []backend.Commit, shas []string, showEmpty bool, send func(backend.KernelWorkspaceAction), selectedContent func(*ui.Context)) {
	identity := workspace.Repository.Path + strings.Join(shas, "/")
	if state.workspaceHistoryIdentity != identity {
		state.workspaceHistorySelection.Clear()
		for _, sha := range shas {
			state.workspaceHistorySelection.Add(sha)
		}
		state.workspaceHistoryIdentity = identity
		state.workspaceHistoryCombinedFilesOpen = false
		if len(shas) != 1 || shas[0] != state.workspaceHistoryFilesSHA {
			state.workspaceHistoryFilesSHA = ""
		}
	}
	listState.Key = func(i int) any { return commits[i].SHA }
	listState.Label = func(i int) string { return commits[i].Summary }
	listState.Selection = &state.workspaceHistorySelection
	listState.Selected = &state.workspaceHistoryCursor
	reordering := len(workspace.ReorderSelection) > 0
	if reordering {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.reorderHint")).Role(ui.RoleStatus).Margin(0, 0, 8, 0)
		quietButton(c.Key("cancel-workspace-reorder"), workspaceLabel(c.Locale(), "workspace.cancelReorder"), "x").OnClick(func() { send(backend.KernelWorkspaceAction{Type: "history.reorder.cancel"}) })
		if c.Shortcut(0, ui.KeyEscape) {
			send(backend.KernelWorkspaceAction{Type: "history.reorder.cancel"})
		}
		quietButton(c.Key("insert-workspace-first"), workspaceLabel(c.Locale(), "workspace.insertHere"), "arrow-up-right").OnClick(func() {
			send(backend.KernelWorkspaceAction{Type: "history.reorder", SHAs: slices.Clone(workspace.ReorderSelection)})
		})
	}
	locale := c.Locale()
	tx := locales.In(locale)
	// A faint neutral surface identifies selection without competing with the
	// message. Preserve the platform selection palette in high-contrast mode.
	baseTheme := c.Theme()
	listTheme := *baseTheme
	if !c.Preferences().HighContrast {
		selected := baseTheme.Background.Mix(baseTheme.Text, 0.045)
		listTheme.Accent, listTheme.InactiveAccent, listTheme.AccentText = selected, selected, baseTheme.Text
		listTheme.SurfaceHover = baseTheme.Background.Mix(baseTheme.Text, 0.02)
	}
	c.SetTheme(&listTheme)
	defer c.SetTheme(baseTheme)
	list := ui.List(c.Key("workspace-history-list"), listState, len(commits), func(i int) {
		commit := commits[i]
		row := ui.Column(c.Key(commit.SHA)).FillWidth().Padding(16).Gap(8).Label(commit.Summary)
		row.Children(func() {
			ui.Text(c, commit.Summary).FillWidth().FontSize(c.Theme().Rem(17.0 / 15)).FontWeight(500)
			if body := strings.TrimSpace(commit.Body); body != "" {
				ui.Text(c.Key("body"), body).FillWidth().MaxWidth(c.Theme().Rem(48)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(baseTheme.TextMuted).Selectable()
			}
			ui.Row(c.Key("metadata")).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				author := commit.AuthorName + " · " + commitDate(c, commit)
				ui.Text(c, author).SingleLine().Grow(1).MinWidth(0).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(baseTheme.TextMuted).Tooltip(author)
				if summary, ok := state.workspaceHistorySummaries[commit.SHA]; ok {
					expanded := state.workspaceHistoryFilesSHA == commit.SHA
					icon := "chevron-right"
					if expanded {
						icon = "chevron-down"
					}
					button := ui.ButtonBase(c.Key("history-file-count")).MinHeight(28).Padding(4, 6).Gap(4).Shrink(0).Label(tx.FileCount(summary.FileCount)).Expanded(expanded)
					quietSurface(c, button).Children(func() {
						ui.Text(c, tx.FileCount(summary.FileCount)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(baseTheme.TextMuted)
						ui.Icon(c, icons[icon]).FontSize(14).TextColor(baseTheme.TextMuted)
					}).OnClick(func() {
						if expanded {
							state.workspaceHistoryFilesSHA = ""
							return
						}
						state.workspaceHistoryFilesSHA = commit.SHA
						if len(shas) != 1 || shas[0] != commit.SHA {
							send(backend.KernelWorkspaceAction{Type: "history.select", SHAs: []string{commit.SHA}})
						}
					})
				} else {
					ui.Text(c, tx.LoadingChangeset()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(baseTheme.TextMuted).Role(ui.RoleStatus)
				}
				if menu := workspaceMenu(workspace, "commit", commit.SHA); menu != nil {
					for index, entry := range menu.Items {
						if entry.LabelKey == "workspace.copySHA" {
							button := ui.ButtonBase(c.Key("history-copy-sha")).MinHeight(28).Padding(4, 6).Gap(6).Shrink(0).
								Label(workspaceLabel(locale, entry.LabelKey)).Tooltip(commit.SHA).Description(commit.SHA).Disabled(!entry.Enabled)
							quietSurface(c, button).Children(func() {
								ui.Text(c, commit.ShortSHA).FontSize(c.Theme().Rem(14.0 / 15)).Font(workspaceCodeFont()).TextColor(baseTheme.TextMuted).NoWrap()
								ui.Icon(c, icons["copy"]).FontSize(14).TextColor(baseTheme.TextMuted)
							}).OnClick(func() {
								send(backend.KernelWorkspaceAction{Type: "menu", Target: menu.Target, Lease: menu.Lease, MenuPath: []int{index}})
							})
							break
						}
					}
				}
			})
			if len(commit.Tags) > 0 {
				ui.Text(c, tx.CommitTags(strings.Join(commit.Tags, ", "))).TextColor(baseTheme.TextMuted)
			}
			if state.workspaceHistoryFilesSHA == commit.SHA && len(shas) == 1 && shas[0] == commit.SHA && selectedContent != nil {
				selectedContent(c)
			}
			if reordering {
				quietButton(c.Key("insert-after"), workspaceLabel(locale, "workspace.insertHere"), "arrow-up-right").Disabled(slices.Contains(workspace.ReorderSelection, commit.SHA)).OnClick(func() {
					sha := commit.SHA
					send(backend.KernelWorkspaceAction{Type: "history.reorder", SHAs: slices.Clone(workspace.ReorderSelection), BeforeSHA: &sha})
				})
			}
		})
		row.PopoverMenu(func(m *ui.Menu) { renderWorkspaceMenu(m, locale, workspaceMenu(workspace, "commit", commit.SHA), send) })
	}).Grow(1).MinHeight(0).FillWidth().Gap(4).Label(workspaceLabel(locale, "workspace.history")).Children(func() {
		if len(commits) == 0 && showEmpty {
			ui.Text(c, locales.In(locale).NoHistory()).Padding(20).TextColor(c.Theme().TextMuted)
		}
	})
	// Arrow keys focus the list itself. Expose the same current-row menu there
	// so keyboard users reach the original copy and history operations too.
	list.PopoverMenu(func(m *ui.Menu) {
		if i := state.workspaceHistoryCursor; i >= 0 && i < len(commits) {
			renderWorkspaceMenu(m, locale, workspaceMenu(workspace, "commit", commits[i].SHA), send)
		}
	})
	changed := list.Changed()
	if changed {
		var selected []string
		for _, commit := range commits {
			if state.workspaceHistorySelection.Has(commit.SHA) {
				selected = append(selected, commit.SHA)
			}
		}
		if len(selected) != 1 || selected[0] != state.workspaceHistoryFilesSHA {
			state.workspaceHistoryFilesSHA = ""
		}
		send(backend.KernelWorkspaceAction{Type: "history.select", SHAs: selected})
	}
}

// A continuous record keeps each commit's message, metadata and file scope
// together. There is no duplicate title or separately sized detail pane.
func (a *app) workspaceHistoryPage(c *ui.Context) {
	tx := locales.In(c.Locale())
	a.workspaceNavigationView(c)
	if a.loadingHistory {
		ui.Text(c, tx.LoadingHistory()).Role(ui.RoleStatus)
	}
	if a.historyErr != nil {
		showError(c, tx.FailedHistory(a.historyErr.Error()))
	}
	if len(a.coreSnapshot.Selected.HistorySelection) > 1 {
		ui.Column(c.Key("combined-history")).FillWidth().Shrink(0).Padding(0, 0, 16, 0).Children(func() { a.workspaceHistoryReview(c) })
	}
	a.workspaceHistoryList(c)
}

// A multi-selection remains the original combined changeset, never mislabeled
// as one arbitrary commit. It is visible without leaving the history list.
func (a *app) workspaceHistoryReview(c *ui.Context) bool {
	w := a.nativeWorkspace()
	if w == nil || a.coreSnapshot.Selected == nil || a.tab != historyTab {
		return false
	}
	selected := a.coreSnapshot.Selected
	if len(selected.HistorySelection) == 0 {
		return false
	}
	tx := locales.In(c.Locale())
	if len(selected.HistorySelection) > 1 {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, workspaceLabel(c.Locale(), "workspace.historySelection", fmt.Sprint(len(selected.HistorySelection)))).Grow(1).FontWeight(500)
			if w.HistorySelectionContiguous && !a.loadingCommit && a.commitErr == nil {
				quietButton(c.Key("combined-file-count"), tx.FileCount(len(selected.HistoryFiles)), "chevron-down").Expanded(a.workspaceHistoryCombinedFilesOpen).OnClick(func() { a.workspaceHistoryCombinedFilesOpen = !a.workspaceHistoryCombinedFilesOpen })
			}
		})
	}
	if !w.HistorySelectionContiguous {
		emptyState(c, workspaceLabel(c.Locale(), "workspace.nonContiguous"))
		return true
	}
	if a.loadingCommit {
		ui.Text(c, tx.LoadingChangeset()).Role(ui.RoleStatus)
		return true
	}
	if a.commitErr != nil {
		showError(c, tx.FailedChangeset(a.commitErr.Error()))
		return true
	}
	if len(selected.HistorySelection) > 1 && !a.workspaceHistoryCombinedFilesOpen {
		return true
	}
	ui.Text(c, tx.CommitStats(selected.HistoryLinesAdded, selected.HistoryLinesDeleted)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	if len(selected.HistoryFiles) == 0 {
		emptyState(c, tx.NoCommitFiles())
		return true
	}
	files := selected.HistoryFiles
	a.commitFilesList.list().Key = func(i int) any { return files[i].ID }
	ui.List(c.Key("workspace-history-files"), a.commitFilesList.list(), len(files), func(i int) {
		file := files[i]
		button := ui.ButtonBase(c.Key(file.ID)).MinHeight(40).FillWidth().Padding(6, 8).Gap(12).Label(tx.OpenFile(file.Path)).Tooltip(file.Path).Disabled(!a.ready)
		quietSurface(c, button).Children(func() {
			ui.Icon(c, icons[fileIcon(file.Path, file.IsSubmodule)]).FontSize(18).Shrink(0)
			ui.Text(c, file.Path).SingleLine().Grow(1).MinWidth(0)
			ui.Text(c, fileStatus(tx, file.Status)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			ui.Icon(c, icons["chevron-right"]).FontSize(18).Shrink(0)
		}).OnClick(func() { a.reviewWorkspacePath(file.Path) })
		a.fileContextMenu(c, button, file.ID)
	}).Height(min(c.Theme().Rem(12), float32(len(files))*40)).FillWidth().Margin(4, 0, 0, 0).Label(tx.ChangedFiles())
	return true
}

// Fetch only the virtual list's visible commits. Exact statistics come from
// Desktop's original Git parser, without selecting each commit to obtain them.
func (a *app) loadVisibleHistorySummaries() {
	ref, ok := a.coreReference()
	if !ok || a.workspaceHistorySummaryLoading || a.workspaceHistorySummaryError != nil {
		return
	}
	first, last := a.commitsList.list().Visible()
	var shas []string
	for i := max(0, first); i < min(len(a.commits), last+1) && len(shas) < 32; i++ {
		if _, exists := a.workspaceHistorySummaries[a.commits[i].SHA]; !exists {
			shas = append(shas, a.commits[i].SHA)
		}
	}
	if len(shas) == 0 {
		return
	}
	a.workspaceHistorySummaryLoading = true
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.HistorySummaries(ctx, ref, shas) },
		complete: func(result backend.KernelResult, err error) {
			a.workspaceHistorySummaryLoading = false
			current, valid := a.coreReference()
			if !valid || current != ref {
				return
			}
			a.workspaceHistorySummaryError = err
			if err != nil {
				return
			}
			if a.workspaceHistorySummaries == nil {
				a.workspaceHistorySummaries = make(map[string]backend.KernelHistorySummary)
			}
			for _, summary := range result.HistorySummaries {
				a.workspaceHistorySummaries[summary.SHA] = summary
			}
		},
	})
}
