package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// designEvidenceApp builds the production view over a deterministic kernel
// snapshot. It exists for layout assertions and opt-in design renders; real
// repository behavior stays covered by the kernel integration tests.
func designEvidenceApp(t *testing.T, now time.Time) *app {
	t.Helper()
	a := newApp(nil, nil)
	t.Cleanup(a.close)
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 64)
	a.ready, a.historyLoaded, a.historyMore, a.tab = true, true, true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture/travel-notes"}
	repository := backend.KernelRepository{KernelRepositoryReference: ref, Name: "travel-notes"}
	a.repo = &backend.Repository{ID: 1, Path: ref.Path, Name: repository.Name}
	stamp := func(age time.Duration) string { return now.Add(-age).Format(time.RFC3339) }
	a.commits = []backend.Commit{
		{SHA: "9f2c1ab4d05e6f708192a3b4c5d6e7f809112233", ShortSHA: "9f2c1ab", Summary: "调整湖区两日行程，补充雨天备选路线", Body: "第一天改为环湖步行，第二天保留山顶观景台。\n雨天时改走室内展馆与旧城街区，交通时间不超过 40 分钟。\n同步更新预算表与装备清单。", AuthorName: "林清", AuthorEmail: "lin@example.invalid", Date: stamp(2 * time.Hour), Tags: []string{"v1.4.0"}},
		{SHA: "4aabcd8e1f20314253647586978a9bacbdcedf01", ShortSHA: "4aabcd8", Summary: "Fix currency rounding in the budget table", AuthorName: "Morgan Reyes", AuthorEmail: "morgan@example.invalid", Date: stamp(5 * time.Hour)},
		{SHA: "14b9eae77aa0b1c2d3e4f5061728394a5b6c7d8e", ShortSHA: "14b9eae", Summary: "Record the long weekend route with transport and walking details", Body: "Keep the description available through natural scrolling in the history list. Empty commits still show an accurate zero-file count, and a binary attachment is recorded as a binary change.\n\nKeyboard exercise: select this record, move to the next record and return.", AuthorName: "Gitowl Acceptance", AuthorEmail: "acceptance@example.invalid", Date: stamp(26 * time.Hour)},
		{SHA: "77c0ffee00112233445566778899aabbccddeeff", ShortSHA: "77c0ffe", Summary: "新增装备清单", AuthorName: "林清", AuthorEmail: "lin@example.invalid", Date: stamp(27 * time.Hour)},
		{SHA: "0d15ea5e00112233445566778899aabbccddeeff", ShortSHA: "0d15ea5", Summary: "Initial itinerary draft", Body: "Outline both days and list the open questions.", AuthorName: "Morgan Reyes", AuthorEmail: "morgan@example.invalid", Date: stamp(9 * 24 * time.Hour)},
	}
	a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{}
	for i, commit := range a.commits {
		a.workspaceHistorySummaries[commit.SHA] = backend.KernelHistorySummary{SHA: commit.SHA, FileCount: []int{3, 1, 2, 4, 5}[i], LinesAdded: 12 * (i + 1), LinesDeleted: 3 * i}
	}
	var menus []backend.KernelWorkspaceMenu
	for _, commit := range a.commits {
		menus = append(menus, backend.KernelWorkspaceMenu{Target: backend.KernelWorkspaceMenuTarget{Kind: "commit", ID: commit.SHA}, Lease: "lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.copySHA", Enabled: true}}})
	}
	branch := "main"
	a.coreSnapshot = &backend.KernelSnapshot{
		Repositories: []backend.KernelRepository{repository, {KernelRepositoryReference: backend.KernelRepositoryReference{ID: 2, Path: "fixture/site"}, Name: "site"}},
		Selected: &backend.KernelSelected{Repository: repository, Section: "history", Branch: &branch, HasRemote: true, History: a.commits,
			HistoryFiles: []backend.KernelFile{{ID: "a", Path: "docs/routes/lake-district.md", Status: "Modified"}, {ID: "b", Path: "budget.csv", Status: "New"}, {ID: "c", Path: "archive/old-route.md", Status: "Deleted"}}},
		Workspace: &backend.KernelWorkspace{Repository: &ref, HistorySelectionContiguous: true, Compare: backend.KernelWorkspaceCompare{Mode: "history"}, Menus: menus},
	}
	return a
}

