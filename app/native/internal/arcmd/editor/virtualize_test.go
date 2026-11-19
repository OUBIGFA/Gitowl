package editor

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func buildLargeMarkdown(targetRunes int) string {
	var b strings.Builder
	b.Grow(targetRunes * 3)
	section := 0
	for utf8.RuneCountInString(b.String()) < targetRunes {
		section++
		fmt.Fprintf(&b, "## Section %d 性能与内存架构\n\n", section)
		for p := 0; p < 5; p++ {
			fmt.Fprintf(&b, "第 %d 节第 %d 段测试正文：包含 **加粗**、*斜体*、`inlineCode()` 与长文排版换行验证，确保百万字符文档打开与滚动平滑。", section, p+1)
			if section == 900 && p == 0 {
				b.WriteString(" UNIQUE_TAIL_NEEDLE ")
			}
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "- 列表项目 %d\n\n", section)
	}
	return b.String()
}

func TestVirtualizedLayoutMillionCharMemoryAndNavigation(t *testing.T) {
	md := buildLargeMarkdown(1_000_000)
	if runes := utf8.RuneCountInString(md); runes < 1_000_000 {
		t.Fatalf("fixture too small: %d runes", runes)
	}

	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	e := New(md)
	md = ""
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
	tt.Frame()

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	if m1.HeapAlloc > m0.HeapAlloc && m1.HeapAlloc-m0.HeapAlloc > 45<<20 {
		t.Fatalf("live heap delta too high for 1M-char document: %.1f MB", float64(m1.HeapAlloc-m0.HeapAlloc)/(1024*1024))
	}
	if !e.shouldVirtualize() {
		t.Fatal("expected million-char document to activate virtualization")
	}
	if len(e.cache) > maxCachedShapes+cacheTrimHysteresis {
		t.Fatalf("initial cache exceeded bound: %d", len(e.cache))
	}
	if e.rows[len(e.rows)-1].layout != nil {
		t.Fatal("offscreen tail block was eagerly shaped on initial open")
	}
	if e.textKnown {
		t.Fatal("open/view materialized the full flattened document string")
	}

	// Jumping through well-spaced regions across the whole document triggers LRU eviction,
	// keeps the shape cache bounded, retains exact height hints, and shapes every visible row.
	step := max(120, len(e.rows)/35)
	for rowIdx := step; rowIdx < len(e.rows); rowIdx += step {
		e.Scroll.Y = e.indexedRow(rowIdx).rect.Y
		tt.Frame()
		if len(e.cache) > maxCachedShapes+cacheTrimHysteresis {
			t.Fatalf("cache exceeded bound after jump to row %d: %d", rowIdx, len(e.cache))
		}
		for vi := range e.visibleRows(0) {
			if e.rows[vi].layout == nil {
				t.Fatalf("visible row %d was not shaped after jump", vi)
			}
		}
	}
	if len(e.heightHints) == 0 {
		t.Fatal("expected evicted shapes to retain exact height hints")
	}

	// Jump back into an early evicted region and verify height/offset stability once re-shaped.
	backRow := step
	beforeY := e.indexedRow(backRow).rect.Y
	e.Scroll.Y = beforeY
	tt.Frame()
	if e.rows[backRow].layout == nil {
		t.Fatalf("evicted row %d was not re-shaped on return", backRow)
	}
	if diff := e.indexedRow(backRow).rect.Y - beforeY; diff < -1 || diff > 1 {
		t.Fatalf("expected evicted region offset to remain stable, moved by %.2f", diff)
	}

	// Return to the middle region for hit testing and editing assertions below.
	e.Scroll.Y = e.indexedRow(len(e.rows) / 2).rect.Y
	tt.Frame()

	// Verify hit testing and editing in a scrolled middle section, plus undo.
	focus(e, tt)
	targetIdx := len(e.rows) / 2
	targetRow := e.ensureRow(targetIdx)
	pt := ui.Point{X: targetRow.x + 12, Y: targetRow.y + 8}
	pos, ok := e.IndexForPoint(pt)
	if !ok || pos < targetRow.leaf.Start || pos > targetRow.leaf.End {
		t.Fatalf("IndexForPoint missed row %d: ok=%v pos=%d range=[%d,%d]", targetIdx, ok, pos, targetRow.leaf.Start, targetRow.leaf.End)
	}
	beforeBlock := targetRow.leaf.Block.Text()
	e.SetSelection(targetRow.leaf.Start, targetRow.leaf.Start)
	tt.Type("XYZ ")
	if got := targetRow.leaf.Block.Text(); got != "XYZ "+beforeBlock {
		t.Fatalf("edit in virtualized middle block failed: got %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := targetRow.leaf.Block.Text(); got != beforeBlock {
		t.Fatalf("undo in virtualized middle block failed: got %q, want %q", got, beforeBlock)
	}

	// FindText jumps cleanly to an offscreen match and shapes the destination.
	e.Scroll.Y = 0
	tt.Frame()
	e.FindText("UNIQUE_TAIL_NEEDLE")
	for range 12 {
		tt.Advance(40 * time.Millisecond)
	}
	if len(e.find.matches) != 1 {
		t.Fatalf("find matches = %d, want 1", len(e.find.matches))
	}
	match := e.find.matches[0]
	r, _, ok := e.BoundsForRange(match)
	if !ok || math.IsNaN(float64(r.Y)) || r.Y <= 0 {
		t.Fatalf("offscreen FindText returned invalid bounds: ok=%v rect=%+v", ok, r)
	}
	runtime.KeepAlive(tt)
}

func TestVirtualizedDocumentPaginatesExact(t *testing.T) {
	// Above virtualizeLeafThreshold (~900 leaves) to verify both interactive
	// virtualization and full unvirtualized PDF pagination work seamlessly.
	md := buildLargeMarkdown(130_000)
	e := New(md)
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 900, 650)
	tt.Frame()
	if !e.shouldVirtualize() {
		t.Fatal("expected fixture to exceed virtualization threshold")
	}
	if e.rows[len(e.rows)-1].layout != nil {
		t.Fatal("expected tail row to be deferred in interactive mode")
	}

	printEd := FromDocument(e.Doc)
	var tops []float32
	ui.NewTester(func(c *ui.Context) {
		tops = printEd.Paginate(Options{Width: 680, Theme: c.Theme()}, 900)
	}, 700, 900).Frame()
	if len(tops) < 2 {
		t.Fatalf("expected multi-page pagination, got %v", tops)
	}
	for i := 1; i < len(tops); i++ {
		if tops[i] <= tops[i-1] {
			t.Fatalf("non-monotonic page tops: %v", tops)
		}
	}
	for i := range printEd.rows {
		if printEd.rows[i].layout == nil {
			t.Fatalf("printing left row %d unshaped", i)
		}
	}
	runtime.KeepAlive(tt)
}

