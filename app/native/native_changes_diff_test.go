package main

import (
	"fmt"
	"io"
	"maps"
	"math"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestHistoryDiffUsesEditorPaneBesideTimeline(test *testing.T) {
	for _, size := range [][2]int{{900, 620}, {1180, 760}, {1920, 1080}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				test.Run(fmt.Sprintf("%dx%d/%s/dark=%t", size[0], size[1], locale, dark), func(test *testing.T) {
					application := designEvidenceApp(test, time.Now(), "review")
					stdout, output := io.Pipe()
					input, stdin := io.Pipe()
					application.core = backend.NewKernel(stdout, stdin, nil)
					updates := make(chan func(), 32)
					application.update = func(update func()) { updates <- update }
					go io.Copy(io.Discard, input)
					test.Cleanup(func() { application.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() })
					application.tab, application.reviewingFile, application.changeIndex, application.commitIndex, application.commitFileIndex = historyTab, false, 2, 0, 0
					application.changeset = &backend.Changeset{Files: coreFiles(application.coreSnapshot.Selected.HistoryFiles)}
					application.coreSnapshot.Selected.Section = "history"
					application.coreSnapshot.Selected.HistorySelection = []string{application.commits[0].SHA}
					application.coreSnapshot.Selected.HistoryFileID = ptr("a")
					application.workspaceHistoryFilesSHA = application.commits[0].SHA
					tester := designEvidenceTester(application, size[0], size[1], locale, dark)
					tx := locales.In(locale)
					historyLabel := workspaceLabel(locale, "workspace.history")
					collapsed := visibleControl(test, tester, historyLabel, size[0], size[1])
					assertNotebookDiffClosed(test, tester, application)
					if err := tester.Click(tx.OpenFile("docs/routes/lake-district.md")); err != nil {
						test.Fatal(err)
					}
					tester.Advance(time.Second)
					history := visibleControl(test, tester, historyLabel, size[0], size[1])
					diff := visibleControl(test, tester, tx.DiffTitle(), size[0], size[1])
					if !application.reviewingFile || diff.X+diff.W >= history.X || diff.X < 12 || math.Abs(float64(diff.Y+diff.H-float32(size[1]-16))) > 1 {
						test.Fatalf("history diff must stay left of the timeline with safe window margins: history=%+v diff=%+v", history, diff)
					}
					if tester.HasText(tx.SaveVersion()) || tester.HasText(tx.IncludeForSave()) || tester.HasText(tx.BackToVersion()) {
						test.Fatal("history review showed changes-only actions or fullscreen navigation")
					}
					tester.Move(4, 4)
					tester.Advance(time.Second)
					saveDesignEvidence(test, tester, fmt.Sprintf("history-diff-%s-%d-dark-%v.png", locale, size[0], dark))
					divider, found := tester.AccessByRole(ui.RoleSplitter, "")
					if !found {
						test.Fatal("history review lost the native draggable divider")
					}
					dividerX, dividerY := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+divider.Bounds.H/2
					tester.Press(dividerX, dividerY)
					tester.Move(dividerX+24, dividerY)
					tester.Release(dividerX+24, dividerY)
					tester.Frame()
					resized := visibleControl(test, tester, tx.DiffTitle(), size[0], size[1])
					if resized.W <= diff.W {
						test.Fatal("drag did not enlarge the history diff")
					}
					preview, selection := application.preview, application.workspaceHistoryFilesSHA
					if err := tester.Click(tx.CollapseDiff()); err != nil {
						test.Fatal(err)
					}
					if folded := visibleControl(test, tester, historyLabel, size[0], size[1]); folded.X <= resized.X+resized.W || application.reviewingFile {
						test.Fatalf("collapse did not restore the timeline: before=%+v after=%+v", collapsed, folded)
					}
					if err := tester.Click(tx.ExpandDiff()); err != nil {
						test.Fatal(err)
					}
					if reopened := visibleControl(test, tester, tx.DiffTitle(), size[0], size[1]); reopened.W != resized.W {
						test.Fatalf("reopening lost the chosen width: resized=%+v reopened=%+v", resized, reopened)
					}
					if application.preview != preview || application.commitFileIndex != 0 || application.workspaceHistoryFilesSHA != selection {
						test.Fatal("panel transitions changed the selected history file or its prepared diff")
					}
				})
			}
		}
	}
}

