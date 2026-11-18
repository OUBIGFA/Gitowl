package editor

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func typeKeys(tt *ui.Tester, s string) {
	for _, r := range s {
		tt.Type(string(r))
	}
}

func TestCaretGlideAndBlockSlideMotion(t *testing.T) {
	e, tt := newTester("First\n\nSecond\n\nThird")
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	startX := e.motion.caret.current.X

	e.SetSelection(5, 5)
	tt.Frame()
	tt.Advance(24 * time.Millisecond)
	midX := e.motion.caret.current.X
	targetX := e.motion.caret.target.X
	if midX <= startX || midX >= targetX {
		t.Fatalf("caret did not glide smoothly: start=%v mid=%v target=%v", startX, midX, targetX)
	}
	tt.Advance(250 * time.Millisecond)
	if e.motion.caret.current.X != targetX {
		t.Fatalf("caret did not settle at target: got=%v want=%v", e.motion.caret.current.X, targetX)
	}

	// Inserting a paragraph before Second slides Second and Third smoothly.
	thirdID := e.Doc.Blocks[2].ID
	e.SetSelection(5, 5)
	tt.Key(0, ui.KeyEnter)
	if slide := e.motion.blocks[thirdID]; slide.dy == 0 {
		t.Fatal("neighboring block did not start a slide transition on Enter")
	}
	tt.Advance(24 * time.Millisecond)
	midDY := e.motion.blocks[thirdID].dy
	if midDY == 0 {
		t.Fatal("block slide snapped immediately instead of easing")
	}
	// Full relayout at dt == 0 must preserve bit-exact rendering.
	tt.Frame()
	pixels := append([]byte(nil), tt.Image().Pix...)
	e.ready = false
	clear(e.cache)
	tt.Frame()
	if !bytes.Equal(pixels, tt.Image().Pix) {
		t.Fatal("motion state broke incremental vs full-layout pixel determinism")
	}
	tt.Advance(300 * time.Millisecond)
	if e.motion.blocks[thirdID].dy != 0 {
		t.Fatalf("block slide did not settle: dy=%v", e.motion.blocks[thirdID].dy)
	}
}

func TestLiveMathPopoverAndCardFlow(t *testing.T) {
	e, tt := newTester("Energy")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	typeKeys(tt, " $")
	if e.Text() != "Energy $$" || e.Caret != 8 || e.inlineMath.blockID == "" {
		t.Fatalf("inline math did not auto-pair and activate: text=%q caret=%d math=%+v", e.Text(), e.Caret, e.inlineMath)
	}
	typeKeys(tt, `E=mc^2`)
	if len(e.rows[0].layout.objects) != 1 || !e.rows[0].layout.objects[0].editing {
		t.Fatalf("inline formula did not stay live-rendered while typing: %+v", e.rows[0].layout.objects)
	}
	tt.Key(0, ui.KeyTab)
	if e.Caret != e.Len() || e.inlineMath.blockID != "" {
		t.Fatalf("Tab did not jump out past closing $: caret=%d len=%d", e.Caret, e.Len())
	}

	// Escape on a newly typed $100 unwraps the auto-paired closing $ into literal $100.
	typeKeys(tt, " cost $100")
	tt.Key(0, ui.KeyEscape)
	if e.Text() != "Energy $E=mc^2$ cost $100" {
		t.Fatalf("Escape did not unwrap literal dollar amount: %q", e.Text())
	}

	// Display math: $$ + Enter opens unified card, double-Enter exits to paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "$$")
	tt.Key(0, ui.KeyEnter)
	if e.Doc.Blocks[1].Kind != "math" || e.rows[1].mathHidden || e.rows[1].y <= e.rows[1].rect.Y+e.rows[1].mathTop {
		t.Fatalf("display math unified card did not open with preview above source: %+v", e.rows[1])
	}
	typeKeys(tt, `x^2 + 1`)
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 3 || e.Doc.Blocks[1].Kind != "math" || e.Doc.Blocks[1].Text() != "x^2 + 1" || e.Doc.Blocks[2].Kind != "paragraph" {
		t.Fatalf("double-Enter did not exit display math cleanly: %+v", e.Doc.Blocks)
	}
}