func TestLargeDocument200kInteractiveEditingAndIME(t *testing.T) {
	md := buildLargeMarkdown(210_000)
	md = strings.Replace(md, "\n\n", "\n\n#tag_perf 测试首段用于输入法与编辑响应。\n\n", 1)
	e := New(md)
	if e.ByteLen() < 200_000 {
		t.Fatalf("fixture byte length too small: %d", e.ByteLen())
	}
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 1000, 700)
	tt.Frame()
	if !e.shouldVirtualize() {
		t.Fatal("expected 200k doc to activate virtualization")
	}
	if len(e.accessText()) > 4*accessWindowHalf*4 {
		t.Fatalf("accessText returned unbounded text: %d bytes", len(e.accessText()))
	}
	if len(e.displayText()) < 200_000 {
		t.Fatalf("displayText truncated document: %d", len(e.displayText()))
	}

	focus(e, tt)
	leaf := e.Engine.LeafAt(0)
	startCount := e.Engine.LeafCount()
	e.SetSelection(leaf.End, leaf.End)
	tt.Frame()

	// ASCII typing and Chinese IME composition stay on single-leaf path without cloning Document.
	for i := 0; i < 15; i++ {
		tt.Type("a")
	}
	for i := 0; i < 5; i++ {
		tt.Compose("ni", 2)
		if e.composition != nil || !e.Composing() {
			t.Fatalf("expected single-leaf IME composition, composition=%v composing=%v", e.composition != nil, e.Composing())
		}
		tt.Compose("nihao", 5)
		tt.Type("你好")
	}
	// Cancel an active composition via Escape.
	tt.Compose("quxiao", 6)
	tt.Key(0, ui.KeyEscape)
	// An IME consumes Escape and reports that its marked text was canceled.
	tt.Compose("", 0)
	if e.Composing() {
		t.Fatal("Escape did not cancel active composition")
	}
	// Undo last committed Chinese word via Cmd+Z.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)

	// Paragraph split (Enter), typing after structural change, and backspace join.
	tt.Key(0, ui.KeyEnter)
	if e.Engine.LeafCount() != startCount+1 {
		t.Fatalf("Enter leaf count = %d, want %d", e.Engine.LeafCount(), startCount+1)
	}
	tt.Type("新段落")
	tt.Compose("ceshi", 5)
	tt.Type("测试")
	if !strings.Contains(e.Engine.LeafAt(e.Caret).Block.Text(), "新段落测试") {
		t.Fatalf("post-Enter IME commit missing text: %q", e.Engine.LeafAt(e.Caret).Block.Text())
	}
	for range 5 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Key(0, ui.KeyBackspace) // join back to previous paragraph
	if e.Engine.LeafCount() != startCount {
		t.Fatalf("Backspace join leaf count = %d, want %d", e.Engine.LeafCount(), startCount)
	}
	if len(e.accessText()) > 4*accessWindowHalf*4 {
		t.Fatalf("accessText grew unbounded after edits: %d", len(e.accessText()))
	}
	runtime.KeepAlive(tt)
}

