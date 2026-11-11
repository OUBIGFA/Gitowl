package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Only frontend interaction state is retained. Original branch/stash/menu
// availability, history selection and continuations come from each snapshot.
type nativeWorkspaceState struct {
	workspaceNavigation               string
	workspaceBranchOpen               bool
	workspaceWorktreeOpen             bool
	workspacePROpen                   bool
	workspaceCompareListVisible       bool
	workspaceCompareOpen              bool
	workspaceBranchFilter             string
	workspaceWorktreeFilter           string
	workspacePRFilter                 string
	workspaceCompareFilter            string
	workspaceFilesFilter              backend.KernelWorkspaceFileFilter
	workspaceFilterIdentity           string
	workspaceBranchList               nativeListState
	workspaceTreeList                 nativeListState
	workspacePRList                   nativeListState
	workspaceCompareList              nativeListState
	workspaceStashList                nativeListState
	workspaceHistorySelection         ui.Selection[string]
	workspaceFilesSelection           ui.Selection[string]
	workspaceFilePointerID            string
	workspaceFileReviewPath           string
	workspaceFilePointerPlain         bool
	workspaceFilesIdentity            string
	workspaceFileCursor               int
	workspaceHistoryIdentity          string
	workspaceHistoryFilesSHA          string
	workspaceHistoryCombinedFilesOpen bool
	workspaceHistoryCursor            int
	workspaceHistorySummaries         map[string]backend.KernelHistorySummary
	workspaceHistorySummaryLoading    bool
	workspaceHistorySummaryError      error
	workspaceHistorySelectionPending  int
	workspacePending                  int
	workspaceErr                      error
	workspaceBannerDismissAt          time.Time
	workspaceBannerFocused            bool
	workspaceBannerFocusPlaced        bool
	workspaceBannerID                 int
	workspaceBannerStarted            time.Time
	workspaceBannerDismissPending     bool
	workspaceFocus                    string
	workspaceDiffFocused              bool
	workspacePreparedDiffID           string
	workspaceDiffWidth                float32
	workspaceRailCurrent              float32
	workspaceRailMaximum              float32
	WorkspaceRailWidth                float32
}

func workspaceLabel(locale, key string, data ...string) string {
	text := nativeDialogLabel(locale, key)
	// These placeholders belong to labels, never to user/Git names. The locale
	// generator escapes ICU braces so technical data cannot become format code.
	pairs := make([]string, 0, len(data)*2)
	for i, value := range data {
		pairs = append(pairs, fmt.Sprintf("{%d}", i), value)
	}
	if len(pairs) != 0 {
		text = strings.NewReplacer(pairs...).Replace(text)
	}
	return text
}

func (a *app) nativeWorkspace() *backend.KernelWorkspace {
	if a.coreSnapshot == nil {
		return nil
	}
	return a.coreSnapshot.Workspace
}

func (a *app) workspaceAction(action backend.KernelWorkspaceAction) {
	if action.Type == "banner" {
		if a.core == nil || a.closed {
			return
		}
		core, ctx := a.core, a.ctx
		a.runCoreTask(func() (backend.KernelResult, error) { return core.WorkspaceBannerAction(ctx, action.ID, action.Action) }, func(_ backend.KernelResult, err error) { a.workspaceErr = err })
		return
	}
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	core, ctx := a.core, a.ctx
	a.workspacePending++
	a.workspaceErr = nil
	if action.Type == "history.select" {
		a.workspaceHistorySelectionPending++
		a.loadingCommit, a.commitErr = true, nil
	}
	// A popup's answer must not queue behind the operation awaiting it. The
	// existing separate reply path remains untouched; navigation is ordered.
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.WorkspaceAction(ctx, ref, action) },
		complete: func(_ backend.KernelResult, err error) {
			a.workspacePending--
			a.workspaceErr = err
			if action.Type == "history.select" {
				a.workspaceHistorySelectionPending--
				a.loadingCommit, a.commitErr = a.workspaceHistorySelectionPending > 0, err
			}
		},
	})
}

func workspaceMenu(workspace *backend.KernelWorkspace, kind, id string) *backend.KernelWorkspaceMenu {
	if workspace == nil {
		return nil
	}
	for i := range workspace.Menus {
		if workspace.Menus[i].Target.Kind == kind && workspace.Menus[i].Target.ID == id {
			return &workspace.Menus[i]
		}
	}
	return nil
}

// Both the contextual and overflow presentation use the same typed menu data;
// no label is interpreted as a Dispatcher method or action identifier.
func renderWorkspaceMenu(m *ui.Menu, locale string, menu *backend.KernelWorkspaceMenu, send func(backend.KernelWorkspaceAction)) {
	if menu == nil {
		return
	}
	renderNativeMenuItems(m, locale, menu.Items, func(path []int) {
		send(backend.KernelWorkspaceAction{Type: "menu", Target: menu.Target, Lease: menu.Lease, MenuPath: path})
	})
}

func renderNativeMenuItems(m *ui.Menu, locale string, items []backend.KernelWorkspaceMenuItem, choose func([]int)) {
	var render func(*ui.Menu, []backend.KernelWorkspaceMenuItem, []int)
	render = func(m *ui.Menu, items []backend.KernelWorkspaceMenuItem, prefix []int) {
		m.MaxWidth(420)
		for i, entry := range items {
			path := append(slices.Clone(prefix), i)
			if entry.Separator {
				m.Separator()
				continue
			}
			label := workspaceLabel(locale, entry.LabelKey, entry.Data...)
			if len(entry.Children) > 0 {
				m.Submenu(label, func(sub *ui.Menu) { render(sub, entry.Children, path) }).Disabled(!entry.Enabled)
				continue
			}
			item := m.Item(label).ID(fmt.Sprint(path)).Disabled(!entry.Enabled).Tooltip(label)
			if entry.Checked != nil {
				item.Checked(*entry.Checked)
			}
			if item.Chosen() {
				choose(path)
			}
		}
	}
	render(m, items, nil)
}