func saveDesignEvidence(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("GITOWL_NATIVE_UI_ARTIFACTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, tt.Image())
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

// The history page keeps every record's controls inside the window in each
// language, appearance and supported size. With GITOWL_NATIVE_UI_ARTIFACTS set
// it also writes the production render for design review.
func TestHistoryPageLayoutAcrossLanguagesAppearancesAndSizes(t *testing.T) {
	now := time.Now()
	for _, size := range [][2]int{{900, 620}, {1180, 760}, {1920, 1080}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/dark=%v", size[0], size[1], locale, dark), func(t *testing.T) {
					a := designEvidenceApp(t, now)
					tt := ui.NewTester(a.view, size[0], size[1])
					tt.SetTitleBar(ui.TitleBar{Height: 48, Right: 138})
					tt.SetLocale(locale)
					tt.SetDark(dark)
					tt.Advance(time.Second)
					tx := locales.In(locale)
					for _, label := range []string{a.commits[0].Summary, a.commits[1].Summary, tx.FileCount(3), workspaceLabel(locale, "workspace.copySHA")} {
						visibleControl(t, tt, label, size[0], size[1])
					}
					saveDesignEvidence(t, tt, fmt.Sprintf("history-%s-%d-dark-%v.png", locale, size[0], dark))
					// Expansion by click is covered by the history tests. Set the state
					// directly: this fixture has no kernel process to receive focus reports.
					a.coreSnapshot.Selected.HistorySelection = []string{a.commits[0].SHA}
					a.workspaceHistoryFilesSHA = a.commits[0].SHA
					tt.Frame()
					tt.Advance(300 * time.Millisecond)
					visibleControl(t, tt, tx.OpenFile("budget.csv"), size[0], size[1])
					saveDesignEvidence(t, tt, fmt.Sprintf("history-expanded-%s-%d-dark-%v.png", locale, size[0], dark))
				})
			}
		}
	}
}

// Day headings are list rows too. They must not be selectable, must not shift
// which commit an arrow key reaches, and must not be asked for statistics.
func TestHistoryDayHeadingsKeepSelectionAndStatisticsOnCommits(t *testing.T) {
	now := time.Now()
	a := newApp(nil, nil)
	defer a.close()
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 8)
	a.ready, a.tab = true, historyTab
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture"}
	a.commits = []backend.Commit{
		{SHA: "a", Summary: "Newest commit", Date: now.Format(time.RFC3339)},
		{SHA: "b", Summary: "Older commit", Date: now.AddDate(0, 0, -3).Format(time.RFC3339)},
	}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref}}, Workspace: &backend.KernelWorkspace{Repository: &ref}}
	var selected []string
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		ui.Column(c).Fill().Children(func() {
			renderWorkspaceHistoryList(c, &a.nativeWorkspaceState, a.commitsList.list(), a.coreSnapshot.Workspace, a.commits, selected, true, func(action backend.KernelWorkspaceAction) {
				selected = action.SHAs
			}, nil)
			a.loadVisibleHistorySummaries()
		})
	}, 900, 620)
	tt.Advance(time.Second)
	if !tt.HasText("Today") {
		t.Fatalf("the newest day has no heading: %q", tt.Texts())
	}
	if len(a.coreQueue) != 1 {
		t.Fatalf("statistics requests: %d", len(a.coreQueue))
	}
	if err := tt.Click("Newest commit"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if len(selected) != 1 || selected[0] != "b" {
		t.Fatalf("Down crossed the day heading to the wrong record: %v", selected)
	}
}
