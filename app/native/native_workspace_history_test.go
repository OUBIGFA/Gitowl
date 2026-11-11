package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestHistoryOverviewShowsCommitContentWithoutAnotherPage(t *testing.T) {
	for _, size := range []struct{ width, height int }{{900, 620}, {1440, 900}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, locale), func(t *testing.T) {
				a := newApp(nil, nil)
				defer a.close()
				a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
				a.ready, a.historyLoaded, a.tab = true, true, historyTab
				sha := "1234567890abcdef1234567890abcdef12345678"
				ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
				a.repo = &backend.Repository{ID: 1, Path: "fixture", Name: "Fixture"}
				a.commits = []backend.Commit{{SHA: sha, ShortSHA: sha[:7], Summary: "Improve project history", Body: "Keep the changed files visible."}, {SHA: "other", Summary: "Previous commit"}}
				a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{sha: {SHA: sha, FileCount: 1, LinesAdded: 4, LinesDeleted: 2}, "other": {SHA: "other", FileCount: 3, LinesAdded: 10}}
				selected := &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}, Section: "history", HistorySelection: []string{sha}, History: a.commits, HistoryFiles: []backend.KernelFile{{ID: "file", Path: "changed.txt", Status: "Modified"}}, HistoryLinesAdded: 4, HistoryLinesDeleted: 2}
				a.coreSnapshot = &backend.KernelSnapshot{Selected: selected, Workspace: &backend.KernelWorkspace{Repository: &ref, HistorySelectionContiguous: true, Menus: []backend.KernelWorkspaceMenu{{Target: backend.KernelWorkspaceMenuTarget{Kind: "commit", ID: sha}, Lease: "lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.copySHA", Enabled: true}}}}}}
				tt := ui.NewTester(func(c *ui.Context) {
					setWorkspaceTheme(c)
					ui.Column(c).FillHeight().Width(float32(size.width) - projectRailWidth).Padding(24).Children(func() { a.workspaceHistoryPage(c) })
				}, size.width, size.height)
				tt.SetLocale(locale)
				tt.Advance(time.Second)
				tx := locales.In(locale)
				for _, label := range []string{"Previous commit", tx.FileCount(1), tx.FileCount(3), workspaceLabel(locale, "workspace.copySHA")} {
					visibleControl(t, tt, label, size.width, size.height)
				}
				if tt.HasText("changed.txt") {
					t.Fatal("file names should remain collapsed until the file count is clicked")
				}
				if err := tt.Click(tx.FileCount(1)); err != nil {
					t.Fatal(err)
				}
				tt.Frame()
				visibleControl(t, tt, "changed.txt", size.width, size.height)
				row, _ := tt.AccessByRole(ui.RoleListItem, "Improve project history")
				count, _ := tt.Find(tx.FileCount(1))
				if count.Y < row.Bounds.Y || count.Y+count.H > row.Bounds.Y+row.Bounds.H {
					t.Fatal("commit information is detached from its history record")
				}
				if a.viewingCommit || tt.HasText(tx.BackToHistory()) || !tt.HasText("Keep the changed files visible.") {
					t.Fatal("history still hides commit content behind a separate detail page")
				}
				if err := tt.Click(workspaceLabel(locale, "workspace.copySHA")); err != nil {
					t.Fatal(err)
				}
				if len(a.coreQueue) != 1 {
					t.Fatal("copy must use the original kernel menu action")
				}
			})
		}
	}
}

func TestVisibleHistoryStatisticsIncludeTheLastRowAndReadOnlyOnce(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.ready, a.tab = true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	a.commits = []backend.Commit{{SHA: "only", Summary: "The only visible row"}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}}, Workspace: &backend.KernelWorkspace{Repository: &ref}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) }) }, 900, 620)
	tt.Advance(time.Second)
	if len(a.coreQueue) != 1 {
		t.Fatal("the final visible row never requested its statistics")
	}
	operation := <-a.coreQueue
	operation.complete(backend.KernelResult{HistorySummaries: []backend.KernelHistorySummary{{SHA: "only", FileCount: 7, LinesAdded: 12}}}, nil)
	tt.Frame() // The real queue invokes completion inside the window's update.
	tt.Advance(time.Second)
	if len(a.coreQueue) != 0 || !tt.HasText(locales.In("en").FileCount(7)) {
		t.Fatalf("statistics were reloaded or not shown: queued=%d data=%+v text=%q", len(a.coreQueue), a.workspaceHistorySummaries, tt.Texts())
	}
}

