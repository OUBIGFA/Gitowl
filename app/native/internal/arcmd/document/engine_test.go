package document

import (
	"math/rand"
	"reflect"
	"testing"
)

func textEngine(text string) *Engine {
	d := New()
	d.Blocks[0].Runs = []Run{{Text: text}}
	return NewEngine(d)
}

func TestRichTransactionsAndRoundTrip(t *testing.T) {
	e := textEngine("Hello 世界😀")
	e.SetSelection(6, 10)
	e.Toggle("bold")
	e.Toggle("underline")
	e.SetLink("https://example.com")
	if e.Doc.PlainText() != "Hello 世界😀" {
		t.Fatal("formatting inserted markup")
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.Doc, loaded) {
		t.Fatal("native round trip changed the model")
	}
	e.Undo()
	if e.CurrentStyle().Link != "" {
		t.Fatal("undo link")
	}
	e.Redo()
	if e.CurrentStyle().Link == "" {
		t.Fatal("redo link")
	}
}

func TestSplitJoinPreservesRuns(t *testing.T) {
	e := textEngine("hello world")
	e.SetSelection(0, 5)
	e.Toggle("bold")
	e.SetSelection(5, 5)
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.PlainText() != "hello\n world" {
		t.Fatal(e.Doc.PlainText())
	}
	if !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("lost marks")
	}
	e.Replace(5, 6, "")
	if len(e.Doc.Blocks) != 1 || e.Doc.PlainText() != "hello world" {
		t.Fatal("join", e.Doc.PlainText())
	}
	e.Undo()
	e.Undo()
	if e.Doc.PlainText() != "hello world" || !e.Doc.Blocks[0].Runs[0].Style.Bold {
		t.Fatal("undo structural edit")
	}
}

