package editor

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func TestNativeWindowResizeDefersAndSettlesWithoutInput(t *testing.T) {
	var source strings.Builder
	for i := range 160 {
		fmt.Fprintf(&source, "Paragraph %d：拖动窗口边缘时仍然需要保持流畅，可见段落立即换行，屏幕外内容在拖动结束后排版。\n\n", i)
	}
	e, tt := newTester(source.String())
	original := e.Text()
	e.Scroll.Y = e.rows[80].rect.Y
	tt.Frame()
	last := e.rows[len(e.rows)-1].leaf.Block.ID
	oldLast := e.cache[last].layout
	for _, width := range []int{840, 800, 760} {
		tt.SetSize(width, 650)
		if !e.resizePending || e.cache[last].layout != oldLast {
			t.Fatal("native resize reshaped offscreen paragraphs")
		}
		tt.Advance(windowResizeQuiet / 2)
		if !e.resizePending || e.cache[last].layout != oldLast {
			t.Fatal("a frame between resize events prematurely reshaped the document")
		}
	}
	// Scroll during resize: newly visible rows must use the current width.
	e.Scroll.Y = e.rows[120].rect.Y
	tt.Frame()
	if got := e.cache[e.rows[120].leaf.Block.ID].width; got != e.width-2*e.padding {
		t.Fatalf("newly visible text retained stale width %v", got)
	}
	anchor := e.resizeAnchor()
	// No explicit Frame or input: the scheduled timer must finish the work.
	tt.Advance(windowResizeQuiet + time.Millisecond)
	if e.resizePending || e.cache[last].layout == oldLast {
		t.Fatal("resize remained approximate after the quiet period")
	}
	if got := e.resizeAnchor(); got.id != anchor.id || math.Abs(float64(got.offset-anchor.offset)) > 1 {
		t.Fatalf("final reflow moved the reading anchor: %+v -> %+v", anchor, got)
	}
	height, pixels := e.height, bytes.Clone(tt.Image().Pix)
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if math.Abs(float64(height-e.height)) > .01 || !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("native resize settled differently from a complete layout")
	}
	if e.Text() != original || e.Version() != 0 {
		t.Fatal("resizing changed the document or undo history")
	}
	// A height-only resize changes the viewport, not text wrapping.
	before := e.rows[120].layout
	tt.SetSize(760, 540)
	if e.resizePending || e.rows[120].layout != before {
		t.Fatal("height-only resize invalidated text shaping")
	}
	// Editing and undo after resize use the same exact text geometry.
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Type("新增😀")
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Text() != original {
		t.Fatal("editing after resize broke undo")
	}
}

func TestRapidWindowResizeBoundsWorkAndUsesLatestWidth(t *testing.T) {
	var source strings.Builder
	for i := range 600 {
		fmt.Fprintf(&source, "Paragraph %d：快速拖动时必须立即采用最新宽度。即使上一次窗口调整后的排版还没有完成，当前可见文字也不能等整篇文章排完再换行。\n\n", i)
	}
	e, tt := newTester(source.String())
	e.Scroll.Y = e.rows[300].rect.Y + 7
	tt.Frame()
	anchor := e.resizeAnchor()
	rows := &e.rows[0]
	checkVisible := func() {
		t.Helper()
		for i := range e.visibleRows(0) {
			r := e.rows[i]
			if got := e.cache[r.leaf.Block.ID].width; got != e.width-2*e.padding {
				t.Fatalf("visible row %d is at width %v, want %v", i, got, e.width-2*e.padding)
			}
		}
		if got := e.resizeAnchor(); got.id != anchor.id || math.Abs(float64(got.offset-anchor.offset)) > 1 {
			t.Fatalf("rapid resize moved the reading anchor: %+v -> %+v", anchor, got)
		}
	}
	for _, width := range []int{560, 820, 510, 760, 620} {
		tt.SetSize(width, 650)
		if e.width != float32(width) || &e.rows[0] != rows {
			t.Fatal("resize lagged behind the newest size or reallocated the row list")
		}
		checkVisible()
		before := map[string]*textLayout{}
		for id, cached := range e.cache {
			before[id] = cached.layout
		}
		// Let the quiet timer fire once, then interrupt its remaining work
		// with another large resize. A pause must never reflow the whole book.
		tt.Advance(windowResizeQuiet)
		changed := 0
		for id, cached := range e.cache {
			if cached.layout != before[id] {
				changed++
			}
		}
		if changed == 0 || changed > resizeBatchShapes+24 || !e.resizePending {
			t.Fatalf("unbounded or missing completion work: %d shapes, pending=%v", changed, e.resizePending)
		}
		checkVisible()
	}
	// All remaining work finishes from frame timers, without more user input.
	for frame := 0; e.resizePending && frame < 100; frame++ {
		tt.Advance(8 * time.Millisecond)
		checkVisible()
	}
	if e.resizePending {
		t.Fatal("batched layout never completed")
	}
	height, pixels := e.height, bytes.Clone(tt.Image().Pix)
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if height != e.height || !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("batched completion differs from a fresh exact layout")
	}
}