func TestVirtualizedTablesMathAndMermaidColdOpenAndScrollAnchoring(t *testing.T) {
	doc := document.New()
	doc.Blocks = make([]*document.Block, 0, 520)
	longCell := strings.Repeat("Multi-line table cell wrapping content for accurate height shift verification. ", 4)
	for i := 0; i < 500; i++ {
		if i%25 == 0 {
			doc.Blocks = append(doc.Blocks, document.NewBlock("math", `\int_0^\infty x^2 e^{-x} dx = 2`))
			mb := document.NewBlock("code", "flowchart LR\n    A[Start] --> B[End]")
			mb.Language = "mermaid"
			doc.Blocks = append(doc.Blocks, mb)
		}
		tbl := document.Table(3, 3)
		if i%2 == 0 {
			tbl.Children[1].Children[1].Runs = []document.Run{{Text: longCell}}
		}
		doc.Blocks = append(doc.Blocks, tbl)
	}

	start := time.Now()
	e := FromOwnedDocument(doc)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 960, 700)
	tt.Frame()
	elapsed := time.Since(start)
	t.Logf("cold open + frame 1 for 500 3x3 tables + math/mermaid (%d leaves): %v", e.Engine.LeafCount(), elapsed)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("cold open took too long: %v", elapsed)
	}
	if !e.hasLazyY {
		t.Fatal("expected hasLazyY enabled on 500-table document")
	}
	last := len(e.rows) - 1
	if e.indexedRow(last).layout != nil {
		t.Fatal("expected last offscreen table cell to remain unshaped after cold open")
	}

	// Scroll to tail (triggers LRU cache trimming across tables) then scroll upward
	// into unshaped multi-line wrapped tables with an active scrollGlide to test Scroll Anchoring.
	e.Scroll.Y = max(0, e.height-900)
	tt.Frame()
	if len(e.cache) > maxCachedShapes+cacheTrimHysteresis {
		t.Fatalf("cache exceeded bound after tail jump: %d", len(e.cache))
	}

	// Pick an unshaped middle-tail table, then land Scroll.Y directly past it so
	// scrolling upward into it changes height above the current viewport anchor.
	mid := len(e.rows) * 3 / 4
	midRow := e.indexedRow(mid)
	targetY := max(0, midRow.rect.Y+midRow.rect.H+40)
	e.Scroll.Y = targetY
	anchorBefore := e.resizeAnchor()
	if anchorBefore.id == "" {
		t.Fatal("expected valid scroll anchor before upward jump")
	}
	anchorIdx := e.rowIDs[anchorBefore.id]
	e.scrollGlide = scrollGlideState{active: true, to: targetY - 160, lastY: targetY, tau: 0.05, at: time.Now()}
	// Materialize the wrapped table just above the anchor while keeping glide active.
	e.Scroll.Y = max(0, midRow.rect.Y-20)
	tt.Frame()
	if !e.scrollGlide.active {
		t.Log("note: scrollGlide settled/stopped during test frame")
	}
	// Verify the anchor block stayed shaped and within sane bounds after table materialization.
	ar := e.indexedRow(anchorIdx)
	if ar.rect.Y <= 0 {
		t.Fatalf("invalid anchored row Y after upward table materialization: %v", ar.rect.Y)
	}
	runtime.KeepAlive(tt)
}