func TestPairScopeTabOutAndSmartTypography(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// Bracket pair capsule + Tab jump-out.
	typeKeys(tt, "(scope")
	if _, _, ok := e.activePair(); !ok {
		t.Fatal("active pair scope not tracked")
	}
	tt.Key(0, ui.KeyTab)
	if e.Text() != "(scope)" || e.Caret != 7 {
		t.Fatalf("Tab jump-out failed: text=%q caret=%d", e.Text(), e.Caret)
	}

	// Smart curly quotes + straight quote closing skip + contractions.
	e.SetSelection(0, e.Len())
	typeKeys(tt, `"hello" don't`)
	if e.Text() != "“hello” don't" {
		t.Fatalf("smart quotes / contraction mismatch: %q", e.Text())
	}

	// Em-dash + Backspace rollback.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "wait--")
	if e.Text() != "wait—" {
		t.Fatalf("em-dash failed: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "wait--" {
		t.Fatalf("Backspace did not roll back em-dash: %q", e.Text())
	}

	// Ellipsis + Backspace rollback.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "hmm...")
	if e.Text() != "hmm…" {
		t.Fatalf("ellipsis failed: %q", e.Text())
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "hmm..." {
		t.Fatalf("Backspace did not roll back ellipsis: %q", e.Text())
	}

	// Triple hyphen on empty paragraph turns into a horizontal rule + paragraph.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "---")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "rule" || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("--- did not create divider block: %+v", e.Doc.Blocks)
	}
}

func TestLiveInlineMarkdownAndPipeTable(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// **bold** compiles live and Backspace restores literal **bold**.
	typeKeys(tt, "**bold**")
	if e.Text() != "bold" || len(e.Doc.Blocks[0].Runs) != 1 || !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("**bold** did not compile live: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}
	if e.CurrentStyle().Bold {
		t.Fatal("live bold leaked into typing style after closing **")
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Text() != "**bold**" || e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatalf("Backspace did not restore literal **bold**: text=%q runs=%+v", e.Text(), e.Doc.Blocks[0].Runs)
	}

	// *italic*, ~~strike~~, ~tilde~, `code`, and non-misfiring **word*.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "*em* ~~del~~ ~sub~ `fn()` **open*")
	runs := e.Doc.Blocks[0].Runs
	if e.Text() != "em del sub fn() **open*" {
		t.Fatalf("unexpected live-compiled text: %q (runs=%+v)", e.Text(), runs)
	}

	// Pipe table line compiles into a 2x3 table on Enter and undoes in one step.
	e.SetSelection(0, e.Len())
	typeKeys(tt, "| Port | Low | High")
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "table" {
		t.Fatalf("pipe line did not compile into table: %+v", e.Doc.Blocks)
	}
	tbl := e.Doc.Blocks[0]
	if len(tbl.Children) != 2 || len(tbl.Children[0].Children) != 3 || tbl.Children[0].Children[0].Text() != "Port" {
		t.Fatalf("unexpected compiled table shape: %+v", tbl.Children)
	}
	if leaf := e.LeafAt(e.Caret); leaf.Block.ID != tbl.Children[1].Children[0].ID {
		t.Fatal("caret did not land in Row 1 Col 0 of compiled pipe table")
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Kind != "paragraph" || e.Text() != "| Port | Low | High" {
		t.Fatalf("pipe table did not undo in one step: %q", e.Text())
	}
}

func TestTableEnterFlowAndRowDragReorder(t *testing.T) {
	e, tt := newTester("| ColA | ColB |\n| --- | --- |\n| R1A | R1B |")
	focus(e, tt)
	tbl := e.Doc.Blocks[0]

	// Enter in Row 0 Col 0 moves straight down to Row 1 Col 0.
	l00, _ := e.Engine.LeafByID(tbl.Children[0].Children[0].ID)
	e.SetSelection(l00.End, l00.End)
	tt.Key(0, ui.KeyEnter)
	if e.LeafAt(e.Caret).Block.ID != tbl.Children[1].Children[0].ID {
		t.Fatal("Enter did not move down to same column")
	}

	// Enter at Row 1 Col 1 (end of filled row) appends Row 2 and wraps to Col 0.
	l11, _ := e.Engine.LeafByID(tbl.Children[1].Children[1].ID)
	e.SetSelection(l11.End, l11.End)
	tt.Key(0, ui.KeyEnter)
	tbl = e.Doc.Blocks[0]
	if len(tbl.Children) != 3 || e.LeafAt(e.Caret).Block.ID != tbl.Children[2].Children[0].ID {
		t.Fatal("Enter on last column did not append row and focus Col 0")
	}

	// Pressing Enter again on the empty trailing row at Col 0 exits the table cleanly.
	tt.Key(0, ui.KeyEnter)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "table" || len(e.Doc.Blocks[0].Children) != 2 || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("double-Enter did not exit trailing blank table row: %+v", e.Doc.Blocks)
	}

	// Add a second data row and drag row 2 above row 1 using its rowBar.
	l10, _ := e.Engine.LeafByID(e.Doc.Blocks[0].Children[1].Children[0].ID)
	e.SetSelection(l10.End, l10.End)
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "R2A")
	tt.Frame()

	bounds, _ := tt.Find("文档")
	d, ok := e.tableDecoration(e.Doc.Blocks[0].ID)
	if !ok {
		t.Fatal("missing table decoration")
	}
	rb2 := rowBar(d, 2)
	rb1 := rowBar(d, 1)
	sx, sy := bounds.X+rb2.X+rb2.W/2, bounds.Y+rb2.Y+rb2.H/2
	ty := bounds.Y + rb1.Y + 4
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(sx, ty)
	tt.Frame()
	tt.Release(sx, ty)
	tt.Frame()

	tbl = e.Doc.Blocks[0]
	if tbl.Children[1].Children[0].Text() != "R2A" || tbl.Children[2].Children[0].Text() != "R1A" {
		t.Fatalf("row drag did not reorder rows: r1=%q r2=%q", tbl.Children[1].Children[0].Text(), tbl.Children[2].Children[0].Text())
	}
}

func TestImageCaptionEnterAndWidthPill(t *testing.T) {
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 600, 360)))
	e, tt := imageTester("![photo.png](photo.png)", 900, ImageResource{Bitmap: bitmap}, nil, nil)
	imgID := e.Doc.Blocks[0].ID

	e.FocusInsertedImage(imgID)
	tt.Frame()
	typeKeys(tt, "Star trails, 2 hours")
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "Body paragraph below")
	tt.Frame()

	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Kind != "image" || e.Doc.Blocks[0].Text() != "Star trails, 2 hours" {
		t.Fatalf("image caption flow failed: %+v", e.Doc.Blocks)
	}
	if e.Doc.Blocks[1].Kind != "paragraph" || e.Doc.Blocks[1].Text() != "Body paragraph below" {
		t.Fatalf("Enter from image caption did not create paragraph below: %+v", e.Doc.Blocks)
	}
	if !e.captionVisible(e.Doc.Blocks[0]) {
		t.Fatal("user-typed caption disappeared after leaving the image block")
	}

	// Hover the image to reveal its resize handles and drag to verify live width percentage state.
	bounds, _ := tt.Find("文档")
	pic := e.rows[0].picture.bounds
	tt.Move(bounds.X+pic.X+pic.W/2, bounds.Y+pic.Y+pic.H/2)
	tt.Frame()
	r, ok := tt.Find("调整图片宽度")
	if !ok {
		t.Fatal("missing image resize handle")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	tt.Move(r.X-60, r.Y+r.H/2)
	tt.Frame()
	if e.imageResize.id != imgID {
		t.Fatal("image resize state not active during drag")
	}
	tt.Release(r.X-60, r.Y+r.H/2)
	tt.Frame()
}