func TestWindowResizeBatchesPreserveTableAnchor(t *testing.T) {
	var source strings.Builder
	for i := range 100 {
		fmt.Fprintf(&source, "Before %d: paragraphs above the table change height when the window narrows.\n\n", i)
	}
	source.WriteString("| A | B | C |\n|---|---|---|\n")
	for i := range 40 {
		fmt.Fprintf(&source, "| Row %d | 表格内较长的内容需要随着窗口宽度重新换行 | Another cell |\n", i)
	}
	for i := range 200 {
		fmt.Fprintf(&source, "After %d: more text below the table to finish in separate batches.\n\n", i)
	}
	e, tt := newTester(source.String())
	e.Scroll.Y = e.rows[160].rect.Y + 4
	tt.Frame()
	anchor := e.resizeAnchor()
	tt.SetSize(560, 650)
	tt.Advance(windowResizeQuiet)
	for frame := 0; e.resizePending && frame < 100; frame++ {
		if got := e.resizeAnchor(); got.id != anchor.id || math.Abs(float64(got.offset-anchor.offset)) > 1 {
			t.Fatalf("table cell moved during a completion batch: %+v -> %+v", anchor, got)
		}
		tt.Advance(8 * time.Millisecond)
	}
	if e.resizePending {
		t.Fatal("table layout never completed")
	}
	height, pixels := e.height, bytes.Clone(tt.Image().Pix)
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if height != e.height || !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("batched table geometry differs from complete layout")
	}
}

func TestShapeCacheRetainsExactImmutableInputs(t *testing.T) {
	e, tt := newTester("Text to wrap and style.\n\n- [ ] Task")
	b := e.Doc.Blocks[0]
	for _, change := range []func(){
		func() { b.Runs[0].Text = "Same length new words." },
		func() { b.Runs[0].Style.Bold = true },
		func() { b.Runs[0].Style.Highlight = "yellow" },
		func() { b.Runs[0].Style.Link = "https://example.com" },
		func() { b.Align = "center" },
		func() { b = e.Doc.Blocks[1]; b.Checked = true },
	} {
		old := map[string]*textLayout{}
		for id, cached := range e.cache {
			old[id] = cached.layout
		}
		change()
		// Direct model changes must also invalidate the cache when a full
		// layout is requested; no revision-only shortcut can hide them.
		e.ready = false
		tt.Frame()
		cached := e.cache[b.ID]
		if cached.layout == old[b.ID] {
			t.Fatal("changed text/style reused stale shaping")
		}
		if len(cached.runs) > 0 && &cached.runs[0] == &b.Runs[0] {
			t.Fatal("cached runs alias mutable document runs")
		}
		pixels := bytes.Clone(tt.Image().Pix)
		e.ready = false
		clear(e.cache)
		tt.Frame()
		if !bytes.Equal(pixels, tt.Image().Pix) {
			t.Fatal("cached inputs disagree with fresh shaping")
		}
	}
	// The snapshot must include all style fields, including annotations.
	before := e.cache[b.ID].runs[0]
	b.Runs[0].Style = document.Style{Annotation: "wavy", AnnotationColor: "red"}
	if e.cache[b.ID].runs[0] != before {
		t.Fatal("model mutation changed the cached style snapshot")
	}
}

