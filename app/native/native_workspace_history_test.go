package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func BenchmarkWorkspaceHistoryFrames(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			commits := make([]backend.Commit, count)
			for i := range commits {
				commits[i] = backend.Commit{SHA: fmt.Sprintf("%040x", i), Summary: fmt.Sprintf("History record %d", i), AuthorName: "Fixture", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)}
			}
			state, list := &nativeWorkspaceState{}, &ui.ListState{}
			workspace := &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{Path: "fixture"}}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() {
					renderWorkspaceHistoryList(c, state, list, workspace, nil, commits, nil, true, func(backend.KernelWorkspaceAction) {}, nil)
				})
			}, 900, 620)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tt.Frame()
			}
		})
	}
}

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
	stdout, output := io.Pipe()
	input, stdin := io.Pipe()
	core := backend.NewKernel(stdout, stdin, nil)
	a := newCoreApp(core, nil)
	defer func() { a.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() }()
	updates := make(chan func(), 8)
	a.update = func(fn func()) { updates <- fn }
	a.ready, a.tab = true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	a.commits = []backend.Commit{{SHA: "only", Summary: "The only visible row", Date: time.Now().Format(time.RFC3339)}}
	a.repo = &backend.Repository{ID: 1, Path: "fixture", Name: "Fixture"}
	a.coreSnapshot = &backend.KernelSnapshot{
		Protocol: 1, Core: "desktop/desktop", Revision: 1,
		Accounts: []backend.KernelAccount{}, Repositories: []backend.KernelRepository{}, Popups: []backend.KernelPopup{},
		Selected:  &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}, Section: "history", History: a.commits, Files: []backend.KernelFile{}},
		Workspace: &backend.KernelWorkspace{Repository: &ref},
	}
	requests := make(chan []byte, 1)
	go func() { data, _ := bufio.NewReader(input).ReadBytes('\n'); requests <- data }()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) }) }, 900, 620)
	tt.Advance(time.Second)
	var request struct {
		ID     int `json:"id"`
		Params struct {
			Name string   `json:"name"`
			SHAs []string `json:"shas"`
		} `json:"params"`
	}
	select {
	case data := <-requests:
		if err := json.Unmarshal(data, &request); err != nil || request.Params.Name != "history.summaries" || !slices.Equal(request.Params.SHAs, []string{"only"}) {
			t.Fatalf("unexpected visible statistics request: %s / %v", data, err)
		}
	case <-time.After(time.Second):
		t.Fatal("the final visible row never requested its statistics")
	}
	// Keep the statistics response pending. An ordinary user action must still
	// reach the original operation queue without waiting for metadata reads.
	acted := make(chan struct{})
	a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) {
		close(acted)
		return backend.KernelResult{}, errors.New("test I/O boundary")
	}})
	select {
	case <-acted:
	case <-time.After(time.Second):
		t.Fatal("history statistics blocked the user's operation queue")
	}
	result := backend.KernelResult{Snapshot: *a.coreSnapshot, HistorySummaries: []backend.KernelHistorySummary{{SHA: "only", FileCount: 7, LinesAdded: 12}}}
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = output.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	for a.corePending > 0 {
		select {
		case apply := <-updates:
			apply()
		case <-time.After(time.Second):
			t.Fatal("statistics completion did not reach the native window")
		}
	}
	tt.Frame()
	tt.Advance(time.Second)
	if a.corePending != 0 || !tt.HasText(locales.In("en").FileCount(7)) {
		t.Fatalf("statistics were reloaded or not shown: pending=%d data=%+v text=%q", a.corePending, a.workspaceHistorySummaries, tt.Texts())
	}
}