// workspaceReviewDiff and workspaceReviewSelection are presentation overrides
// only. Expansion retains original line IDs for all include/discard requests.
func (a *app) workspaceReviewDiff(original *backend.Diff) *backend.Diff {
	w := a.nativeWorkspace()
	if w != nil && w.TextDiff != nil && w.TextDiff.ExpandedText != nil {
		return &backend.Diff{Kind: "text", Text: w.TextDiff.ExpandedText}
	}
	return original
}
func (a *app) workspaceReviewSelection(original *backend.KernelDiffSelection) *backend.KernelDiffSelection {
	w := a.nativeWorkspace()
	if w != nil && w.TextDiff != nil && w.TextDiff.ExpandedText != nil && w.TextDiff.Selection != nil {
		return w.TextDiff.Selection
	}
	return original
}

func (a *app) fileContextMenu(c *ui.Context, row ui.Element, fileID string) {
	kind := "file"
	if a.tab == historyTab {
		kind = "historyFile"
	}
	menu := workspaceMenu(a.nativeWorkspace(), kind, fileID)
	if menu != nil {
		row.PopoverMenu(func(m *ui.Menu) { renderWorkspaceMenu(m, c.Locale(), menu, a.workspaceAction) })
	}
}
func (a *app) commitContextMenu(c *ui.Context, row ui.Element, sha string) {
	menu := workspaceMenu(a.nativeWorkspace(), "commit", sha)
	if menu != nil {
		row.PopoverMenu(func(m *ui.Menu) { renderWorkspaceMenu(m, c.Locale(), menu, a.workspaceAction) })
	}
}

// This handles only native selection/layout. The caller drains the FIFO of
// presentation events once; a snapshot cannot replay one of these actions.
func (a *app) workspacePresentation(action string) bool {
	if action == "select-all" {
		if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil {
			return false
		}
		switch a.workspaceFocus {
		case "history":
			shas := make([]string, len(a.commits))
			for i, commit := range a.commits {
				shas[i] = commit.SHA
			}
			a.workspaceAction(backend.KernelWorkspaceAction{Type: "history.select", SHAs: shas})
			return true
		case "files":
			workspace := a.nativeWorkspace()
			if workspace == nil {
				return false
			}
			ref, ok := a.coreReference()
			if !ok {
				return false
			}
			ids := slices.Clone(workspace.VisibleFileIDs)
			core, ctx := a.core, a.ctx
			a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.SelectFiles(ctx, ref, ids) }})
			return true
		}
		return false // Text/selectable diff and system roles belong to MyGo.
	}
	if action != "increase-active-resizable-width" && action != "decrease-active-resizable-width" {
		return false
	}
	delta := float32(5) // Original Resizable keyboard step, in DIPs.
	if action == "decrease-active-resizable-width" {
		delta = -delta
	}
	switch a.workspaceFocus {
	case "diff":
		if a.preview == nil || a.workspaceDiffWidth <= 0 {
			return false
		}
		for _, patch := range a.preview.patches {
			if patch.state.Layout != editor.DiffSplit {
				continue
			}
			percent := patch.state.SplitPercent
			if percent == 0 {
				percent = 50
			}
			patch.state.SplitPercent = max(15, min(85, percent+100*delta/a.workspaceDiffWidth))
		}
		return true
	case "rail", "files", "history":
		current, maximum := a.workspaceRailCurrent, a.workspaceRailMaximum
		if current == 0 {
			current = projectRailWidth
		}
		if maximum == 0 {
			maximum = 360
		}
		a.WorkspaceRailWidth = max(200, min(maximum, current+delta))
		return true
	}
	return false
}

func (a *app) nativeRailWidth(c *ui.Context) float32 {
	width, _ := c.Size()
	value := a.WorkspaceRailWidth
	if value == 0 {
		value = projectRailWidth
	}
	// Keep the unique central composer readable at the minimum window size.
	a.workspaceRailMaximum = max(200, min(360, width-620))
	a.workspaceRailCurrent = max(200, min(value, a.workspaceRailMaximum))
	return a.workspaceRailCurrent
}

// Called at the end of a frame. Only the focus kind is retained, never a
// Context/Element; list handles are the official persistent focus interface.
func (a *app) observeWorkspaceFocus(c *ui.Context) {
	switch {
	case a.commitsList.list().FocusWithin(c):
		a.workspaceFocus = "history"
	case a.changesList.list().FocusWithin(c):
		a.workspaceFocus = "files"
	case a.coreProjects.FocusWithin(c):
		a.workspaceFocus = "rail"
	case a.workspaceDiffFocused:
		a.workspaceFocus = "diff"
	default:
		a.workspaceFocus = ""
	}
	a.workspaceDiffFocused = false
	active := a.workspaceFocus != ""
	if a.core != nil && a.coreSnapshot != nil && active != a.coreResizableFocus {
		a.coreResizableFocus = active
		core, ctx := a.core, a.ctx
		// Feed the real native focus fact into Desktop's original menu policy.
		// This updates eligibility; the existing 5-DIP presentation stays native.
		a.runCoreTask(func() (backend.KernelResult, error) {
			return core.ReportResizableFocus(ctx, active)
		}, nil)
	}
}