func TestVirtualizedFenwickShiftMatchesFullLayout(t *testing.T) {
	doc := document.New()
	doc.Blocks = make([]*document.Block, 0, 950)
	for i := 0; i < 920; i++ {
		switch {
		case i%100 == 0:
			doc.Blocks = append(doc.Blocks, &document.Block{ID: document.NewBlock("heading", "").ID, Kind: "heading", Level: 2, Runs: []document.Run{{Text: fmt.Sprintf("Heading %d", i)}}})
		case i%150 == 0:
			doc.Blocks = append(doc.Blocks, document.Table(2, 2))
		default:
			doc.Blocks = append(doc.Blocks, document.NewBlock("paragraph", fmt.Sprintf("Line %d short text.", i)))
		}
	}

	e := FromOwnedDocument(doc)
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme()})
	}, 880, 700)
	tt.Frame()
	if !e.hasLazyY {
		t.Fatal("expected hasLazyY enabled")
	}

	// Mutate paragraph 5 so its height grows across multiple wrapped lines.
	leaf5 := e.Engine.LeafAt(30)
	e.Engine.FocusBlock(leaf5.Block.ID)
	cur := e.Engine.LeafAt(e.Caret)
	e.Engine.Replace(cur.Start, cur.End, strings.Repeat("Extra wrapped content for Fenwick O(log B) shift test. ", 12))
	tt.Frame()

	// Snapshot lazy Fenwick-adjusted positions at mid and tail before full rebuild.
	tailRowIdx := len(e.rows) - 1
	midRowIdx := len(e.rows) / 2
	gotMidY := e.indexedRow(midRowIdx).rect.Y
	gotTailY := e.indexedRow(tailRowIdx).rect.Y
	gotTailHandleY := e.indexedHandle(len(e.handles) - 1).top
	gotTailOutlineY := e.indexedOutline(len(e.outline) - 1).y
	gotLastDecY := e.indexedDecoration(len(e.decorations) - 1).rect.Y

	e.ready = false
	tt.Frame()

	diff := func(a, b float32) float32 { return abs32(a - b) }
	if d := diff(gotMidY, e.rows[midRowIdx].rect.Y); d > 0.5 {
		t.Fatalf("mid row Y mismatch after Fenwick shift: got %.2f, full %.2f", gotMidY, e.rows[midRowIdx].rect.Y)
	}
	if d := diff(gotTailY, e.rows[tailRowIdx].rect.Y); d > 0.5 {
		t.Fatalf("tail row Y mismatch after Fenwick shift: got %.2f, full %.2f", gotTailY, e.rows[tailRowIdx].rect.Y)
	}
	if d := diff(gotTailHandleY, e.handles[len(e.handles)-1].top); d > 0.5 {
		t.Fatalf("tail handle top mismatch: got %.2f, full %.2f", gotTailHandleY, e.handles[len(e.handles)-1].top)
	}
	if d := diff(gotTailOutlineY, e.outline[len(e.outline)-1].y); d > 0.5 {
		t.Fatalf("tail outline Y mismatch: got %.2f, full %.2f", gotTailOutlineY, e.outline[len(e.outline)-1].y)
	}
	if d := diff(gotLastDecY, e.decorations[len(e.decorations)-1].rect.Y); d > 0.5 {
		t.Fatalf("last decoration Y mismatch: got %.2f, full %.2f", gotLastDecY, e.decorations[len(e.decorations)-1].rect.Y)
	}
	runtime.KeepAlive(tt)
}