func TestChangesDiffPanelKeepsFilesDraftAndNativeEditingAcrossLayouts(t *testing.T) {
	for _, size := range [][2]int{{900, 620}, {1180, 760}, {1920, 1080}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/dark=%t", size[0], size[1], locale, dark), func(t *testing.T) {
					a := designEvidenceApp(t, time.Now(), "review")
					// Keep focus-report transport pending while exercising the real
					// native view. Business replies are covered by kernel integration.
					stdout, output := io.Pipe()
					input, stdin := io.Pipe()
					a.core = backend.NewKernel(stdout, stdin, nil)
					updates := make(chan func(), 32)
					a.update = func(fn func()) { updates <- fn }
					go io.Copy(io.Discard, input)
					t.Cleanup(func() { a.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() })
					a.reviewingFile = false
					tt := ui.NewTester(a.view, size[0], size[1])
					tt.SetLocale(locale)
					tt.SetDark(dark)
					tt.Advance(time.Second)
					tx := locales.In(locale)
					collapsedFiles := visibleControl(t, tt, tx.ChangedFiles(), size[0], size[1])
					_ = visibleControl(t, tt, tx.ExpandDiff(), size[0], size[1])
					assertNotebookDiffClosed(t, tt, a)
					if err := tt.Click("docs/routes/lake-district.md"); err != nil {
						t.Fatal(err)
					}
					if _, shown := tt.Find(tx.DiffTitle()); shown {
						t.Fatal("ordinary file click expanded the diff")
					}
					tt.Move(4, 4)
					tt.Advance(time.Second)
					saveDesignEvidence(t, tt, fmt.Sprintf("changes-collapsed-%s-%d-dark-%v.png", locale, size[0], dark))
					original, included := a.commitSummary, maps.Clone(a.included)
					if err := tt.Click(tx.CommitSummary()); err != nil {
						t.Fatal(err)
					}
					tt.Key(ui.Ctrl, ui.KeyA)
					tt.Type("Keep this draft 中文")
					if err := tt.Click(tx.OpenFile("docs/routes/lake-district.md")); err != nil {
						t.Fatal(err)
					}
					tt.Advance(time.Second)
					files := visibleControl(t, tt, tx.ChangedFiles(), size[0], size[1])
					diff := visibleControl(t, tt, tx.DiffTitle(), size[0], size[1])
					heading, found := tt.AccessByRole(ui.RoleHeading, "docs/routes/lake-district.md")
					inclusion := visibleControl(t, tt, tx.IncludeForSave(), size[0], size[1])
					collapse := visibleControl(t, tt, tx.CollapseDiff(), size[0], size[1])
					for _, label := range []string{tx.SideBySide(), tx.Unified(), tx.WrapLines()} {
						visibleControl(t, tt, label, size[0], size[1])
					}
					if !found || heading.Bounds.X < diff.X || inclusion.X < diff.X || collapse.X+collapse.W > files.X {
						t.Fatalf("diff controls lost their safety padding: diff=%+v heading=%+v inclusion=%+v collapse=%+v", diff, heading.Bounds, inclusion, collapse)
					}
					tt.Move(4, 4)
					tt.Advance(time.Second)
					saveDesignEvidence(t, tt, fmt.Sprintf("changes-expanded-%s-%d-dark-%v.png", locale, size[0], dark))
					if diff.X+diff.W >= files.X || math.Abs(float64(diff.Y+diff.H-float32(size[1]-16))) > 1 {
						t.Fatalf("diff content lost its safe window margins: %+v in %v", diff, size)
					}
					if !a.reviewingFile || diff.X+diff.W >= files.X || !tt.HasText(tx.IncludeAll()) {
						t.Fatalf("file review replaced the home page: files=%+v diff=%+v", files, diff)
					}
					visibleControl(t, tt, tx.OpenFile("budget.csv"), size[0], size[1])
					save := visibleControl(t, tt, tx.SaveVersion(), size[0], size[1])
					if save.X < files.X-1 {
						t.Fatal("save controls escaped the file changes pane")
					}
					divider, ok := tt.AccessByRole(ui.RoleSplitter, "")
					if !ok {
						t.Fatal("native draggable divider missing")
					}
					x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+divider.Bounds.H/2
					tt.Press(x, y)
					tt.Move(x+40, y)
					tt.Release(x+40, y)
					tt.Frame()
					resized := visibleControl(t, tt, tx.DiffTitle(), size[0], size[1])
					if resized.W <= diff.W || resized.X != diff.X {
						t.Fatalf("drag did not resize the right pane: before=%+v after=%+v", diff, resized)
					}
					tt.Key(0, ui.KeyLeft)
					keyboard := visibleControl(t, tt, tx.DiffTitle(), size[0], size[1])
					if keyboard.W >= resized.W {
						t.Fatal("focused native divider did not resize with the keyboard")
					}
					resized = keyboard
					if err := tt.Click(tx.CollapseDiff()); err != nil {
						t.Fatal(err)
					}
					if _, shown := tt.Find(tx.DiffTitle()); shown || a.reviewingFile {
						t.Fatal("collapse left the diff open")
					}
					if folded := visibleControl(t, tt, tx.ChangedFiles(), size[0], size[1]); folded.X <= keyboard.X+keyboard.W || folded.W <= 0 {
						t.Fatalf("collapse did not restore the centered width: before=%+v after=%+v", collapsedFiles, folded)
					}
					assertNotebookDiffClosed(t, tt, a)
					if err := tt.Click(tx.ExpandDiff()); err != nil {
						t.Fatal(err)
					}
					if reopened := visibleControl(t, tt, tx.DiffTitle(), size[0], size[1]); reopened.W != resized.W {
						t.Fatalf("collapse lost the chosen pane width: resized=%+v reopened=%+v", resized, reopened)
					}
					if a.commitSummary != "Keep this draft 中文" || !maps.Equal(included, a.included) {
						t.Fatal("opening or resizing changed the draft or included files")
					}
					// Keeping the text value alone is insufficient. Native undo must
					// survive the panel opening, resizing, closing and reopening.
					if err := tt.Click(tx.CommitSummary()); err != nil {
						t.Fatal(err)
					}
					tt.Key(ui.Ctrl, ui.KeyZ)
					if a.commitSummary != original {
						t.Fatalf("panel transitions discarded native undo: %q", a.commitSummary)
					}
					// Exercise the image in the production split, rather than a
					// standalone harness that supplies its own content padding.
					prepared, err := prepareNativeImageDiff(a.ctx, nativeImageTestDiff(t, 600, 400))
					if err != nil {
						t.Fatal(err)
					}
					a.preview = &preview{kind: "image"}
					a.imagePrepared = prepared
					tt.Advance(time.Second)
					image := visibleControl(t, tt, nativeImageLabel(locale, "preview"), size[0], size[1])
					divider, ok = tt.AccessByRole(ui.RoleSplitter, "")
					if !ok || image.X < 12 || image.X+image.W > divider.Bounds.X || math.Abs(float64(image.Y+image.H-float32(size[1]-16))) > 1 {
						t.Fatalf("image preview lost the diff panel's safe margins: image=%+v divider=%+v", image, divider.Bounds)
					}
					saveDesignEvidence(t, tt, fmt.Sprintf("changes-image-%s-%d-dark-%v.png", locale, size[0], dark))
				})
			}
		}
	}
}

// Closing a diff keeps the editor and the review column available.
func assertNotebookDiffClosed(t *testing.T, tester *ui.Tester, a *app) {
	t.Helper()
	if a.reviewingFile {
		t.Fatal("diff is open before selection")
	}
	if _, found := tester.AccessByRole(ui.RoleSplitter, ""); !found {
		t.Fatal("editor and review splitter is missing")
	}
}