func TestHistorySelectionUsesASubtleSurface(t *testing.T) {
	for _, dark := range []bool{false, true} {
		state, list := &nativeWorkspaceState{}, &ui.ListState{}
		workspace := &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{Path: "fixture"}}
		tt := ui.NewTester(func(c *ui.Context) {
			setWorkspaceTheme(c)
			ui.Column(c).Fill().Padding(24).Children(func() {
				renderWorkspaceHistoryList(c, state, list, workspace, []backend.Commit{{SHA: "a", Summary: "Selected commit"}}, []string{"a"}, true, func(backend.KernelWorkspaceAction) {}, nil)
			})
		}, 900, 620)
		tt.SetDark(dark)
		tt.Advance(time.Second)
		row, ok := tt.AccessByRole(ui.RoleListItem, "Selected commit")
		if !ok {
			t.Fatal("history selection missing")
		}
		pixels := tt.Image()
		selected := pixels.RGBAAt(int(row.Bounds.X+row.Bounds.W-12), int(row.Bounds.Y+8))
		canvas := pixels.RGBAAt(12, 12)
		if selected == canvas || math.Abs(float64(selected.R)-float64(canvas.R)) > 16 || math.Abs(float64(selected.G)-float64(canvas.G)) > 16 || math.Abs(float64(selected.B)-float64(canvas.B)) > 16 {
			t.Fatalf("dark=%v: selection should use a subtle surface: canvas=%v selected=%v", dark, canvas, selected)
		}
	}
}

func TestHistoryReviewExposesOriginalFullSHAActionOnlyForOneCommit(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.ready, a.viewingCommit, a.tab = true, true, historyTab
	sha := "1234567890abcdef1234567890abcdef12345678"
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	selected := &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}, HistorySelection: []string{sha}, History: []backend.Commit{{SHA: sha, ShortSHA: "1234567", Summary: "Selected commit"}}}
	menu := backend.KernelWorkspaceMenu{Target: backend.KernelWorkspaceMenuTarget{Kind: "commit", ID: sha}, Lease: "current-lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.copySHA", Enabled: true}}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: selected, Workspace: &backend.KernelWorkspace{Repository: &ref, HistorySelectionContiguous: true, Menus: []backend.KernelWorkspaceMenu{menu}}}
	a.commits = selected.History
	a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{sha: {SHA: sha}}
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Padding(24).Children(func() { a.workspaceHistoryPage(c) })
	}, 900, 620)
	tt.Advance(time.Second)
	label := workspaceLabel("en", "workspace.copySHA")
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if len(a.coreQueue) != 1 {
		t.Fatal("copy did not route through the original kernel command queue")
	}
	menu.Items[0].Enabled = false
	a.coreSnapshot.Workspace.Menus[0] = menu
	tt.Frame()
	button, found := tt.AccessByRole(ui.RoleButton, label)
	if !found || !button.Disabled {
		t.Fatal("copy ignored the original action's disabled state")
	}
}

func TestWorkspaceHistoryReviewUsesCombinedSelectionAndOriginalFileMenu(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.ready, a.viewingCommit, a.tab = true, true, historyTab
	a.workspaceHistoryCombinedFilesOpen = true
	ref := backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 1, Path: "fixture"}}
	selected := &backend.KernelSelected{Repository: ref, Section: "history", HistorySelection: []string{"a", "b"}, History: []backend.Commit{{SHA: "a", Summary: "First commit only"}, {SHA: "b", Summary: "Second commit only"}}, HistoryFiles: []backend.KernelFile{{ID: "file", Path: "changed.txt", Status: "Modified"}}, HistoryLinesAdded: 2, HistoryLinesDeleted: 1}
	w := &backend.KernelWorkspace{Repository: &ref.KernelRepositoryReference, HistorySelectionContiguous: true, Menus: []backend.KernelWorkspaceMenu{{Target: backend.KernelWorkspaceMenuTarget{Kind: "historyFile", ID: "file"}, Lease: "fixture-lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.copyPath", Enabled: true}}}}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: selected, Workspace: w}
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Padding(24).Children(func() {
			if !a.workspaceHistoryReview(c) {
				t.Fatal("native history review was not handled")
			}
		})
	}, 900, 620)
	tt.Advance(time.Second)
	if !tt.HasText(workspaceLabel("en", "workspace.historySelection", "2")) || tt.HasText("First commit only") {
		t.Fatal("combined diff is mislabeled as the first commit")
	}
	if err := tt.RightClick(locales.In("en").OpenFile("changed.txt")); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tt.Menu(), workspaceLabel("en", "workspace.copyPath")) {
		t.Fatalf("original history file action was not presented: %v", tt.Menu())
	}
	tt.CloseMenu()
	w.HistorySelectionContiguous = false
	tt.Frame()
	if !tt.HasText(workspaceLabel("en", "workspace.nonContiguous")) || tt.HasText("changed.txt") {
		t.Fatal("non-contiguous commits displayed a misleading combined file diff")
	}
}

