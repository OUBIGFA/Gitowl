package editor

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func blockTexts(e *Editor) string {
	var out []string
	for _, b := range e.Doc.Blocks {
		out = append(out, b.Text())
	}
	return strings.Join(out, ",")
}

func TestEscapeSelectsBlocksAndShiftArrowsExtend(t *testing.T) {
	e, tt := newTester("A\n\nB\n\nC\n\nD")
	focus(e, tt)
	e.SetSelection(2, 2) // in B
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	if got := e.BlockSelection(); len(got) != 1 || got[0] != e.Doc.Blocks[1].ID {
		t.Fatal("escape should select the caret's block", got)
	}
	tt.Key(ui.Shift, ui.KeyDown)
	tt.Key(ui.Shift, ui.KeyDown)
	if len(e.BlockSelection()) != 3 {
		t.Fatal("shift down extends", len(e.BlockSelection()))
	}
	tt.Key(ui.Shift, ui.KeyUp)
	if len(e.BlockSelection()) != 2 {
		t.Fatal("shift up shrinks", len(e.BlockSelection()))
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if !strings.Contains(tt.Clipboard(), "B") || !strings.Contains(tt.Clipboard(), "C") {
		t.Fatal("copy takes the blocks whole", tt.Clipboard())
	}
	tt.Key(ui.Cmd|ui.Alt, ui.Key2)
	if e.Doc.Blocks[1].Kind != "heading" || e.Doc.Blocks[2].Level != 2 {
		t.Fatal("turn every selected block")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyDown)
	if blockTexts(e) != "A,D,B,C" {
		t.Fatal("move down together", blockTexts(e))
	}
	tt.Key(0, ui.KeyBackspace)
	if blockTexts(e) != "A,D" || len(e.BlockSelection()) != 0 {
		t.Fatal("delete together", blockTexts(e))
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if blockTexts(e) != "A,D,B,C" {
		t.Fatal("undo the delete in one step", blockTexts(e))
	}
}

func TestMarginDragSelectsBlocksAndHandleMovesThem(t *testing.T) {
	e := New("A\n\nB\n\nC\n\nD")
	// The margin left of the handles needs a page wider than its column.
	tt := ui.NewTester(func(c *ui.Context) { w, _ := c.Size(); View(c, e, Options{Width: w, Theme: c.Theme()}) }, 1300, 650)
	tt.Frame()
	focus(e, tt)
	bounds, _ := tt.Find("文档")
	x := bounds.X + e.padding - 80
	tt.Press(x, bounds.Y+e.handles[1].top+6)
	tt.Move(x, bounds.Y+e.handles[2].top+6)
	tt.Release(x, bounds.Y+e.handles[2].top+6)
	if got := e.BlockSelection(); len(got) != 2 || got[0] != e.Doc.Blocks[1].ID {
		t.Fatal("margin drag", got)
	}
	// Dragging a selected block's handle moves them all before A's line.
	h, top := e.handles[1], e.handles[0]
	tt.Press(bounds.X+h.rect.X+10, bounds.Y+h.rect.Y+10)
	tt.Move(bounds.X+h.rect.X+10, bounds.Y+top.top+2)
	tt.Release(bounds.X+h.rect.X+10, bounds.Y+top.top+2)
	if blockTexts(e) != "B,C,A,D" {
		t.Fatal("drag moves the selection together", blockTexts(e))
	}
	// Typing ends the selection and writes at the caret.
	tt.Frame()
	e.SelectBlocks(e.Doc.Blocks[0].ID, e.Doc.Blocks[1].ID)
	tt.Frame()
	e.OpenBlockMenu(e.Doc.Blocks[0].ID, "")
	tt.Frame()
	if !tt.HasText("已选 2 个内容块") {
		t.Fatal("the menu acts on the selection")
	}
	if err := tt.Click("无序列表"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Kind != "bullet" || e.Doc.Blocks[1].Kind != "bullet" {
		t.Fatal("menu turn")
	}
	tt.Key(0, ui.KeyEscape)
	if len(e.BlockSelection()) != 0 {
		t.Fatal("escape clears the selection")
	}
}

func TestShiftClickHandleExtendsFromCaret(t *testing.T) {
	e, tt := newTester("A\n\nB\n\nC")
	focus(e, tt)
	e.SetSelection(0, 0)
	tt.Frame()
	bounds, _ := tt.Find("文档")
	h := e.handles[2]
	tt.Move(bounds.X+e.padding+4, bounds.Y+h.top+6)
	tt.ClickAtWith(ui.Shift, bounds.X+h.rect.X+10, bounds.Y+h.rect.Y+10)
	if len(e.BlockSelection()) != 3 {
		t.Fatal("shift-click a handle", len(e.BlockSelection()))
	}
	tt.ClickAt(bounds.X+e.padding+4, bounds.Y+e.handles[0].top+8)
	if len(e.BlockSelection()) != 0 {
		t.Fatal("a click in the text ends the selection")
	}
}

func TestBlockSelectionIsAnnounced(t *testing.T) {
	e, tt := newTester("A\n\nB")
	focus(e, tt)
	tt.Key(0, ui.KeyEscape)
	tt.Key(ui.Shift, ui.KeyDown)
	got := strings.Join(tt.Announcements(), "|")
	if !strings.Contains(got, "已选 1 个内容块") || !strings.Contains(got, "已选 2 个内容块") {
		t.Fatal(got)
	}
}
