package editor

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func TestIncrementalLayoutMatchesCompleteLayout(t *testing.T) {
	for _, kind := range []string{"paragraph", "bullet", "numbered", "quote", "callout", "code", "heading", "section", "cell", "last-cell"} {
		t.Run(kind, func(t *testing.T) {
			e, tt := newTester("# Title\n\nfirst paragraph\n\n- parent\n  - child\n\n1. first\n2. second\n\n> quote\n\n> [!TIP]\n> callout\n\n```go\ncode\n```\n\n## Heading\n\n| A | B |\n|---|---|\n| a | b |\n\nafter")
			e.Engine.Insert(document.Columns())
			for _, block := range e.Doc.Blocks {
				if block.Kind == "table" {
					e.Engine.MergeCells(block.ID, document.CellRange{Row0: 0, Col0: 0, Row1: 0, Col1: 1})
				}
			}
			tt.Frame()
			focus(e, tt)
			var target document.Leaf
			for _, leaf := range e.Doc.Leaves() {
				if leaf.Block.Kind == kind || kind == "section" && leaf.Block.Kind == "heading" && leaf.Block.Level == 2 || kind == "last-cell" && leaf.Block.Kind == "cell" {
					target = leaf
					if kind != "last-cell" {
						break
					}
				}
			}
			e.SetSelection(target.End, target.End)
			tt.Frame()
			check := func() {
				t.Helper()
				pixels := slices.Clone(tt.Image().Pix)
				for i := range e.rows {
					e.indexedRow(i)
				}
				rows := slices.Clone(e.rows)
				height := e.height
				decorations := slices.Clone(e.decorations)
				handles := slices.Clone(e.handles)
				folds := slices.Clone(e.foldButtons)
				outline := slices.Clone(e.outline)
				resizers := slices.Clone(e.resizers)
				e.ready = false
				tt.Frame()
				if len(rows) != len(e.rows) || math.Abs(float64(height-e.height)) > .001 {
					t.Fatal("incremental layout changed the document extent")
				}
				for i, row := range rows {
					full := e.rows[i]
					if row.leaf.Start != full.leaf.Start || row.leaf.End != full.leaf.End || row.leaf.ByteStart != full.leaf.ByteStart || row.leaf.ByteEnd != full.leaf.ByteEnd || math.Abs(float64(row.y-full.y)) > .001 || math.Abs(float64(row.rect.H-full.rect.H)) > .001 {
						t.Fatalf("incremental row %d differs from complete layout", i)
					}
				}
				if !equalLayout(decorations, e.decorations) || !equalLayout(handles, e.handles) || !equalLayout(folds, e.foldButtons) || !equalLayout(outline, e.outline) || !equalLayout(resizers, e.resizers) {
					t.Fatalf("incremental controls differ: decorations=%v handles=%v folds=%v outline=%v resizers=%v", equalLayout(decorations, e.decorations), equalLayout(handles, e.handles), equalLayout(folds, e.foldButtons), equalLayout(outline, e.outline), equalLayout(resizers, e.resizers))
				}
				if !bytes.Equal(pixels, tt.Image().Pix) {
					t.Fatal("incremental layout changed rendered pixels")
				}
				if e.wordCount() != countWords(e.Doc.PlainText()) {
					t.Fatalf("incremental word count differs: got %d, want %d", e.wordCount(), countWords(e.Doc.PlainText()))
				}
			}
			for _, text := range []string{"😀", strings.Repeat("中文 wrap ", 35), "\u2028next"} {
				rows := &e.rows[0]
				tt.Type(text)
				if text == "😀" && (kind == "heading" || kind == "section" || kind == "cell" || kind == "last-cell") && rows != &e.rows[0] {
					t.Fatal("text edit unexpectedly rebuilt unrelated rows")
				}
				// Bring the following merged table into view before comparing
				// against a full layout, including its bars and cell rules.
				for _, d := range e.decorations {
					if d.kind == "table" {
						e.Scroll.Y = d.rect.Y - 50
						break
					}
				}
				tt.Frame()
				check()
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			check()
			tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
			check()
			tt.Compose("候选", 2)
			check()
			tt.Type("中文")
			check()
			// Multiple edits without a frame must take the complete-layout path.
			e.ReplaceText(nil, "one")
			e.ReplaceText(nil, "two")
			tt.Frame()
			check()
		})
	}
}

// Translating float32 coordinates can round differently from rebuilding them
// in document order. Subpixel tolerance still catches stale or missing geometry.
func equalLayout(a, b any) bool {
	var equal func(reflect.Value, reflect.Value) bool
	equal = func(a, b reflect.Value) bool {
		if a.Type() != b.Type() {
			return false
		}
		switch a.Kind() {
		case reflect.Float32, reflect.Float64:
			return math.Abs(a.Float()-b.Float()) < .001
		case reflect.Struct:
			for i := 0; i < a.NumField(); i++ {
				if !equal(a.Field(i), b.Field(i)) {
					return false
				}
			}
			return true
		case reflect.Slice:
			if a.Len() != b.Len() || a.IsNil() != b.IsNil() {
				return false
			}
			for i := 0; i < a.Len(); i++ {
				if !equal(a.Index(i), b.Index(i)) {
					return false
				}
			}
			return true
		default:
			return a.Comparable() && a.Equal(b)
		}
	}
	return equal(reflect.ValueOf(a), reflect.ValueOf(b))
}

func TestIncrementalHeadingAndMergedTableExtents(t *testing.T) {
	e, tt := newTester("# Title\n\n## Section\n\nbody\n\n| A | B | C |\n|---|---|---|\n| a | b | c |\n| d | e | f |\n\n## After\n\nend")
	for _, b := range e.Doc.Blocks {
		if b.Kind == "table" {
			e.Engine.MergeCells(b.ID, document.CellRange{Row0: 0, Col0: 0, Row1: 1, Col1: 1})
			e.Engine.SetWidths(b.ID, []float32{1, 2, 3})
		}
	}
	tt.Frame()
	options := Options{Width: e.width, Theme: &e.cacheTheme}
	type geometryRow struct {
		id                             string
		start, end, byteStart, byteEnd int
		rect                           ui.Rect
		x, y, width, height            float32
	}
	type geometry struct {
		rows        []geometryRow
		decorations []decoration
		handles     []blockHandle
		folds       []foldButton
		outline     []outlineItem
		resizers    []columnHandle
		height, end float32
	}
	capture := func() geometry {
		g := geometry{decorations: slices.Clone(e.decorations), handles: slices.Clone(e.handles), folds: slices.Clone(e.foldButtons), outline: slices.Clone(e.outline), resizers: slices.Clone(e.resizers), height: e.height, end: e.contentEnd}
		for i := range e.rows {
			r := e.indexedRow(i)
			w, h := r.layout.Size()
			g.rows = append(g.rows, geometryRow{r.leaf.Block.ID, r.leaf.Start, r.leaf.End, r.leaf.ByteStart, r.leaf.ByteEnd, r.rect, r.x, r.y, w, h})
		}
		return g
	}
	for _, target := range e.Doc.Leaves() {
		if target.Block.Kind != "heading" && target.Block.Kind != "cell" {
			continue
		}
		for _, text := range []string{"", strings.Repeat("中文😀 wrapping ", 25), "short"} {
			e.Engine.FocusBlock(target.Block.ID)
			leaf := e.Engine.LeafAt(e.Caret)
			e.Engine.Replace(leaf.Start, leaf.End, text)
			if !e.relayoutText(options) {
				t.Fatalf("%s did not use local layout", target.Block.Kind)
			}
			got := capture()
			e.ready = false
			e.layout(options)
			if !equalLayout(got, capture()) {
				t.Fatalf("%s text %q: local layout differs", target.Block.Kind, text)
			}
		}
	}
}
