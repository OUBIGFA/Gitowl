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

// The patch of the review scene: two hunks, additions and deletions, a
// Chinese line and a long one, as a real working-tree diff returns them.
const designEvidencePatch = "diff --git a/docs/routes/lake-district.md b/docs/routes/lake-district.md\n" +
	"--- a/docs/routes/lake-district.md\n+++ b/docs/routes/lake-district.md\n" +
	"@@ -3,6 +3,7 @@ ## 第一天\n" +
	" 早上从车站出发，沿湖步行到观景台。\n" +
	"-午饭在码头附近的小店。\n" +
	"+午饭改到旧城街区，留出半小时休息。\n" +
	"+雨天备选：室内展馆与旧城街区，交通时间不超过 40 分钟。\n" +
	" 下午乘船返回，傍晚整理照片。\n" +
	" \n" +
	" ## 第二天\n" +
	"-山顶观景台视天气决定。\n" +
	"+山顶观景台保留，出发前一小时确认天气。\n" +
	"@@ -21,3 +22,3 @@ ## 预算\n" +
	" 交通：往返车票与船票。\n" +
	"-住宿：一晚湖边民宿。\n" +
	"+住宿：一晚湖边民宿，含早餐；若改期，提前两天联系房东以免产生取消费用，并同步更新预算表。\n" +
	" 餐饮：按每人每日标准估算。\n"