func TestHistorySelectionUsesASubtleSurface(t *testing.T) {
	for _, dark := range []bool{false, true} {
		state, list := &nativeWorkspaceState{}, &ui.ListState{}
		workspace := &backend.KernelWorkspace{Repository: &backend.KernelRepositoryReference{Path: "fixture"}}
		tt := ui.NewTester(func(c *ui.Context) {
			setWorkspaceTheme(c)
			ui.Column(c).Fill().Padding(24).Children(func() {
				renderWorkspaceHistoryList(c, state, list, workspace, nil, []backend.Commit{{SHA: "a", Summary: "Selected commit"}}, []string{"a"}, true, func(backend.KernelWorkspaceAction) {}, nil)
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

func TestWorkspaceHistoryLoadsNearTheBottomLikeUpstream(t *testing.T) {
	for _, mode := range []string{"history", "ahead", "behind"} {
		t.Run(mode, func(t *testing.T) {
			a := newApp(nil, nil)
			defer a.close()
			a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
			a.ready, a.historyLoaded, a.historyMore, a.tab = true, true, true, historyTab
			ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
			a.repo = &backend.Repository{ID: 1, Path: ref.Path, Name: "Fixture"}
			a.workspaceHistorySummaries = make(map[string]backend.KernelHistorySummary)
			for i := 0; i < 60; i++ {
				sha := fmt.Sprintf("%040x", i)
				a.commits = append(a.commits, backend.Commit{SHA: sha, Summary: fmt.Sprintf("Commit %d", i), Date: time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)})
				a.workspaceHistorySummaries[sha] = backend.KernelHistorySummary{SHA: sha}
			}
			a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}}, Workspace: &backend.KernelWorkspace{Repository: &ref, Compare: backend.KernelWorkspaceCompare{Mode: mode}}}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) })
			}, 900, 620)
			tt.Advance(time.Second)
			if len(a.coreQueue) != 0 {
				t.Fatal("history loaded older commits before scrolling near the bottom")
			}
			a.commitsList.list().ScrollIntoView(len(a.workspaceHistoryRows) - 1)
			tt.Frame()
			tt.Advance(time.Second)
			if mode != "history" {
				if len(a.coreQueue) != 0 {
					t.Fatal("a complete comparison requested another history batch")
				}
				return
			}
			if len(a.coreQueue) != 1 || !a.loadingHistory {
				t.Fatal("scrolling near the bottom must load exactly one batch through the original kernel")
			}
			tt.Advance(time.Second)
			if len(a.coreQueue) != 1 {
				t.Fatal("scroll frames repeated the pending history request")
			}
		})
	}
}

func TestShortHistoryDoesNotPaginateOnActivation(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.ready, a.historyLoaded, a.historyMore, a.tab = true, true, true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	a.repo = &backend.Repository{ID: 1, Path: ref.Path}
	a.commits = []backend.Commit{{SHA: "a", Summary: "Initial commit"}}
	a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{"a": {}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}}, Workspace: &backend.KernelWorkspace{Repository: &ref, Compare: backend.KernelWorkspaceCompare{Mode: "history"}}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) })
	}, 900, 620)
	tt.Advance(time.Second)
	if len(a.coreQueue) != 0 {
		t.Fatal("activating a short history requested a batch without scrolling")
	}
	// Focus/redraw is not a request for older history. The explicit control
	// still works when the entire list fits in the viewport.
	tt.Key(0, ui.KeyTab)
	tt.Advance(time.Second)
	if len(a.coreQueue) != 0 {
		t.Fatal("focus frames requested older history")
	}
	if err := tt.Click(locales.In("en").LoadMore()); err != nil {
		t.Fatal(err)
	}
	if len(a.coreQueue) != 1 || !a.loadingHistory {
		t.Fatal("explicitly loading older commits did not reach the original kernel")
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
			renderWorkspaceHistoryList(c, state, list, workspace, nil, commits, selected, true, func(action backend.KernelWorkspaceAction) {
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

func TestHistorySelectionKeepsTheLatestClickWhileKernelReplies(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.ready, a.tab = true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	a.repo = &backend.Repository{ID: 1, Path: ref.Path}
	a.commits = []backend.Commit{{SHA: "a", Summary: "First commit"}, {SHA: "b", Summary: "Second commit"}, {SHA: "c", Summary: "Third commit"}}
	a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{"a": {}, "b": {}, "c": {}}
	selected := &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}, HistorySelection: []string{"a"}}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: selected, Workspace: &backend.KernelWorkspace{Repository: &ref}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.workspaceHistoryList(c) }) }, 900, 620)
	tt.Frame()
	for _, label := range []string{"Second commit", "Third commit"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if a.workspaceHistorySelectionPending != 2 {
		t.Fatal("both clicks must still use the original ordered kernel actions")
	}
	// The first click's reply arrives while the second one is still pending.
	a.workspaceHistorySelectionPending--
	selected.HistorySelection = []string{"b"}
	tt.Frame()
	if !a.workspaceHistorySelection.Has("c") || a.workspaceHistorySelection.Has("b") {
		t.Fatal("an earlier kernel reply moved the visible selection back")
	}
	a.workspaceHistorySelectionPending--
	selected.HistorySelection = []string{"c"}
	tt.Frame()
	if !a.workspaceHistorySelection.Has("c") {
		t.Fatal("the completed original selection was not preserved")
	}
	// Once settled, an external selection change still follows the kernel.
	selected.HistorySelection = []string{"a"}
	tt.Frame()
	if !a.workspaceHistorySelection.Has("a") || a.workspaceHistorySelection.Has("c") {
		t.Fatal("the view stopped following the original kernel after settling")
	}
	if err := tt.Click("Second commit"); err != nil {
		t.Fatal(err)
	}
	for len(a.coreQueue) > 1 {
		<-a.coreQueue
	}
	failed := <-a.coreQueue
	failed.complete(backend.KernelResult{}, errors.New("selection failed"))
	tt.Frame()
	if !a.workspaceHistorySelection.Has("a") || a.workspaceHistorySelection.Has("b") {
		t.Fatal("a failed selection did not restore the original kernel selection")
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
			renderWorkspaceHistoryList(c, state, list, workspace, nil, commits, selected, true, func(action backend.KernelWorkspaceAction) {
				if action.Type != "history.select" {
					t.Fatalf("unexpected history action: %+v", action)
				}
				selected = slices.Clone(action.SHAs)
			}, nil)
		})
	}, 900, 620)
	tt.Advance(time.Second)
	// A compact preview retains the complete accessible description.
	if !tt.HasText(body) {
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

// Selecting a record is a history operation, not consent to expand a long
// description. Expansion is a separate, keyboard-accessible control.
func TestHistoryDescriptionExpandsOnlyWhenRequested(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			a := designEvidenceApp(t, time.Now(), "history")
			a.commits[0].Body = strings.Repeat("Keep each change with its author and its files.\n", 16)
			a.coreSnapshot.Selected.History = a.commits
			tt := ui.NewTester(func(c *ui.Context) {
				setWorkspaceTheme(c)
				ui.Column(c).Width(620).FillHeight().Padding(24).Children(func() { a.workspaceHistoryPage(c) })
			}, 900, 620)
			tt.SetLocale(locale)
			tt.Advance(time.Second)
			before, ok := tt.AccessByRole(ui.RoleListItem, a.commits[0].Summary)
			if !ok {
				t.Fatal("history record missing")
			}
			a.coreSnapshot.Selected.HistorySelection = []string{a.commits[0].SHA}
			tt.Frame()
			after, _ := tt.AccessByRole(ui.RoleListItem, a.commits[0].Summary)
			if after.Bounds.H != before.Bounds.H {
				t.Fatalf("selection expanded the description: before=%v after=%v", before.Bounds, after.Bounds)
			}
			expand := workspaceLabel(locale, "workspace.historyShowDescription")
			if err := tt.Click(expand); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			expanded, _ := tt.AccessByRole(ui.RoleListItem, a.commits[0].Summary)
			if expanded.Bounds.H <= after.Bounds.H || !tt.HasText(strings.TrimSpace(a.commits[0].Body)) {
				t.Fatal("explicit expansion did not expose the full description")
			}
			// The collapse control remains at the start of the description,
			// reachable even when the full text is taller than the viewport.
			if err := tt.Click(workspaceLabel(locale, "workspace.historyHideDescription")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			collapsed, _ := tt.AccessByRole(ui.RoleListItem, a.commits[0].Summary)
			if collapsed.Bounds.H != before.Bounds.H {
				t.Fatal("collapsing the description did not restore its compact record")
			}
		})
	}
}