func TestNestedTransactionsAndContainers(t *testing.T) {
	e := textEngine("")
	before := e.Doc.Clone()
	e.Edit(func() { e.Replace(0, 0, "A"); e.Insert(Table(2, 3)); e.Insert(Columns()) })
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Undo()
	if !reflect.DeepEqual(e.Doc, before) {
		t.Fatal("nested changes did not share one undo")
	}
	e.Redo()
	if len(e.Doc.Blocks) != 3 {
		t.Fatal("redo blocks")
	}
	id := e.Doc.Blocks[1].ID
	e.FocusBlock(id)
	e.ResizeTable(true, 1)
	e.ResizeTable(false, 1)
	if len(e.Doc.Blocks[1].Children) != 3 || len(e.Doc.Blocks[1].Children[0].Children) != 4 {
		t.Fatal("table resize")
	}
	e.Duplicate(id)
	if err := e.Doc.Validate(); err != nil {
		t.Fatal("duplicate IDs", err)
	}
	e.MoveBlock(id, 0)
	if e.Doc.Blocks[0].ID != id {
		t.Fatal("move failed")
	}
	e.DeleteBlock(id)
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectUnsupportedOrCorruptFormat(t *testing.T) {
	for _, text := range []string{`{"format":"arcmd","version":9,"blocks":[{"id":"a","kind":"paragraph"}]}`, `{"format":"arcmd","version":4,"icon":"🚀","blocks":[{"id":"a","kind":"paragraph"}]}`, `{"format":"arcmd","version":5,"cover":"../x.png","blocks":[{"id":"a","kind":"paragraph"}]}`, `{"format":"arcmd","version":5,"cover":"gradient:nope","blocks":[{"id":"a","kind":"paragraph"}]}`, `{"format":"arcmd","version":1,"blocks":[]}`, `{"format":"arcmd","version":1,"blocks":[{"id":"a","kind":"future"}]}`, `{"format":"arcmd","version":1,"blocks":[{"id":"a","kind":"paragraph","future":true}]}`, `{"format":"arcmd","version":1,"blocks":[{"id":"a","kind":"paragraph"},{"id":"a","kind":"paragraph"}]}`} {
		if _, err := Decode(text); err == nil {
			t.Fatal("accepted unsupported document", text)
		}
	}
}

func TestRandomEditsKeepValidDocument(t *testing.T) {
	e := textEngine("Start")
	rng := rand.New(rand.NewSource(17))
	for range 450 {
		n := Len(e.Doc.PlainText())
		a, z := rng.Intn(n+1), rng.Intn(n+1)
		e.SetSelection(a, z)
		if rng.Intn(3) == 0 {
			e.SetSelection(a, a)
		}
		switch rng.Intn(15) {
		case 0, 1, 2:
			e.Replace(min(e.Anchor, e.Caret), max(e.Anchor, e.Caret), []string{"文", "😀", "a\nb", ""}[rng.Intn(4)])
		case 3:
			if rng.Intn(2) == 0 {
				e.Toggle("bold")
			} else {
				c, h := TextColors[rng.Intn(len(TextColors))], TextHighlights[rng.Intn(len(TextHighlights))]
				e.SetTextColor(&c, &h)
			}
		case 4:
			e.Insert(Table(2, 2))
		case 5:
			e.Insert(Columns())
		case 6:
			e.Undo()
		case 7:
			e.Redo()
		case 14:
			fragment := New()
			fragment.Blocks = []*Block{NewBlock("paragraph", "粘贴"), NewBlock("paragraph", "末尾😀")}
			e.Paste(fragment)
		case 12:
			for _, b := range e.Doc.Blocks {
				if b.Kind == "table" {
					r0, c0 := rng.Intn(len(b.Children)), rng.Intn(len(b.Children[0].Children))
					e.MergeCells(b.ID, CellRange{r0, c0, min(r0+rng.Intn(2), len(b.Children)-1), min(c0+rng.Intn(2), len(b.Children[0].Children)-1)})
					break
				}
			}
		case 13:
			if len(e.Doc.Blocks) > 0 {
				b := e.Doc.Blocks[rng.Intn(len(e.Doc.Blocks))]
				e.ToggleFold(b.ID)
			}
		case 10, 11:
			for _, b := range e.Doc.Blocks {
				if b.Kind == "table" {
					if rng.Intn(2) == 0 {
						e.TableInsert(b.ID, rng.Intn(2) == 0, rng.Intn(4))
					} else {
						e.TableDelete(b.ID, rng.Intn(2) == 0, rng.Intn(3))
					}
					break
				}
			}
		case 8, 9:
			if !e.Join(rng.Intn(2) == 0) {
				e.Replace(max(0, e.Caret-1), e.Caret, "")
			}
		}
		if err := e.Doc.Validate(); err != nil {
			t.Fatal(err)
		}
		length := Len(e.Doc.PlainText())
		assertLeafOffsets(t, e)
		if e.Anchor < 0 || e.Caret < 0 || e.Anchor > length || e.Caret > length {
			t.Fatalf("invalid selection %d %d / %d", e.Anchor, e.Caret, length)
		}
	}
}

func TestParagraphAutoformatAndSoftBreak(t *testing.T) {
	e := textEngine("")
	e.Replace(0, 0, "##")
	e.Replace(2, 2, " ")
	if e.Doc.Blocks[0].Kind != "heading" || e.Doc.Blocks[0].Level != 2 || e.Doc.PlainText() != "" {
		t.Fatal("heading input rule")
	}
	e.Replace(0, 0, "Title")
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatal("heading Enter")
	}
	e.Replace(e.Caret, e.Caret, "One")
	e.Enter(true)
	e.Replace(e.Caret, e.Caret, "Two")
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[1].Text() != "One\u2028Two" {
		t.Fatal("soft break")
	}
}