func TestSmoothScrollGlideManualInterruptAndFootnoteFlash(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("Line ")
		b.WriteByte(byte('A' + i%26))
		b.WriteString("\n\n")
	}
	b.WriteString("Anchor[^1]\n\n[^1]: Note target at bottom")
	e := New(b.String())
	typewriter := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Typewriter: typewriter})
	}, 900, 420)
	tt.Frame()
	focus(e, tt)

	// Move caret to paragraph 20: typewriter mode starts a smooth scroll glide.
	leaf20, _ := e.Engine.LeafByID(e.Doc.Blocks[20].ID)
	e.SetSelection(leaf20.Start, leaf20.Start)
	e.reveal = true
	tt.Frame()
	if !e.scrollGlide.active || e.Scroll.Y <= 0 || e.Scroll.Y >= e.scrollGlide.to {
		t.Fatalf("typewriter glide did not start smoothly: active=%v y=%v to=%v", e.scrollGlide.active, e.Scroll.Y, e.scrollGlide.to)
	}
	wantY := e.scrollGlide.to
	tt.Advance(400 * time.Millisecond)
	if e.scrollGlide.active || abs32(e.Scroll.Y-wantY) > 1 {
		t.Fatalf("scroll glide did not settle at target: active=%v y=%v want=%v", e.scrollGlide.active, e.Scroll.Y, wantY)
	}

	// Start another glide, then interrupt manually — glide cancels immediately.
	e.SetSelection(0, 0)
	e.reveal = true
	tt.Frame()
	if !e.scrollGlide.active {
		t.Fatal("expected upward scroll glide")
	}
	manualY := e.Scroll.Y + 45
	e.Scroll.Y = manualY
	tt.Frame()
	if e.scrollGlide.active || abs32(e.Scroll.Y-manualY) > 1 {
		t.Fatalf("manual scroll did not cancel glide: active=%v y=%v want=%v", e.scrollGlide.active, e.Scroll.Y, manualY)
	}

	// Footnote jump and return flash their destination blocks.
	typewriter = false
	anchorBlock := e.Doc.Blocks[40]
	noteBlock := e.Doc.Blocks[41]
	anchorLeaf, _ := e.Engine.LeafByID(anchorBlock.ID)
	e.SetSelection(anchorLeaf.End, anchorLeaf.End)
	tt.Frame()
	if !e.FollowFootnote() {
		t.Fatal("FollowFootnote did not jump to note")
	}
	if e.bookmarks.flash != noteBlock.ID {
		t.Fatalf("FollowFootnote did not flash note block: got=%q want=%q", e.bookmarks.flash, noteBlock.ID)
	}
	if !e.ReturnFootnote() {
		t.Fatal("ReturnFootnote did not jump back to reference")
	}
	if e.bookmarks.flash != anchorBlock.ID {
		t.Fatalf("ReturnFootnote did not flash origin block: got=%q want=%q", e.bookmarks.flash, anchorBlock.ID)
	}
}