// designEvidenceApp builds the production view over a deterministic kernel
// snapshot. It exists for layout assertions and opt-in design renders; real
// repository behavior stays covered by the kernel integration tests.
func designEvidenceApp(t *testing.T, now time.Time, section string) *app {
	t.Helper()
	a := newApp(nil, nil)
	t.Cleanup(a.close)
	a.core, a.coreQueue = &backend.Kernel{}, make(chan nativeOperation, 64)
	a.ready, a.commitCapable, a.historyLoaded, a.historyMore = true, true, true, true
	a.tab = changesTab
	if section == "history" {
		a.tab = historyTab
	}
	ref := backend.KernelRepositoryReference{ID: 1, Path: "fixture/travel-notes"}
	two, one, zero := 2, 1, 0
	repository := backend.KernelRepository{KernelRepositoryReference: ref, Name: "travel-notes", Kind: "repository", Local: &backend.KernelRepositoryLocal{Ahead: &two, Behind: &one, ChangedFiles: 4}}
	a.repo = &backend.Repository{ID: 1, Path: ref.Path, Name: repository.Name}
	stamp := func(age time.Duration) string { return now.Add(-age).Format(time.RFC3339) }
	a.commits = []backend.Commit{
		{SHA: "9f2c1ab4d05e6f708192a3b4c5d6e7f809112233", ShortSHA: "9f2c1ab", Summary: "调整湖区两日行程，补充雨天备选路线", Body: "第一天改为环湖步行，第二天保留山顶观景台。\n雨天时改走室内展馆与旧城街区，交通时间不超过 40 分钟。\n同步更新预算表与装备清单。", AuthorName: "林清", AuthorEmail: "lin@example.invalid", Date: stamp(2 * time.Hour), Tags: []string{"v1.4.0"}},
		{SHA: "4aabcd8e1f20314253647586978a9bacbdcedf01", ShortSHA: "4aabcd8", Summary: "Fix currency rounding in the budget table", AuthorName: "Morgan Reyes", AuthorEmail: "morgan@example.invalid", Date: stamp(5 * time.Hour)},
		{SHA: "14b9eae77aa0b1c2d3e4f5061728394a5b6c7d8e", ShortSHA: "14b9eae", Summary: "Record the long weekend route with transport and walking details", Body: "Keep the description available through natural scrolling in the history list. Empty commits still show an accurate zero-file count, and a binary attachment is recorded as a binary change.\n\nKeyboard exercise: select this record, move to the next record and return.", AuthorName: "Gitowl Acceptance", AuthorEmail: "acceptance@example.invalid", Date: stamp(26 * time.Hour)},
		{SHA: "77c0ffee00112233445566778899aabbccddeeff", ShortSHA: "77c0ffe", Summary: "新增装备清单", AuthorName: "林清", AuthorEmail: "lin@example.invalid", Date: stamp(27 * time.Hour)},
		{SHA: "0d15ea5e00112233445566778899aabbccddeeff", ShortSHA: "0d15ea5", Summary: "Initial itinerary draft", Body: "Outline both days and list the open questions.", AuthorName: "Morgan Reyes", AuthorEmail: "morgan@example.invalid", Date: stamp(9 * 24 * time.Hour), Tags: []string{"v1.0.0", "itinerary-first", "shared"}},
	}
	a.workspaceHistorySummaries = map[string]backend.KernelHistorySummary{}
	for i, commit := range a.commits {
		a.workspaceHistorySummaries[commit.SHA] = backend.KernelHistorySummary{SHA: commit.SHA, FileCount: []int{3, 1, 2, 4, 5}[i], LinesAdded: 12 * (i + 1), LinesDeleted: 3 * i}
	}
	var menus []backend.KernelWorkspaceMenu
	for _, commit := range a.commits {
		menus = append(menus, backend.KernelWorkspaceMenu{Target: backend.KernelWorkspaceMenuTarget{Kind: "commit", ID: commit.SHA}, Lease: "lease", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.copySHA", Enabled: true}}})
	}
	files := []backend.KernelFile{
		{ID: "a", Path: "docs/routes/lake-district.md", Status: "Modified", Inclusion: "all"},
		{ID: "b", Path: "budget.csv", Status: "New", Inclusion: "all"},
		{ID: "c", Path: "archive/old-route.md", Status: "Deleted", Inclusion: "none"},
		{ID: "d", Path: "photos/day-one/lakeside-viewpoint.jpg", Status: "Modified", Inclusion: "none"},
	}
	a.status = &backend.Status{Branch: ptr("main"), Ahead: 2, Behind: 1}
	a.included = map[string]bool{}
	for _, file := range files {
		a.status.Files = append(a.status.Files, backend.File{Path: file.Path, Status: file.Status})
		if file.Inclusion != "none" {
			a.included[file.Path] = true
		}
	}
	a.commitSummary = "调整湖区两日行程"
	enabled := func(id string) backend.KernelMenuItem {
		return backend.KernelMenuItem{ID: id, Enabled: true, Visible: true}
	}
	sectionName := "changes"
	if section == "history" {
		sectionName = "history"
	}
	a.coreSnapshot = &backend.KernelSnapshot{
		Menu: backend.KernelMenuDTO{Items: []backend.KernelMenuItem{{ID: "repository", Enabled: true, Visible: true, Items: []backend.KernelMenuItem{enabled("fetch"), enabled("pull"), enabled("push")}}}},
		Repositories: []backend.KernelRepository{repository,
			{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 2, Path: "fixture/site"}, Name: "site", Kind: "repository", Local: &backend.KernelRepositoryLocal{Ahead: &zero, Behind: &zero}},
			{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 3, Path: "fixture/photo-archive-with-a-long-project-name"}, Name: "photo-archive-with-a-long-project-name", Kind: "repository", Local: &backend.KernelRepositoryLocal{ChangedFiles: 12}},
		},
		CommitGeneration: &backend.KernelGenerationSettings{Source: "copilot", Language: "auto"},
		Selected: &backend.KernelSelected{Repository: repository, Section: sectionName, Branch: ptr("main"), Upstream: ptr("origin/main"), HasRemote: true, Ahead: 2, Behind: 1, History: a.commits, Files: files, GenerationAvailable: true,
			HistoryFiles: []backend.KernelFile{{ID: "a", Path: "docs/routes/lake-district.md", Status: "Modified"}, {ID: "b", Path: "budget.csv", Status: "New"}, {ID: "c", Path: "archive/old-route.md", Status: "Deleted"}}},
		Workspace: &backend.KernelWorkspace{Repository: &ref, HistorySelectionContiguous: true, Compare: backend.KernelWorkspaceCompare{Mode: "history"}, Menus: menus,
			VisibleFileIDs: []string{"a", "b", "c", "d"}, LocalCommitSHAs: []string{a.commits[0].SHA, a.commits[1].SHA}, TagsToPush: []string{"v1.4.0"}},
	}
	if section == "review" {
		text := designEvidencePatch
		p, err := preparePreview(backend.Diff{Kind: "text", Text: &text}, files[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		a.preview, a.reviewingFile, a.changeIndex = p, true, 0
	}
	if section == "clean" {
		a.status.Files, a.included = nil, map[string]bool{}
		a.coreSnapshot.Selected.Files, a.coreSnapshot.Workspace.VisibleFileIDs = nil, nil
		a.commitSummary = ""
	}
	return a
}

func ptr[T any](value T) *T { return &value }

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

func designEvidenceTester(a *app, width, height int, locale string, dark bool) *ui.Tester {
	tt := ui.NewTester(a.view, width, height)
	tt.SetTitleBar(ui.TitleBar{Height: 48, Right: 138})
	tt.SetLocale(locale)
	tt.SetDark(dark)
	tt.Advance(time.Second)
	return tt
}

var designEvidenceSizes = [][2]int{{900, 620}, {1180, 760}, {1920, 1080}}

// Every page keeps its controls inside the window in each language,
// appearance and supported size. With GITOWL_NATIVE_UI_ARTIFACTS set it also
// writes the production render for design review.
func TestHistoryPageLayoutAcrossLanguagesAppearancesAndSizes(t *testing.T) {
	now := time.Now()
	for _, size := range designEvidenceSizes {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/dark=%v", size[0], size[1], locale, dark), func(t *testing.T) {
					a := designEvidenceApp(t, now, "history")
					tt := designEvidenceTester(a, size[0], size[1], locale, dark)
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

func TestChangesAndReviewLayoutAcrossLanguagesAppearancesAndSizes(t *testing.T) {
	now := time.Now()
	for _, scene := range []string{"changes", "review", "clean"} {
		for _, size := range designEvidenceSizes {
			for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
				for _, dark := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%dx%d/%s/dark=%v", scene, size[0], size[1], locale, dark), func(t *testing.T) {
						a := designEvidenceApp(t, now, scene)
						tt := designEvidenceTester(a, size[0], size[1], locale, dark)
						tx := locales.In(locale)
						labels := []string{tx.SaveVersion(), tx.PullUpdates(), tx.PushChanges()}
						if scene == "changes" {
							labels = append(labels, tx.OpenFile("budget.csv"), tx.GenerateMessage())
						}
						if scene == "review" {
							labels = append(labels, tx.BackToOverview())
						}
						for _, label := range labels {
							visibleControl(t, tt, label, size[0], size[1])
						}
						saveDesignEvidence(t, tt, fmt.Sprintf("%s-%s-%d-dark-%v.png", scene, locale, size[0], dark))
					})
				}
			}
		}
	}
}