func TestVersionOneMigratesAndColorsValidate(t *testing.T) {
	d, err := Decode(`{"format":"arcmd","version":1,"blocks":[{"id":"a","kind":"callout","runs":[{"text":"x"}]}]}`)
	if err != nil || d.Version != CurrentVersion {
		t.Fatal("migrate", err)
	}
	for _, text := range []string{
		`{"format":"arcmd","version":1,"blocks":[{"id":"a","kind":"callout","color":"red"}]}`,
		`{"format":"arcmd","version":2,"blocks":[{"id":"a","kind":"callout","color":"pink"}]}`,
		`{"format":"arcmd","version":2,"blocks":[{"id":"a","kind":"paragraph","color":"red"}]}`,
	} {
		if _, err := Decode(text); err == nil {
			t.Fatal("accepted", text)
		}
	}
	d, err = Decode(`{"format":"arcmd","version":2,"blocks":[{"id":"a","kind":"callout","color":"yellow"}]}`)
	if err != nil || d.Blocks[0].Color != "yellow" {
		t.Fatal("color", err)
	}
	encoded, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(encoded)
	if err != nil || again.Blocks[0].Color != "yellow" {
		t.Fatal("round trip", err)
	}
}

func TestCalloutColorUndoAndKindChange(t *testing.T) {
	d := New()
	d.Blocks[0] = NewBlock("callout", "note")
	e := NewEngine(d)
	e.SetColor("", "red")
	if e.Doc.Blocks[0].Color != "red" {
		t.Fatal("color")
	}
	e.SetColor("", "pink")
	if e.Doc.Blocks[0].Color != "red" {
		t.Fatal("unknown color accepted")
	}
	e.Undo()
	if e.Doc.Blocks[0].Color != "" {
		t.Fatal("undo color")
	}
	e.Redo()
	e.SetKind("paragraph", 0)
	if e.Doc.Blocks[0].Color != "" || e.Doc.Validate() != nil {
		t.Fatal("kind change kept color", e.Doc.Validate())
	}
}

// blocks builds a document of top-level blocks for structural tests.
func blocks(bs ...*Block) *Engine {
	d := New()
	d.Blocks = bs
	return NewEngine(d)
}

func kinds(e *Engine) []string {
	var out []string
	for _, b := range e.Doc.Blocks {
		out = append(out, b.Kind+":"+b.Text())
	}
	return out
}

func TestBackspaceRemovesEmptyStructures(t *testing.T) {
	// Backspace in an emptied table removes the table, caret above it.
	e := blocks(NewBlock("paragraph", "Intro"), Table(2, 2), NewBlock("paragraph", "After"))
	e.SetSelection(6, 6) // the first cell
	if !e.Join(true) {
		t.Fatal("join not handled")
	}
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:Intro", "paragraph:After"}) {
		t.Fatal(got)
	}
	if e.Caret != 5 {
		t.Fatal("caret", e.Caret)
	}
	e.Undo()
	if e.Doc.Blocks[1].Kind != "table" {
		t.Fatal("undo table removal")
	}

	// An emptied column layout as the only block leaves a paragraph.
	e = blocks(Columns())
	e.SetSelection(1, 1)
	e.Join(true)
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:"}) || e.Doc.Validate() != nil {
		t.Fatal(got)
	}

	// A table with content is never merged: Backspace in a cell steps over.
	e = blocks(Table(1, 2))
	e.Doc.Blocks[0].Children[0].Children[0].Runs = []Run{{Text: "A"}}
	e.SetSelection(2, 2)
	e.Join(true)
	if len(e.Doc.Blocks[0].Children[0].Children) != 2 || e.Caret != 1 {
		t.Fatal("cells merged or caret stayed", e.Caret)
	}

	// An empty paragraph after a table goes; one with text stays put.
	e = blocks(Table(1, 1), NewBlock("paragraph", ""))
	e.Doc.Blocks[0].Children[0].Children[0].Runs = []Run{{Text: "A"}}
	e.SetSelection(2, 2)
	e.Join(true)
	if len(e.Doc.Blocks) != 1 || e.Caret != 1 {
		t.Fatal("empty paragraph after table", kinds(e))
	}
	e = blocks(Table(1, 1), NewBlock("paragraph", "B"))
	e.Doc.Blocks[0].Children[0].Children[0].Runs = []Run{{Text: "A"}}
	e.SetSelection(2, 2)
	e.Join(true)
	if got := kinds(e); len(got) != 2 || got[1] != "paragraph:B" || e.Caret != 1 {
		t.Fatal(got)
	}
}