func TestWorkspaceHistoryOverviewRetainsLoadMore(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.ready, a.historyLoaded, a.historyMore, a.tab = true, true, true, historyTab
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{}, Workspace: &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{ID: 1, Path: "fixture"}, Compare: backend.KernelWorkspaceCompare{Mode: "history"}}}
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) })
	}, 900, 620)
	tt.Frame()
	if !tt.HasText(locales.In("en").LoadMore()) || !tt.HasText(locales.In("en").NoHistory()) {
		t.Fatal("native history early return lost original empty/load-more controls")
	}
	a.loadingHistory = true
	tt.Frame()
	if tt.HasText(locales.In("en").NoHistory()) {
		t.Fatal("loading history is mislabeled as an empty history")
	}
	a.loadingHistory = false
	a.coreSnapshot.Workspace.Compare.Mode = "ahead"
	tt.Frame()
	if tt.HasText(locales.In("en").LoadMore()) {
		t.Fatal("a complete comparison offers inappropriate history pagination")
	}
}

func TestWorkspaceHistoryPointerAndKeyboardUpdateTheSameSelection(t *testing.T) {
	state := &nativeWorkspaceState{workspaceHistoryCursor: -1}
	list := &ui.ListState{}
	workspace := &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{ID: 1, Path: "fixture"}}
	commits := []backend.Commit{{SHA: "a", Summary: "First commit"}, {SHA: "b", Summary: "Second commit"}, {SHA: "c", Summary: "Third commit"}}
	var selected []string
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() {
			renderWorkspaceHistoryList(c, state, list, workspace, commits, selected, true, func(action backend.KernelWorkspaceAction) {
				if action.Type != "history.select" {
					t.Fatalf("unexpected navigation command: %+v", action)
				}
				selected = slices.Clone(action.SHAs)
			}, nil)
		})
	}, 900, 620)
	tt.Advance(time.Second)
	if err := tt.Click("First commit"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selected, []string{"a"}) {
		t.Fatalf("plain history click selected wrong commit: %v", selected)
	}
	if err := tt.ClickWith(ui.Cmd, "Second commit"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selected, []string{"a", "b"}) {
		t.Fatalf("modifier click lost selection: %v", selected)
	}
	if err := tt.ClickWith(ui.Shift, "Third commit"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(selected, "b") || !slices.Contains(selected, "c") {
		t.Fatalf("shift selection lost range: %v", selected)
	}
	tt.Key(0, ui.KeyUp)
	if !slices.Equal(selected, []string{"b"}) {
		t.Fatalf("arrow selected wrong commit: %v", selected)
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(selected, []string{"b"}) {
		t.Fatal("Enter changed the visible selection")
	}
}

func clickHistoryRecordTitle(t *testing.T, tt *ui.Tester, title string, mods ui.Modifiers) {
	t.Helper()
	// The complete record has the same label as its title. Its center can be
	// selectable description text, so locate the rendered title before clicking.
	text, ok := tt.AccessByRole(ui.RoleText, title)
	if !ok || text.Offscreen || text.Bounds.W <= 0 || text.Bounds.H <= 0 {
		t.Fatalf("history title is not visible: %+v", text)
	}
	tt.ClickAtWith(mods, text.Bounds.X+text.Bounds.W/2, text.Bounds.Y+text.Bounds.H/2)
}

func TestHistoryRecordTitleSelectsWithSelectableDescription(t *testing.T) {
	state := &nativeWorkspaceState{workspaceHistoryCursor: -1}
	list := &ui.ListState{}
	workspace := &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{ID: 1, Path: "fixture"}}
	body := "Review every selected change.\nGeneration only fills the draft.\nKeep the original author and recovery behavior."
	commits := []backend.Commit{{SHA: "a", Summary: "Commit with description", Body: body}, {SHA: "b", Summary: "Second described commit", Body: body}}
	var selected []string
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() {
			renderWorkspaceHistoryList(c, state, list, workspace, commits, selected, true, func(action backend.KernelWorkspaceAction) {
				if action.Type != "history.select" {
					t.Fatalf("unexpected history action: %+v", action)
				}
				selected = slices.Clone(action.SHAs)
			}, nil)
		})
	}, 900, 620)
	tt.Advance(time.Second)
	// Unselected, the description reads as one line.
	if !tt.HasText(strings.ReplaceAll(body, "\n", " ")) {
		t.Fatal("the actual history description is missing")
	}
	clickHistoryRecordTitle(t, tt, commits[0].Summary, 0)
	if !slices.Equal(selected, []string{"a"}) {
		t.Fatalf("title click did not select its record: %v", selected)
	}
	clickHistoryRecordTitle(t, tt, commits[1].Summary, ui.Cmd)
	if !slices.Equal(selected, []string{"a", "b"}) {
		t.Fatalf("modifier title click did not preserve the first record: %v", selected)
	}
}
