package document

import (
	"strings"
	"testing"
)

func pageOf(texts ...string) *Engine {
	d := New()
	d.Blocks = nil
	for _, t := range texts {
		d.Blocks = append(d.Blocks, NewBlock("paragraph", t))
	}
	return NewEngine(d)
}

func texts(e *Engine) string {
	var out []string
	for _, b := range e.Doc.Blocks {
		out = append(out, b.Text())
	}
	return strings.Join(out, ",")
}

func ids(e *Engine, at ...int) []string {
	var out []string
	for _, i := range at {
		out = append(out, e.Doc.Blocks[i].ID)
	}
	return out
}

func TestDeleteBlocksIsOneStep(t *testing.T) {
	e := pageOf("a", "b", "c", "d")
	e.DeleteBlocks(ids(e, 2, 1))
	if texts(e) != "a,d" {
		t.Fatal(texts(e))
	}
	if leaf := e.LeafAt(e.Caret); leaf.Block.Text() != "d" {
		t.Fatal("caret should go to the block after", leaf.Block.Text())
	}
	e.Undo()
	if texts(e) != "a,b,c,d" {
		t.Fatal("undo", texts(e))
	}
	e.DeleteBlocks(ids(e, 0, 1, 2, 3))
	if texts(e) != "" || len(e.Doc.Blocks) != 1 || e.Doc.Validate() != nil {
		t.Fatal("deleting everything leaves one empty paragraph")
	}
}

func TestMoveAndShiftBlocks(t *testing.T) {
	e := pageOf("a", "b", "c", "d", "e")
	e.MoveBlocks(ids(e, 1, 2), 5)
	if texts(e) != "a,d,e,b,c" {
		t.Fatal(texts(e))
	}
	e.MoveBlocks(ids(e, 3, 4), 0)
	if texts(e) != "b,c,a,d,e" {
		t.Fatal(texts(e))
	}
	e.ShiftBlocks(ids(e, 0, 1), 1)
	if texts(e) != "a,b,c,d,e" {
		t.Fatal(texts(e))
	}
	e.ShiftBlocks(ids(e, 3, 4), 1)
	if texts(e) != "a,b,c,d,e" {
		t.Fatal("the end does not move", texts(e))
	}
	e.ShiftBlocks(ids(e, 1, 2), -1)
	if texts(e) != "b,c,a,d,e" {
		t.Fatal(texts(e))
	}
	e.Undo()
	if texts(e) != "a,b,c,d,e" {
		t.Fatal("one undo step", texts(e))
	}
}