func TestHistorySyncLabelsDescribeReachabilityWithoutInventingPushOrPullEvents(t *testing.T) {
	for _, scenario := range []struct {
		name, mode, ref, label string
		remote, local          bool
	}{
		{"unpushed", "history", "", "workspace.historyPendingPush", true, true},
		{"on remote", "history", "", "workspace.historyOnRemote", true, false},
		{"no remote", "history", "", "workspace.historyLocalOnly", false, true},
		{"upstream behind", "behind", "origin/main", "workspace.historyPendingPull", true, false},
		{"local branch comparison", "behind", "feature", "workspace.historyComparison", true, false},
		{"other remote comparison", "behind", "upstream/main", "workspace.historyComparison", true, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := designEvidenceApp(t, time.Now(), "history")
			a.commits = a.commits[:1]
			a.coreSnapshot.Selected.HasRemote = scenario.remote
			w := a.coreSnapshot.Workspace
			w.LocalCommitSHAs = nil
			if scenario.local {
				w.LocalCommitSHAs = []string{a.commits[0].SHA}
			}
			w.Compare.Mode, w.Compare.Ref = scenario.mode, &scenario.ref
			tt := designEvidenceTester(a, 900, 620, "zh-Hans", false)
			label := workspaceLabel("zh-Hans", scenario.label)
			tag := visibleControl(t, tt, label, 900, 620)
			row, _ := tt.AccessByRole(ui.RoleListItem, a.commits[0].Summary)
			if tag.Y < row.Bounds.Y || tag.Y+tag.H > row.Bounds.Y+row.Bounds.H {
				t.Fatal("sync state is detached from the commit it describes")
			}
		})
	}
}
