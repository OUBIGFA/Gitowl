package main

import (
	"path"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

type workspaceChangesCallbacks struct {
	checkState  func(string) ui.CheckState
	include     func(string, bool)
	selectFiles func([]string)
	review      func(string)
	contextMenu func(*ui.Context, ui.Element, string)
}

// The overview is replaced by focused review on an ordinary file click.
// Native Ctrl/Shift and keyboard navigation remain selection-only. Inclusion
// stays a separate checkbox and never changes as a side effect of selection.
func (a *app) workspaceChangesList(c *ui.Context) bool {
	w := a.nativeWorkspace()
	if a.core == nil || w == nil || a.coreSnapshot.Selected == nil || a.tab != changesTab {
		return false
	}
	selected := a.coreSnapshot.Selected
	var files []backend.KernelFile
	for _, id := range w.VisibleFileIDs {
		for _, file := range selected.Files {
			if file.ID == id {
				files = append(files, file)
				break
			}
		}
	}
	renderWorkspaceChangesList(c, &a.nativeWorkspaceState, a.changesList.list(), files, selected.SelectedFileIDs, selected.Repository.Path, a.ready, a.composerLocked(), workspaceChangesCallbacks{
		checkState: a.fileCheckState, include: a.changeIncluded, selectFiles: a.selectWorkspaceFiles, review: a.reviewWorkspacePath, contextMenu: a.fileContextMenu,
	})
	return true
}

// This same production renderer is used by MyGoTester. ObserveInput is MyGo
// 0.8.1's non-consuming capture API: using Clicked on a child would steal the
// click from List's native Ctrl/Shift range selection. Only pointer-up queues a
// review; List.Changed also fires for arrows and must not open the review.
func renderWorkspaceChangesList(c *ui.Context, state *nativeWorkspaceState, listState *ui.ListState, files []backend.KernelFile, selectedIDs []string, repositoryPath string, ready, locked bool, callbacks workspaceChangesCallbacks) {
	identity := repositoryPath + strings.Join(selectedIDs, "/")
	if identity != state.workspaceFilesIdentity {
		state.workspaceFilesSelection.Clear()
		for _, id := range selectedIDs {
			state.workspaceFilesSelection.Add(id)
		}
		state.workspaceFilesIdentity = identity
	}
	listState.Key = func(i int) any { return files[i].ID }
	listState.Label = func(i int) string { return files[i].Path }
	listState.Selection = &state.workspaceFilesSelection
	listState.Selected = &state.workspaceFileCursor
	listState.MenuKeepsSelection = true
	tx := locales.In(c.Locale())
	// Selection is a neutral surface, distinct from the primary Save action.
	// MyGo still owns row selection, keyboard movement and accessibility.
	baseTheme := c.Theme()
	listTheme := *baseTheme
	if !c.Preferences().HighContrast {
		listTheme.Accent, listTheme.InactiveAccent, listTheme.AccentText = baseTheme.SurfacePressed, baseTheme.SurfacePressed, baseTheme.Text
	}
	c.SetTheme(&listTheme)
	defer c.SetTheme(baseTheme)
	list := ui.List(c.Key("workspace-changed-files"), listState, len(files), func(i int) {
		file := files[i]
		row := ui.Row(c.Key(file.ID)).Height(48).FillWidth().Padding(0, 4).Gap(8).Radius(6).Label(file.Path)
		row.Children(func() {
			included := callbacks.checkState(file.Path)
			// Inclusion is independent of row selection. Keep the checkbox's
			// original accent so a selected row cannot erase its checked state.
			c.SetTheme(baseTheme)
			ui.CheckboxState(c.Key("include-file"), &included, "").Size(32, 32).Padding(8).Label(tx.IncludeFile(file.Path)).Tooltip(tx.IncludeFile(file.Path)).Disabled(locked || !ready).OnChange(func() { callbacks.include(file.Path, included == ui.CheckOn) })
			c.SetTheme(&listTheme)
			// Observe only the file region, not its independent inclusion checkbox.
			ui.Row(c.Key("review-file-region")).Grow(1).MinWidth(0).FillHeight().Gap(8).Disabled(!ready).ObserveInput(func(event ui.InputEvent) {
				switch event.Kind {
				case ui.InputPointerDown:
					if event.Button == 0 {
						state.workspaceFilePointerID = file.ID
						state.workspaceFilePointerPlain = event.Mods&(ui.Cmd|ui.Ctrl|ui.Shift) == 0
					}
				case ui.InputPointerUp:
					if event.Button == 0 {
						if !event.Canceled && state.workspaceFilePointerID == file.ID && state.workspaceFilePointerPlain && event.Mods&(ui.Cmd|ui.Ctrl|ui.Shift) == 0 {
							state.workspaceFileReviewPath = file.Path
						}
						state.workspaceFilePointerID = ""
					}
				}
			}).Children(func() {
				ui.Icon(c, icons[fileIcon(file.Path, file.IsSubmodule)]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
				ui.Text(c, path.Base(file.Path)).SingleLine().Shrink(1).MinWidth(0).Tooltip(file.Path)
				if directory := path.Dir(file.Path); directory != "." {
					ui.Text(c, directory).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0).Tooltip(file.Path)
				} else {
					ui.Spacer(c)
				}
				// The pill keeps the base theme's colors on a selected row.
				c.SetTheme(baseTheme)
				fileStatusPill(c, tx, file.Status, file.IsSubmodule)
				c.SetTheme(&listTheme)
			})
			// Inherit the list's selected foreground, rather than resetting a
			// dark selected row to the unselected theme's dark text color.
			ui.ButtonBase(c.Key("review-file")).Size(36, 36).Padding(9).Radius(8).Label(tx.OpenFile(file.Path)).Tooltip(tx.OpenFile(file.Path)).Disabled(!ready).
				Children(func() { ui.Icon(c, icons["chevron-right"]).FontSize(18) }).OnClick(func() { callbacks.review(file.Path) })
		})
		if callbacks.contextMenu != nil {
			callbacks.contextMenu(c, row, file.ID)
		}
	}).Grow(1).MinHeight(0).FillWidth().Gap(2).Padding(4, 0, 0, 0).Label(tx.ChangedFiles())
	if list.Changed() {
		var ids []string
		for _, file := range files {
			if state.workspaceFilesSelection.Has(file.ID) {
				ids = append(ids, file.ID)
			}
		}
		callbacks.selectFiles(ids)
	}
	if path := state.workspaceFileReviewPath; path != "" {
		state.workspaceFileReviewPath = ""
		callbacks.review(path)
	} else if list.Submitted() && state.workspaceFileCursor >= 0 && state.workspaceFileCursor < len(files) {
		callbacks.review(files[state.workspaceFileCursor].Path)
	}
}

func (a *app) selectWorkspaceFiles(ids []string) {
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	ids = slices.Clone(ids)
	if ids == nil {
		ids = []string{}
	}
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.SelectFiles(ctx, ref, ids) }, complete: func(_ backend.KernelResult, err error) { a.workspaceErr = err }})
}

