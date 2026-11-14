package main

import (
	"fmt"
	"io"
	"maps"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

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
					visibleControl(t, tt, tx.ExpandDiff(), size[0], size[1])
					if _, shown := tt.Find(tx.DiffTitle()); shown {
						t.Fatal("diff is expanded before the user opens it")
					}
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
					if !a.reviewingFile || diff.X <= files.X+files.W || !tt.HasText(tx.IncludeAll()) {
						t.Fatalf("file review replaced the home page: files=%+v diff=%+v", files, diff)
					}
					visibleControl(t, tt, tx.OpenFile("budget.csv"), size[0], size[1])
					save := visibleControl(t, tt, tx.SaveVersion(), size[0], size[1])
					if save.X+save.W >= diff.X {
						t.Fatal("save controls escaped the file changes pane")
					}
					divider, ok := tt.AccessByRole(ui.RoleSplitter, "")
					if !ok {
						t.Fatal("native draggable divider missing")
					}
					x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+divider.Bounds.H/2
					tt.Press(x, y)
					tt.Move(x-40, y)
					tt.Release(x-40, y)
					tt.Frame()
					resized := visibleControl(t, tt, tx.DiffTitle(), size[0], size[1])
					if resized.W <= diff.W || resized.X >= diff.X {
						t.Fatalf("drag did not resize the right pane: before=%+v after=%+v", diff, resized)
					}
					tt.Key(0, ui.KeyRight)
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
				})
			}
		}
	}
}