// Day headings must not be selectable or shift which commit an arrow reaches.
// The real pipe regression separately checks their exclusion from statistics.
func TestHistoryDayHeadingsKeepSelectionOnCommits(t *testing.T) {
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
			renderWorkspaceHistoryList(c, &a.nativeWorkspaceState, a.commitsList.list(), a.coreSnapshot.Workspace, a.coreSnapshot.Selected, a.commits, selected, true, func(action backend.KernelWorkspaceAction) {
				selected = action.SHAs
			}, nil)
		})
	}, 900, 620)
	tt.Advance(time.Second)
	if !tt.HasText("Today") {
		t.Fatalf("the newest day has no heading: %q", tt.Texts())
	}
	if err := tt.Click("Newest commit"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if len(selected) != 1 || selected[0] != "b" {
		t.Fatalf("Down crossed the day heading to the wrong record: %v", selected)
	}
}

// Each settings category has its icon, and the panel still fits every label.
func TestSettingsNavigationIconsFitEveryLanguage(t *testing.T) {
	pages := []string{"accounts", "integrations", "copilot", "git", "appearance", "notifications", "prompts", "advanced", "accessibility"}
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", locale, dark), func(t *testing.T) {
				a := newApp(nil, nil)
				defer a.close()
				popup := dialogFixture()
				popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
				var choices []backend.KernelDialogChoice
				for _, page := range pages {
					choices = append(choices, backend.KernelDialogChoice{ID: page, Label: "settings.option." + page})
				}
				popup.Fields = []backend.KernelDialogField{
					{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "appearance", Choices: choices},
					{ID: "theme", Kind: "select", LabelKey: "settings.theme", Value: "light", Choices: []backend.KernelDialogChoice{{ID: "light", Label: "settings.option.light"}}},
				}
				popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "theme"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
				a.coreSnapshot = &backend.KernelSnapshot{CurrentPopup: popup}
				a.setLocale = func(string) {}
				tt := ui.NewTester(func(c *ui.Context) {
					setWorkspaceTheme(c)
					a.coreAuthenticationView(c, locales.In(c.Locale()))
				}, 1180, 760)
				tt.SetLocale(locale)
				tt.SetDark(dark)
				tt.Advance(time.Second)
				for _, page := range pages {
					visibleControl(t, tt, nativeDialogChoiceLabel(locale, "settings.option."+page), 1180, 760)
				}
				saveDesignEvidence(t, tt, fmt.Sprintf("settings-%s-dark-%v.png", locale, dark))
			})
		}
	}
}