func (a *app) reviewWorkspacePath(path string) {
	if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil {
		return
	}
	files := a.coreSnapshot.Selected.Files
	if a.tab == historyTab {
		files = a.coreSnapshot.Selected.HistoryFiles
	}
	for i, file := range files {
		if file.Path == path {
			a.reviewFile(i)
			return
		}
	}
}

// Called by the parent snapshot adapter. It resets presentation on a repository
// switch without cancelling or erasing an original in-flight operation.
func (a *app) applyWorkspaceSnapshot(snapshot backend.KernelSnapshot, changedRepository bool) {
	if snapshot.Workspace == nil || snapshot.Workspace.TextDiff == nil || snapshot.Workspace.TextDiff.Identity != a.workspacePreparedDiffID {
		a.workspacePreparedDiffID = ""
	}
	if changedRepository {
		a.workspaceHistorySource, a.workspaceHistoryRows = nil, nil
		a.workspaceHistoryScroll, a.workspaceHistoryScrollY = ui.ScrollState{}, 0
		a.workspaceHistorySummaries = nil
		a.workspaceHistorySummaryError = nil
		a.workspaceBranchOpen, a.workspaceWorktreeOpen, a.workspacePROpen, a.workspaceCompareOpen = false, false, false, false
		a.workspaceHistoryIdentity, a.workspaceFilesIdentity, a.workspaceFilterIdentity, a.workspacePreparedDiffID = "", "", "", ""
		a.workspaceFilePointerID, a.workspaceFileReviewPath = "", ""
		a.workspaceHistoryCursor, a.workspaceFileCursor = -1, -1
		a.workspaceBranchList, a.workspaceTreeList, a.workspacePRList, a.workspaceCompareList, a.workspaceStashList = nativeListState{}, nativeListState{}, nativeListState{}, nativeListState{}, nativeListState{}
	}
	if w := snapshot.Workspace; w != nil && (changedRepository || a.workspacePending == 0) {
		if changedRepository || a.workspaceNavigation != w.Navigation {
			a.workspaceNavigation = w.Navigation
			a.workspaceBranchOpen = w.Navigation == "branches"
			a.workspacePROpen = w.Navigation == "pullRequests"
			a.workspaceWorktreeOpen = w.Navigation == "worktrees"
		}
		if changedRepository || a.workspaceCompareListVisible != w.Compare.ShowBranchList {
			a.workspaceCompareListVisible = w.Compare.ShowBranchList
			a.workspaceCompareOpen = w.Compare.ShowBranchList
		}
		a.workspaceBranchFilter, a.workspaceWorktreeFilter, a.workspacePRFilter, a.workspaceCompareFilter = w.Filters.Branches, w.Filters.Worktrees, w.Filters.PullRequests, w.Filters.Compare
		a.workspaceFilesFilter = w.FilesFilter
	}
}

func (a *app) workspaceStashFile() *backend.KernelFile {
	w := a.nativeWorkspace()
	if w == nil || w.Stash == nil || !w.Stash.Selected || w.Stash.FileID == nil {
		return nil
	}
	for i := range w.Stash.Files {
		if w.Stash.Files[i].ID == *w.Stash.FileID {
			return &w.Stash.Files[i]
		}
	}
	return nil
}