func TestResizeReflowsVisibleTextAndFinishesExactly(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Resize\n\n")
	for i := range 160 {
		fmt.Fprintf(&source, "Paragraph %d：调整分屏时应实时换行，屏幕外的段落不应反复重排。留下足够的文字，验证换行和滚动位置。\n\n", i)
	}
	e := New(source.String())
	width := float32(620)
	resizing := false
	tt := ui.NewTester(func(c *ui.Context) { View(c, e, Options{Width: width, Theme: c.Theme(), Resizing: resizing}) }, 640, 500)
	tt.Frame()
	last := e.rows[len(e.rows)-1].leaf.Block.ID
	oldLast := e.cache[last].layout
	oldFirst := e.rows[1].layout
	width, resizing = 420, true
	tt.Frame()
	if e.rows[1].layout == oldFirst {
		t.Fatal("visible text did not reflow")
	}
	if e.cache[last].layout != oldLast || !e.resizePending {
		t.Fatal("offscreen text was reshaped during drag")
	}
	// Scrolling while holding the divider must also refresh newly visible rows.
	e.Scroll.Y = e.rows[80].rect.Y
	tt.Frame()
	if cached := e.cache[e.rows[80].leaf.Block.ID]; cached.width != width-2*e.padding {
		t.Fatal("newly visible text kept stale wrapping")
	}
	anchor := e.resizeAnchor()
	resizing = false
	tt.Frame()
	if e.resizePending || e.cache[last].layout == oldLast {
		t.Fatal("release left approximate geometry")
	}
	if got := e.resizeAnchor(); got.id != anchor.id || math.Abs(float64(got.offset-anchor.offset)) > 1 {
		t.Fatalf("release changed reading anchor: %+v -> %+v", anchor, got)
	}
	pixels := bytes.Clone(tt.Image().Pix)
	height := e.height
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if math.Abs(float64(height-e.height)) > .01 || !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("released resize differs from complete layout")
	}
}

// Table cells are laid out at y 0 and moved into place afterwards; resize
// deferral must still judge them by their real position, or a long table
// below the fold is reshaped on every frame of a divider drag.
func TestResizeDefersOffscreenTableCells(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Resize\n\n")
	for i := range 40 {
		fmt.Fprintf(&source, "Paragraph %d：表格在屏幕外时，拖动分屏不应重排它的单元格。\n\n", i)
	}
	source.WriteString("| A | B | C |\n|---|---|---|\n")
	for i := range 30 {
		fmt.Fprintf(&source, "| 单元格 %d | 把结构化数据渲染成卡片、表格或审批界面 | 普通内容 |\n", i)
	}
	e := New(source.String())
	width := float32(620)
	resizing := false
	tt := ui.NewTester(func(c *ui.Context) { View(c, e, Options{Width: width, Theme: c.Theme(), Resizing: resizing}) }, 640, 500)
	tt.Frame()
	var cells []string
	for _, r := range e.rows {
		if r.leaf.Block.Kind == "cell" {
			cells = append(cells, r.leaf.Block.ID)
		}
	}
	if len(cells) == 0 || e.rows[len(e.rows)-1].rect.Y < 2000 {
		t.Fatal("fixture table is not below the fold")
	}
	before := map[string]float32{}
	for _, id := range cells {
		before[id] = e.cache[id].width
	}
	width, resizing = 420, true
	tt.Frame()
	for _, id := range cells {
		if e.cache[id].width != before[id] {
			t.Fatal("offscreen table cell was reshaped during drag")
		}
	}
	// Scrolled onto the table, its visible cells wrap at the new width.
	e.Scroll.Y = e.rows[len(e.rows)-1].rect.Y - 200
	tt.Frame()
	if e.cache[cells[len(cells)-1]].width == before[cells[len(cells)-1]] {
		t.Fatal("visible table cell kept stale wrapping")
	}
	resizing = false
	tt.Frame()
	for _, id := range cells {
		if e.cache[id].width == before[id] {
			t.Fatal("release left a table cell at the old width")
		}
	}
}