func TestFocusAfterglowFoldChevronAndTaskBounce(t *testing.T) {
	e := New("# Section\n\nBody one\n\n- [ ] Ship release")
	dim := true
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Dim: dim})
	}, 900, 500)
	tt.Frame()
	focus(e, tt)

	secID := e.Doc.Blocks[0].ID
	bodyID := e.Doc.Blocks[1].ID
	taskID := e.Doc.Blocks[2].ID
	e.SetSelection(0, 0)
	tt.Frame()

	// Moving focus from Section to Body one lights Body one immediately and leaves
	// a decaying spotlight afterglow on Section.
	bodyLeaf, _ := e.Engine.LeafByID(bodyID)
	e.SetSelection(bodyLeaf.Start, bodyLeaf.Start)
	tt.Frame()
	if g := e.motion.dimGlow[secID]; g <= 0 {
		t.Fatalf("exited block did not get focus spotlight afterglow: %v", g)
	}
	if g := e.motion.dimGlow[bodyID]; g != 0 {
		t.Fatalf("entered block should light up immediately without dimGlow: %v", g)
	}
	tt.Advance(500 * time.Millisecond)
	if g := e.motion.dimGlow[secID]; g != 0 {
		t.Fatalf("focus afterglow did not decay to zero: %v", g)
	}

	// Folding Section rotates its chevron from 0 toward -90; unfolding animates
	// revealed child blocks smoothly.
	dim = false
	e.toggleFold(secID)
	tt.Frame()
	tt.Advance(24 * time.Millisecond)
	angle := e.motion.foldAngles[secID]
	if angle >= 0 || angle <= -90 {
		t.Fatalf("fold chevron did not animate smoothly between 0 and -90: %v", angle)
	}
	tt.Advance(350 * time.Millisecond)
	if e.motion.foldAngles[secID] != -90 {
		t.Fatalf("fold chevron did not settle at -90: %v", e.motion.foldAngles[secID])
	}

	e.toggleFold(secID)
	tt.Frame()
	if e.motion.blocks[bodyID].dy == 0 || e.motion.foldFade[bodyID] == 0 {
		t.Fatalf("unfolded child block did not glide/fade in: dy=%v fade=%v", e.motion.blocks[bodyID].dy, e.motion.foldFade[bodyID])
	}
	tt.Advance(400 * time.Millisecond)
	if e.motion.blocks[bodyID].dy != 0 || e.motion.foldFade[bodyID] != 0 {
		t.Fatalf("unfold animation did not settle cleanly: dy=%v fade=%v", e.motion.blocks[bodyID].dy, e.motion.foldFade[bodyID])
	}

	// Clicking the task checkbox checks it and plays the spring/checkmark animation.
	var taskRow row
	for _, r := range e.rows {
		if r.leaf.Block.ID == taskID {
			taskRow = r
			break
		}
	}
	if !e.toggleTask(taskRow.leaf.Start, taskRow.x-16) {
		t.Fatal("toggleTask did not toggle checkbox")
	}
	if !e.Doc.Blocks[2].Checked {
		t.Fatal("task block not marked Checked")
	}
	if _, ok := e.taskAnim[taskID]; !ok {
		t.Fatal("task checkbox spring animation was not armed")
	}
	tt.Frame()
	tt.Advance(260 * time.Millisecond)
	tt.Frame()
	if _, ok := e.taskAnim[taskID]; ok {
		t.Fatal("task checkbox animation did not finish and clean up")
	}
}