func TestBackspaceUnstylesThenJoins(t *testing.T) {
	callout := NewBlock("callout", "")
	callout.Color = "red"
	e := blocks(NewBlock("paragraph", "Intro"), callout)
	e.SetSelection(6, 6)
	e.Join(true)
	if b := e.Doc.Blocks[1]; b.Kind != "paragraph" || b.Color != "" {
		t.Fatal("callout kept its style", b.Kind, b.Color)
	}
	if e.Join(true) {
		t.Fatal("a plain paragraph should join as text")
	}
	e.Replace(5, 6, "")
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:Intro"}) {
		t.Fatal(got)
	}

	// A rule goes with Backspace from the paragraph after it.
	e = blocks(NewBlock("paragraph", "A"), NewBlock("rule", ""), NewBlock("paragraph", "B"))
	e.SetSelection(3, 3)
	e.Join(true)
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:A", "paragraph:B"}) || e.Caret != 2 {
		t.Fatal(got, e.Caret)
	}

	// Code with text is stepped over, not merged into a paragraph.
	e = blocks(NewBlock("paragraph", "A"), NewBlock("code", "x\ny"), NewBlock("paragraph", "B"))
	e.SetSelection(1, 1)
	e.Join(false)
	if len(e.Doc.Blocks) != 3 || e.Caret != 2 {
		t.Fatal("forward delete merged code", kinds(e), e.Caret)
	}
}

func TestSelectionDeleteRemovesCoveredStructures(t *testing.T) {
	e := blocks(NewBlock("paragraph", "Intro"), Table(2, 2), Columns(), NewBlock("paragraph", "After"))
	end := Len(e.Doc.PlainText())
	e.Replace(2, end-2, "")
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:Iner"}) || e.Caret != 2 {
		t.Fatal(got, e.Caret)
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}

	// A selection across cells of one row clears them without merging.
	e = blocks(Table(1, 3))
	for i, text := range []string{"ab", "cd", "ef"} {
		e.Doc.Blocks[0].Children[0].Children[i].Runs = []Run{{Text: text}}
	}
	e.Replace(1, 7, "X")
	row := e.Doc.Blocks[0].Children[0].Children
	if len(row) != 3 || row[0].Text() != "aX" || row[1].Text() != "" || row[2].Text() != "f" {
		t.Fatal("row changed shape", row[0].Text(), row[1].Text(), row[2].Text())
	}

	// A selection that is exactly a whole table replaces it with the text.
	e = blocks(NewBlock("paragraph", "A"), Table(1, 2), NewBlock("paragraph", "B"))
	e.Replace(1, 4, "")
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:AB"}) {
		t.Fatal(got)
	}
}