func TestKindIndentDuplicateBlocks(t *testing.T) {
	e := pageOf("a", "b", "c")
	e.Doc.Blocks = append(e.Doc.Blocks, Table(1, 1))
	e.SetBlocksKind(ids(e, 0, 1, 3), "bullet", 0)
	if e.Doc.Blocks[0].Kind != "bullet" || e.Doc.Blocks[1].Kind != "bullet" || e.Doc.Blocks[3].Kind != "table" || e.Doc.Blocks[2].Kind != "paragraph" {
		t.Fatal("kinds", e.Doc.Blocks[0].Kind, e.Doc.Blocks[3].Kind)
	}
	e.IndentBlocks(ids(e, 0, 1), 1)
	if e.Doc.Blocks[1].Indent != 1 {
		t.Fatal("indent")
	}
	copies := e.DuplicateBlocks(ids(e, 0, 1))
	if texts(e) != "a,b,a,b,c," || len(copies) != 2 || copies[0] == e.Doc.Blocks[0].ID {
		t.Fatal(texts(e))
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	frag := e.BlocksFragment(ids(e, 4, 2))
	if len(frag.Blocks) != 2 || frag.Blocks[0].Text() != "a" || frag.Blocks[1].Text() != "c" {
		t.Fatal("fragment keeps page order")
	}
}

func TestBlocksCarryWhatTheyFold(t *testing.T) {
	d := New()
	d.Blocks = []*Block{{ID: NewID(), Kind: "heading", Level: 1, Runs: []Run{{Text: "h"}}, Folded: true}, NewBlock("paragraph", "x"), NewBlock("paragraph", "y")}
	d.Blocks[2] = &Block{ID: NewID(), Kind: "heading", Level: 1, Runs: []Run{{Text: "next"}}}
	e := NewEngine(d)
	e.DeleteBlocks(ids(e, 0))
	if texts(e) != "next" {
		t.Fatal("a folded heading goes with what it hides", texts(e))
	}
}

func TestToggleFoldsItsContent(t *testing.T) {
	d := New()
	toggle := &Block{ID: NewID(), Kind: "toggle", Runs: []Run{{Text: "更多"}}}
	inner := NewBlock("paragraph", "inside")
	inner.Indent = 1
	after := NewBlock("paragraph", "after")
	d.Blocks = []*Block{toggle, inner, after}
	e := NewEngine(d)
	if !e.Doc.CanFold(toggle.ID) || !e.Doc.FoldableBlocks()[toggle.ID] {
		t.Fatal("a toggle always folds")
	}
	e.ToggleFold(toggle.ID)
	hidden := e.Doc.HiddenBlocks()
	if hidden[inner.ID] != toggle.ID || hidden[after.ID] != "" {
		t.Fatal("a folded toggle hides what is indented under it", hidden)
	}
	s, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(s)
	if err != nil || !back.Blocks[0].Folded || back.Blocks[0].Kind != "toggle" {
		t.Fatal("round trip", err)
	}
	if _, err := Decode(strings.Replace(s, `"version": 8`, `"version": 7`, 1)); err == nil {
		t.Fatal("a v7 document has no toggles")
	}
	// Turned into a paragraph, it no longer folds.
	e.SetBlocksKind([]string{toggle.ID}, "paragraph", 0)
	if e.Doc.Blocks[0].Folded {
		t.Fatal("a paragraph is never folded")
	}
}

func TestEnterInToggle(t *testing.T) {
	d := New()
	toggle := &Block{ID: NewID(), Kind: "toggle", Runs: []Run{{Text: "标题"}}}
	d.Blocks = []*Block{toggle, NewBlock("paragraph", "after")}
	e := NewEngine(d)
	e.SetSelection(2, 2)
	e.Enter(false)
	if b := e.Doc.Blocks[1]; b.Kind != "paragraph" || b.Indent != 1 || e.LeafAt(e.Caret).Block != b {
		t.Fatal("enter at the end of an open toggle writes inside it")
	}
	e.Undo()
	e.ToggleFold(toggle.ID)
	e.Doc.Blocks = append(e.Doc.Blocks[:1], append([]*Block{{ID: NewID(), Kind: "paragraph", Indent: 1, Runs: []Run{{Text: "in"}}}}, e.Doc.Blocks[1:]...)...)
	e.SetSelection(2, 2)
	e.Enter(false)
	if b := e.Doc.Blocks[2]; b.Indent != 0 || b.Text() != "" {
		t.Fatal("enter at a folded toggle goes after what it hides", texts(e))
	}
	empty := &Block{ID: NewID(), Kind: "toggle"}
	e.Doc.Blocks = []*Block{empty}
	e.SetSelection(0, 0)
	e.Enter(false)
	if e.Doc.Blocks[0].Kind != "paragraph" {
		t.Fatal("enter in an empty toggle turns it into a paragraph")
	}
}

func TestWordGoal(t *testing.T) {
	e := pageOf("a")
	e.SetGoal(1000)
	s, err := e.Doc.Encode()
	if err != nil || !strings.Contains(s, `"goal": 1000`) {
		t.Fatal(s, err)
	}
	if _, err := Decode(strings.Replace(s, `"version": 8`, `"version": 7`, 1)); err == nil {
		t.Fatal("a v7 document has no goal")
	}
	e.SetGoal(-1)
	if e.Doc.Goal != 1000 {
		t.Fatal("negative goals are ignored")
	}
	e.Undo()
	if e.Doc.Goal != 0 {
		t.Fatal("undo")
	}
}