func TestTableColumnDragReorderAndEqualWidthSnap(t *testing.T) {
	e, tt := newTester("| ColA | ColB | ColC |\n| --- | --- | --- |\n| 1 | 2 | 3 |")
	focus(e, tt)
	tblID := e.Doc.Blocks[0].ID
	l00, _ := e.Engine.LeafByID(e.Doc.Blocks[0].Children[0].Children[0].ID)
	e.SetSelection(l00.End, l00.End)
	tt.Frame()

	bounds, _ := tt.Find("文档")
	d, ok := e.tableDecoration(tblID)
	if !ok {
		t.Fatal("missing table decoration")
	}
	cb2 := columnBar(d, 2)
	cb0 := columnBar(d, 0)
	sx, sy := bounds.X+cb2.X+cb2.W/2, bounds.Y+cb2.Y+cb2.H/2
	tx := bounds.X + cb0.X + 8
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(tx, sy)
	tt.Frame()
	if !e.tableColDrag.active || e.tableColDrag.to != 0 {
		t.Fatalf("column drag did not target column 0: %+v", e.tableColDrag)
	}
	tt.Release(tx, sy)
	tt.Frame()

	hdr := e.Doc.Blocks[0].Children[0].Children
	if hdr[0].Text() != "ColC" || hdr[1].Text() != "ColA" || hdr[2].Text() != "ColB" {
		t.Fatalf("column drag reorder mismatch: %q, %q, %q", hdr[0].Text(), hdr[1].Text(), hdr[2].Text())
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	hdr = e.Doc.Blocks[0].Children[0].Children
	if hdr[0].Text() != "ColA" || hdr[1].Text() != "ColB" || hdr[2].Text() != "ColC" {
		t.Fatal("column drag reorder did not undo in a single step")
	}

	// Start with unequal 2-column widths and drag within 6 DIP of equal width:
	// magnetic snap commits exact 50% / 50% widths.
	e2, tt2 := newTester("| Left | Right |\n| --- | --- |\n| a | b |")
	focus(e2, tt2)
	id2 := e2.Doc.Blocks[0].ID
	e2.Engine.SetWidths(id2, []float32{200, 400})
	tt2.Frame()
	if len(e2.resizers) != 1 {
		t.Fatalf("expected 1 column resizer, got %d", len(e2.resizers))
	}
	res := e2.resizers[0]
	bounds2, _ := tt2.Find("文档")
	equalX := res.x + (res.widths[1]-res.widths[0])*0.5
	tt2.Press(bounds2.X+res.x, bounds2.Y+res.y+12)
	tt2.Move(bounds2.X+equalX+3, bounds2.Y+res.y+12)
	tt2.Frame()
	tt2.Release(bounds2.X+equalX+3, bounds2.Y+res.y+12)
	tt2.Frame()

	w := e2.Doc.Blocks[0].Widths
	if len(w) != 2 || abs32(w[0]-w[1]) > 0.01 {
		t.Fatalf("column resizer did not magnetically snap to equal widths: %v", w)
	}
}

func TestLivePrependBlockAndInlineMarkdown(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)

	// 1. Prepend "## " before existing text -> heading level 2.
	typeKeys(tt, "我要变二级标题")
	e.SetSelection(0, 0)
	tt.Frame()
	typeKeys(tt, "## ")
	if b := e.Doc.Blocks[0]; b.Kind != "heading" || b.Level != 2 || b.Text() != "我要变二级标题" || e.Caret != 0 {
		t.Fatalf("live prepend ## failed: kind=%s level=%d text=%q caret=%d", b.Kind, b.Level, b.Text(), e.Caret)
	}

	// 2. Prepend " > " before existing text -> quote.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "我要变引用")
	l := e.Engine.LeafAt(e.Caret)
	e.SetSelection(l.Start, l.Start)
	tt.Frame()
	typeKeys(tt, " > ")
	if b := e.Doc.Blocks[1]; b.Kind != "quote" || b.Text() != "我要变引用" {
		t.Fatalf("live prepend > failed: kind=%s text=%q", b.Kind, b.Text())
	}

	// 3. Prepend "- [ ] " before existing text -> task.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	// Empty quote block Enter drops quote style to a plain paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "我要变任务")
	l = e.Engine.LeafAt(e.Caret)
	e.SetSelection(l.Start, l.Start)
	tt.Frame()
	typeKeys(tt, "- [ ] ")
	if b := e.Doc.Blocks[2]; b.Kind != "task" || b.Checked || b.Text() != "我要变任务" {
		t.Fatalf("live prepend - [ ] failed: kind=%s checked=%v text=%q", b.Kind, b.Checked, b.Text())
	}

	// 4. Prepend opening "**" before text ending with "**文字" -> live bold + Backspace rollback.
	e.SetSelection(e.Len(), e.Len())
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	// Empty task block Enter drops task style to a plain paragraph.
	tt.Key(0, ui.KeyEnter)
	typeKeys(tt, "这是一段重点**文字")
	l = e.Engine.LeafAt(e.Caret)
	beforeBold := l.Start + len([]rune("这是一段"))
	e.SetSelection(beforeBold, beforeBold)
	tt.Frame()
	typeKeys(tt, "**")
	b := e.Doc.Blocks[3]
	if b.Text() != "这是一段重点文字" || len(b.Runs) != 3 || !b.Runs[1].Style.Bold || b.Runs[1].Text != "重点" {
		t.Fatalf("live prepend ** failed: text=%q runs=%+v", b.Text(), b.Runs)
	}
	tt.Key(0, ui.KeyBackspace)
	if e.Doc.Blocks[3].Text() != "这是一段**重点**文字" {
		t.Fatalf("backspace did not roll back reverse inline bold: %q", e.Doc.Blocks[3].Text())
	}
}
