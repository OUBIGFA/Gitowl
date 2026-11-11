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
			h.tt.SetLocale(tag)
			tx := locales.In(tag)
			h.click(t, tx.OpenFile("notes.md"))
			before := visibleControl(t, h.tt, tx.ImageBefore(), 1180, 760)
			after := visibleControl(t, h.tt, tx.ImageAfter(), 1180, 760)
			if before.X >= after.X || before.Y != after.Y {
				t.Fatalf("diff sides lack aligned before/after context: %+v / %+v", before, after)
			}
			section := visibleControl(t, h.tt, tx.DiffSection(), 1180, 760)
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
			h.click(t, tx.ReviewOptions())
			if !h.tt.HasText(tx.NarrowDiffLayout()) || h.tt.HasText(tx.SideBySide()) {
				t.Fatal("narrow review offered a side-by-side control that cannot take effect")
			}
			h.click(t, tx.WrapLines())
			if h.a.wrap {
				t.Fatal("native wrap control stopped working")
			}
			h.tt.Key(0, ui.KeyEscape)
			h.tt.SetSize(1180, 760)
			h.click(t, tx.ReviewOptions())
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
