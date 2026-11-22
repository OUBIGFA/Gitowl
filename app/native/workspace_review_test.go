package main

import (
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestNativeReviewExplainsSidesAndRetainsRawRangeTooltip(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans"} {
		t.Run(tag, func(t *testing.T) {
			h := newViewHarness(t)
			h.open(t, []backend.File{{Path: "notes.md", Status: "Modified"}})
			h.answer(t, "diff", patch("notes.md", "before", "after"), nil)
			h.tt.SetSize(1920, 1080)
			h.tt.SetLocale(tag)
			tx := locales.In(tag)
			h.click(t, tx.OpenFile("notes.md"))
			h.click(t, tx.ExpandDiff())
			h.answer(t, "diff", patch("notes.md", "before", "after"), nil)
			divider, ok := contentDivider(h.tt)
			if !ok {
				t.Fatal("file/diff divider missing")
			}
			x, y := divider.Bounds.X, divider.Bounds.Y+divider.Bounds.H/2
			h.tt.Press(x, y)
			h.tt.Move(x-220, y)
			h.tt.Release(x-220, y)
			before := visibleControl(t, h.tt, tx.ImageBefore(), 1920, 1080)
			after := visibleControl(t, h.tt, tx.ImageAfter(), 1920, 1080)
			if before.X >= after.X || before.Y != after.Y {
				t.Fatalf("diff sides lack aligned before/after context: %+v / %+v", before, after)
			}
			section := visibleControl(t, h.tt, tx.DiffSection(), 1920, 1080)
			h.tt.Move(section.X+section.W/2, section.Y+section.H/2)
			h.tt.Advance(time.Second)
			if !h.tt.HasText(tx.RawDiffRange("@@ -7,2 +7,2 @@")) {
				t.Fatalf("original range is not available in the native tooltip: %q", h.tt.Texts())
			}
			h.tt.SetSize(900, 620)
			// Red and green lines carry the unified layout; only split columns
			// need visible names. Its reading order stays in the description.
			if h.tt.HasText(tx.UnifiedDiffHint()) || h.tt.HasText(tx.ImageBefore()) || h.a.preview.patches[0].state.Layout != editor.DiffUnified {
				t.Fatal("narrow unified review kept split labels or a redundant caption")
			}
			for _, label := range []string{tx.SideBySide(), tx.Unified(), tx.WrapLines()} {
				visibleControl(t, h.tt, label, 900, 620)
			}
			if _, folded := h.tt.AccessByRole(ui.RoleButton, tx.ReviewOptions()); folded {
				t.Fatal("review options are still hidden behind a button")
			}
			split, found := h.tt.AccessByRole(ui.RoleRadio, tx.SideBySide())
			if !found || !split.Disabled {
				t.Fatal("narrow review must show the unavailable side-by-side option as disabled")
			}
			h.click(t, tx.WrapLines())
			if h.a.wrap {
				t.Fatal("native wrap control stopped working")
			}
			h.tt.SetSize(1920, 1080)
			h.click(t, tx.Unified())
			if h.tt.HasText(tx.ImageBefore()) || h.a.preview.patches[0].state.Layout != editor.DiffUnified {
				t.Fatal("the real unified option did not change the native view")
			}
			h.click(t, tx.SideBySide())
			if !h.tt.HasText(tx.ImageBefore()) || !h.tt.HasText(tx.ImageAfter()) || h.a.preview.patches[0].state.Layout != editor.DiffSplit {
				t.Fatal("the real split option lost its before/after labels")
			}
			hunk := h.a.preview.patches[0].file.Hunks[0]
			if hunk.OldStart != 7 || hunk.NewStart != 7 || h.a.preview.patches[0].state.New.String() != "context\nafter" {
				t.Fatal("presentation changes altered original ranges or patch contents")
			}
		})
	}
}