func TestTableInsertAndDeleteLines(t *testing.T) {
	table := Table(2, 2)
	table.Widths = []float32{1, 3}
	e := blocks(NewBlock("paragraph", "A"), table)
	id := table.ID
	e.TableInsert(id, false, 1)
	tb := e.Doc.Blocks[1]
	if len(tb.Children[0].Children) != 3 || len(tb.Widths) != 3 || tb.Widths[1] != 2 {
		t.Fatal("insert column", len(tb.Children[0].Children), tb.Widths)
	}
	if leaf := e.LeafAt(e.Caret); leaf.Block != tb.Children[0].Children[1] {
		t.Fatal("caret not in the new column")
	}
	e.TableInsert(id, true, 0)
	if len(tb.Children) != 3 || e.LeafAt(e.Caret).Block != tb.Children[0].Children[0] {
		t.Fatal("insert row")
	}
	e.TableDelete(id, false, 0)
	if len(tb.Children[0].Children) != 2 || len(tb.Widths) != 2 || tb.Widths[0] != 2 {
		t.Fatal("delete column", tb.Widths)
	}
	e.TableDelete(id, true, 2)
	if len(tb.Children) != 2 {
		t.Fatal("delete row")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	// Deleting the only row removes the table.
	e = blocks(NewBlock("paragraph", "A"), Table(1, 2))
	e.TableDelete(e.Doc.Blocks[1].ID, true, 0)
	if len(e.Doc.Blocks) != 1 || e.Caret != 1 {
		t.Fatal("last row", kinds(e), e.Caret)
	}
	e.Undo()
	if e.Doc.Blocks[1].Kind != "table" {
		t.Fatal("undo")
	}
}

func TestTextColorsMigrateValidateAndUndo(t *testing.T) {
	// Versions 1 and 2 migrate; neither may carry text colors.
	for _, v := range []string{"1", "2"} {
		d, err := Decode(`{"format":"arcmd","version":` + v + `,"blocks":[{"id":"a","kind":"paragraph","runs":[{"text":"x"}]}]}`)
		if err != nil || d.Version != CurrentVersion {
			t.Fatal("migrate", v, err)
		}
		if _, err := Decode(`{"format":"arcmd","version":` + v + `,"blocks":[{"id":"a","kind":"paragraph","runs":[{"text":"x","style":{"color":"red"}}]}]}`); err == nil {
			t.Fatal("version", v, "accepted a text color")
		}
	}
	for _, style := range []string{`{"color":"pink"}`, `{"highlight":"light-pink"}`} {
		if _, err := Decode(`{"format":"arcmd","version":3,"blocks":[{"id":"a","kind":"paragraph","runs":[{"text":"x","style":` + style + `}]}]}`); err == nil {
			t.Fatal("accepted", style)
		}
	}
	e := textEngine("Hello world")
	e.SetSelection(6, 11)
	red, marker := "red", "light-yellow"
	e.SetTextColor(&red, &marker)
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 2 || runs[1].Style.Color != "red" || runs[1].Style.Highlight != "light-yellow" || runs[0].Style.Color != "" {
		t.Fatal("color", runs)
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(again, e.Doc) {
		t.Fatal("round trip", err)
	}
	none := ""
	e.SetTextColor(&none, nil)
	if r := e.Doc.Blocks[0].Runs[1]; r.Style.Color != "" || r.Style.Highlight != "light-yellow" {
		t.Fatal("clear color only", r.Style)
	}
	e.Undo()
	if e.Doc.Blocks[0].Runs[1].Style.Color != "red" {
		t.Fatal("undo")
	}
	// A collapsed caret colors what is typed next.
	e.SetSelection(11, 11)
	blue := "blue"
	e.SetTextColor(&blue, &none)
	e.Replace(11, 11, "!")
	if last := e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1]; last.Text != "!" || last.Style.Color != "blue" || last.Style.Highlight != "" {
		t.Fatal("typing color", last)
	}
}

func TestFoldHeadingsAndListsOutsideHistory(t *testing.T) {
	h1, p1, h2, p2, h1b := NewBlock("heading", "A"), NewBlock("paragraph", "a"), NewBlock("heading", "B"), NewBlock("paragraph", "b"), NewBlock("heading", "C")
	h1.Level, h2.Level, h1b.Level = 1, 2, 1
	item, child, next := NewBlock("bullet", "x"), NewBlock("bullet", "y"), NewBlock("bullet", "z")
	child.Indent = 1
	e := blocks(h1, p1, h2, p2, h1b, item, child, next)
	if !e.Doc.CanFold(h1.ID) || !e.Doc.CanFold(item.ID) || e.Doc.CanFold(next.ID) || e.Doc.CanFold(p1.ID) {
		t.Fatal("can fold")
	}
	e.ToggleFold(h1.ID)
	e.ToggleFold(item.ID)
	hidden := e.Doc.HiddenBlocks()
	for _, b := range []*Block{p1, h2, p2, child} {
		if hidden[b.ID] == "" {
			t.Fatal("not hidden", b.Text())
		}
	}
	if hidden[h1b.ID] != "" || hidden[next.ID] != "" {
		t.Fatal("hid too much")
	}
	// Folds are saved, but undo neither folds nor unfolds.
	e.SetSelection(0, 0)
	e.Replace(0, 0, "!")
	e.ToggleFold(item.ID)
	e.Undo()
	if !e.Doc.Blocks[0].Folded || e.Doc.Blocks[5].Folded {
		t.Fatal("undo changed folds")
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := Decode(encoded); !again.Blocks[0].Folded {
		t.Fatal("fold not saved")
	}
	// Reveal opens what hides a block, as the caret moving in does.
	if !e.Reveal(p2.ID) || e.Doc.Blocks[0].Folded {
		t.Fatal("reveal")
	}
	// Turning a folded heading into a paragraph drops the fold.
	e.ToggleFold(h1.ID)
	e.SetSelection(1, 1)
	e.SetKind("paragraph", 0)
	if e.Doc.Blocks[0].Folded {
		t.Fatal("stray fold")
	}
}

func TestMergeSplitAndCellBackground(t *testing.T) {
	table := Table(3, 3)
	for r, row := range table.Children {
		for c, cell := range row.Children {
			cell.Runs = []Run{{Text: string(rune('a' + r*3 + c))}}
		}
	}
	e := blocks(table)
	id := table.ID
	e.MergeCells(id, CellRange{0, 0, 1, 1})
	tb := e.Doc.Blocks[0]
	anchor := tb.Children[0].Children[0]
	if anchor.ColSpan != 2 || anchor.RowSpan != 2 || anchor.Text() != "a\nb\nd\ne" {
		t.Fatal("merge", anchor.ColSpan, anchor.RowSpan, anchor.Text())
	}
	if e.Doc.PlainText() != "a\nb\nd\ne\nc\nf\ng\nh\ni" {
		t.Fatalf("covered cells left the text: %q", e.Doc.PlainText())
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	// A range crossing the merge grows over it.
	if got := Expand(tb, CellRange{1, 1, 2, 1}); got != (CellRange{0, 0, 2, 1}) {
		t.Fatal("expand", got)
	}
	// Inserting a column through the merge widens it; deleting its
	// anchor's row hands the merge down.
	e.TableInsert(id, false, 1)
	if anchor.ColSpan != 3 || e.Doc.Validate() != nil {
		t.Fatal("insert through", anchor.ColSpan)
	}
	e.TableDelete(id, true, 0)
	moved := tb.Children[0].Children[0]
	if moved.RowSpan != 0 || moved.ColSpan != 3 || moved.Text() != "a\nb\nd\ne" || e.Doc.Validate() != nil {
		t.Fatal("delete anchor row", moved.ColSpan, moved.RowSpan, moved.Text())
	}
	e.SplitCell(moved.ID)
	if moved.ColSpan != 0 || len(e.Doc.Leaves()) != 8 {
		t.Fatal("split", len(e.Doc.Leaves()))
	}
	e.SetCellBackground(id, CellRange{0, 0, 1, 0}, "light-blue")
	if tb.Children[1].Children[0].Background != "light-blue" || tb.Children[0].Children[1].Background != "" {
		t.Fatal("background")
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"format":"arcmd","version":4,"blocks":[{"id":"t","kind":"table","children":[{"id":"r","kind":"row","children":[{"id":"a","kind":"cell","colspan":2},{"id":"b","kind":"cell","runs":[{"text":"x"}]}]}]}]}`,
		`{"format":"arcmd","version":4,"blocks":[{"id":"t","kind":"table","children":[{"id":"r","kind":"row","children":[{"id":"a","kind":"cell","colspan":3},{"id":"b","kind":"cell"}]}]}]}`,
		`{"format":"arcmd","version":4,"blocks":[{"id":"a","kind":"paragraph","background":"red"}]}`,
		`{"format":"arcmd","version":4,"blocks":[{"id":"a","kind":"paragraph","folded":true}]}`,
		`{"format":"arcmd","version":3,"blocks":[{"id":"a","kind":"callout","emoji":"💡"}]}`,
	} {
		if _, err := Decode(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	callout := NewBlock("callout", "x")
	e = blocks(callout)
	e.SetEmoji(callout.ID, "💡")
	if e.Doc.Blocks[0].Emoji != "💡" {
		t.Fatal("emoji")
	}
	e.Undo()
	if e.Doc.Blocks[0].Emoji != "" {
		t.Fatal("undo emoji")
	}
}

func TestDecodeTurnsFileBlocksIntoLinks(t *testing.T) {
	source := `{"format":"arcmd","version":4,"blocks":[{"id":"a","kind":"file","runs":[{"text":"report.pdf"}],"url":"assets/report.pdf"},{"id":"b","kind":"file","url":"assets/x.zip"}]}`
	d, err := Decode(source)
	if err != nil {
		t.Fatal(err)
	}
	first, second := d.Blocks[0], d.Blocks[1]
	if first.Kind != "paragraph" || first.URL != "" || len(first.Runs) != 1 || first.Runs[0].Text != "report.pdf" || first.Runs[0].Style.Link != "assets/report.pdf" {
		t.Fatalf("%+v", first)
	}
	if second.Text() != "assets/x.zip" || second.Runs[0].Style.Link != "assets/x.zip" {
		t.Fatalf("%+v", second)
	}
	if _, err := Decode(mustEncode(t, d)); err != nil {
		t.Fatal(err)
	}
}

func mustEncode(t *testing.T, d *Document) string {
	t.Helper()
	text, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestInsertAt(t *testing.T) {
	kinds := func(e *Engine) []string {
		var out []string
		for _, b := range e.Doc.Blocks {
			out = append(out, b.Kind+":"+b.Text())
		}
		return out
	}
	// An empty document's paragraph gives way.
	e := NewEngine(New())
	e.InsertAt(0, NewBlock("image", "a"))
	if got := kinds(e); !reflect.DeepEqual(got, []string{"image:a"}) {
		t.Fatalf("empty document: %v", got)
	}
	e = textEngine("one")
	e.InsertAt(0, NewBlock("image", "a"), NewBlock("image", "b"))
	e.InsertAt(99, NewBlock("image", "c"))
	if got := kinds(e); !reflect.DeepEqual(got, []string{"image:a", "image:b", "paragraph:one", "image:c"}) {
		t.Fatalf("blocks = %v", got)
	}
	if leaf := e.LeafAt(e.Caret); leaf.Block.Text() != "c" {
		t.Errorf("caret on %q, want the last inserted", leaf.Block.Text())
	}
	e.Undo()
	e.Undo()
	if got := kinds(e); !reflect.DeepEqual(got, []string{"paragraph:one"}) {
		t.Errorf("undo: %v", got)
	}
	if err := e.Doc.Validate(); err != nil {
		t.Error(err)
	}
}

func TestPrependMarkdownPrefixPreservesExistingContent(t *testing.T) {
	prepend := func(e *Engine, prefix string) {
		for _, r := range prefix {
			e.Replace(e.Caret, e.Caret, string(r))
		}
	}
	roundTrip := func(t *testing.T, e *Engine) {
		t.Helper()
		encoded := mustEncode(t, e.Doc)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(e.Doc, decoded) {
			t.Fatalf("round-trip mismatch:\n got: %+v\nwant: %+v", decoded.Blocks, e.Doc.Blocks)
		}
	}

	// Heading "## " before styled text preserves inline runs and undoes/redoes cleanly.
	e := textEngine("我要变二级标题")
	e.SetSelection(3, 7)
	e.Toggle("bold")
	e.SetSelection(0, 0)
	prepend(e, "## ")
	b := e.Doc.Blocks[0]
	if b.Kind != "heading" || b.Level != 2 || b.Text() != "我要变二级标题" || e.Caret != 0 {
		t.Fatalf("prepend ## failed: kind=%s level=%d text=%q caret=%d", b.Kind, b.Level, b.Text(), e.Caret)
	}
	if len(b.Runs) != 2 || b.Runs[0].Text != "我要变" || !b.Runs[1].Style.Bold || b.Runs[1].Text != "二级标题" {
		t.Fatalf("prepend ## lost inline runs: %+v", b.Runs)
	}
	roundTrip(t, e)
	e.Undo()
	if e.Doc.Blocks[0].Kind != "paragraph" || e.Doc.Blocks[0].Text() != "##我要变二级标题" {
		t.Fatalf("undo heading autoformat: %+v", e.Doc.Blocks[0])
	}
	e.Redo()
	if e.Doc.Blocks[0].Kind != "heading" || e.Doc.Blocks[0].Level != 2 || e.Doc.Blocks[0].Text() != "我要变二级标题" {
		t.Fatalf("redo heading autoformat: %+v", e.Doc.Blocks[0])
	}

	// Quote with 1-3 CommonMark leading spaces.
	q := textEngine("我要变引用")
	q.SetSelection(0, 0)
	prepend(q, " > ")
	if q.Doc.Blocks[0].Kind != "quote" || q.Doc.Blocks[0].Text() != "我要变引用" {
		t.Fatalf("prepend leading-space > failed: %+v", q.Doc.Blocks[0])
	}
	roundTrip(t, q)

	// Bullet "- " then "[ ] " into unchecked task, and "- [x] " into checked task.
	task := textEngine("待办事项")
	task.SetSelection(0, 0)
	prepend(task, "- ")
	if task.Doc.Blocks[0].Kind != "bullet" || task.Doc.Blocks[0].Text() != "待办事项" {
		t.Fatalf("prepend - failed: %+v", task.Doc.Blocks[0])
	}
	prepend(task, "[ ] ")
	if task.Doc.Blocks[0].Kind != "task" || task.Doc.Blocks[0].Checked || task.Doc.Blocks[0].Text() != "待办事项" {
		t.Fatalf("prepend [ ] in bullet failed: %+v", task.Doc.Blocks[0])
	}
	roundTrip(t, task)

	doneTask := textEngine("已完成")
	doneTask.SetSelection(0, 0)
	prepend(doneTask, "- [x] ")
	if doneTask.Doc.Blocks[0].Kind != "task" || !doneTask.Doc.Blocks[0].Checked || doneTask.Doc.Blocks[0].Text() != "已完成" {
		t.Fatalf("prepend - [x] failed: %+v", doneTask.Doc.Blocks[0])
	}
	roundTrip(t, doneTask)

	// Ordered list with non-1 number "2. ".
	ord := textEngine("第二条")
	ord.SetSelection(0, 0)
	prepend(ord, "2. ")
	if ord.Doc.Blocks[0].Kind != "numbered" || ord.Doc.Blocks[0].Text() != "第二条" {
		t.Fatalf("prepend 2. failed: %+v", ord.Doc.Blocks[0])
	}
	roundTrip(t, ord)

	// Verbatim code "``` " and math "$$ " strip inline marks from trailing text.
	code := textEngine("fmt.Println(1)")
	code.SetSelection(0, 3)
	code.Toggle("bold")
	code.SetSelection(0, 0)
	prepend(code, "``` ")
	cb := code.Doc.Blocks[0]
	if cb.Kind != "code" || cb.Text() != "fmt.Println(1)" || len(cb.Runs) != 1 || cb.Runs[0].Style != (Style{}) {
		t.Fatalf("prepend ``` failed: %+v", cb)
	}
	roundTrip(t, code)

	math := textEngine("a_x=b_y")
	math.SetSelection(0, 3)
	math.Toggle("italic")
	math.SetSelection(0, 0)
	prepend(math, "$$ ")
	mb := math.Doc.Blocks[0]
	if mb.Kind != "math" || mb.Text() != "a_x=b_y" || len(mb.Runs) != 1 || mb.Runs[0].Style != (Style{}) {
		t.Fatalf("prepend $$ failed: %+v", mb)
	}
	roundTrip(t, math)

	// 4 leading spaces must NOT autoformat.
	indented := textEngine("保持正文")
	indented.SetSelection(0, 0)
	prepend(indented, "    ## ")
	if indented.Doc.Blocks[0].Kind != "paragraph" || indented.Doc.Blocks[0].Text() != "    ## 保持正文" {
		t.Fatalf("4 leading spaces should not autoformat: %+v", indented.Doc.Blocks[0])
	}
}
