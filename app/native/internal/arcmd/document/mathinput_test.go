package document

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestMathFencesAreTransactions(t *testing.T) {
	e := textEngine("$$")
	e.SetSelection(2, 2)
	before := e.Doc.Clone()
	e.Enter(false)
	if e.Doc.Blocks[0].Kind != "math" || e.Doc.PlainText() != "" || e.Caret != 0 {
		t.Fatal("opening fence did not start math")
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("opening fence undo lost text")
	}
	e.Redo()
	e.Replace(0, 0, "x^2\n$$")
	before = e.Doc.Clone()
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "x^2" || e.LeafAt(e.Caret).Block.Kind != "paragraph" {
		t.Fatal("closing fence did not return to text")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("closing fence was not one undo step")
	}
}

func TestMathFencesDoNotConvertLiteralOrSelectedText(t *testing.T) {
	for _, kind := range []string{"code", "raw", "cell", "heading"} {
		e := textEngine("$$")
		e.Doc.Blocks[0].Kind = kind
		e.SetSelection(2, 2)
		e.Enter(false)
		if e.Doc.Blocks[0].Kind != kind {
			t.Fatalf("converted %s to math", kind)
		}
	}
	e := textEngine("$$")
	e.SetSelection(0, 2)
	e.Enter(false)
	if e.Doc.Blocks[0].Kind == "math" {
		t.Fatal("replacing selection started math")
	}
}

func TestPasteMathPreservesItsBlockAndSurroundingText(t *testing.T) {
	e := textEngine("BeforeAfter")
	e.SetSelection(6, 6)
	before := e.Doc.Clone()
	fragment := New()
	fragment.Blocks = []*Block{NewBlock("math", `\int_0^\infty x^2 e^{-x} dx = 2`)}
	e.Paste(fragment)
	blocks := e.Doc.Blocks
	if len(blocks) != 3 || blocks[0].Text() != "Before" || blocks[1].Kind != "math" || blocks[1].ID == fragment.Blocks[0].ID || blocks[2].Text() != "After" {
		t.Fatal("formula paste lost structure or surrounding text")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("formula paste was not a single undo step")
	}
}

func TestMathDoubleEnterExitAndPipeTableCompile(t *testing.T) {
	e := textEngine("$$")
	e.SetSelection(2, 2)
	e.Enter(false)
	e.Replace(0, 0, "F = ma")
	e.Enter(false)
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "math" || e.Doc.Blocks[0].Text() != "F = ma\n" {
		t.Fatalf("first Enter inside math exited prematurely: %+v", e.Doc.Blocks)
	}
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || e.Doc.Blocks[0].Text() != "F = ma" || e.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("second Enter on blank trailing line did not exit math: %+v", e.Doc.Blocks)
	}
	e.Undo()
	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Text() != "F = ma\n" {
		t.Fatal("double-Enter exit was not one undo step")
	}

	p := textEngine("| Port | Low | High")
	p.SetSelection(Len("| Port | Low | High"), Len("| Port | Low | High"))
	before := p.Doc.Clone()
	p.Enter(false)
	if len(p.Doc.Blocks) != 1 || p.Doc.Blocks[0].Kind != "table" || len(p.Doc.Blocks[0].Children) != 2 || len(p.Doc.Blocks[0].Children[0].Children) != 3 {
		t.Fatalf("pipe header did not compile into 2x3 table: %+v", p.Doc.Blocks)
	}
	tr := p.Doc.Blocks[0].Children[0]
	if tr.Children[0].Text() != "Port" || tr.Children[1].Text() != "Low" || tr.Children[2].Text() != "High" {
		t.Fatalf("unexpected header cells: %q, %q, %q", tr.Children[0].Text(), tr.Children[1].Text(), tr.Children[2].Text())
	}
	if p.LeafAt(p.Caret).Block.ID != p.Doc.Blocks[0].Children[1].Children[0].ID {
		t.Fatal("caret did not land in first body cell")
	}
	p.Undo()
	if !reflect.DeepEqual(before, p.Doc) {
		t.Fatal("pipe table compile did not undo in one step")
	}

	tbl := p.Doc.Blocks[0]
	_ = tbl
	p.Redo()
	id := p.Doc.Blocks[0].ID
	p.Replace(p.Caret, p.Caret, "SSH")
	p.TableInsert(id, true, 2)
	p.Replace(p.Caret, p.Caret, "HTTPS")
	beforeMove := p.Doc.Clone()
	p.MoveTableRow(id, 2, 1)
	if p.Doc.Blocks[0].Children[1].Children[0].Text() != "HTTPS" || p.Doc.Blocks[0].Children[2].Children[0].Text() != "SSH" {
		t.Fatal("MoveTableRow did not reorder rows")
	}
	p.Undo()
	if !reflect.DeepEqual(beforeMove, p.Doc) {
		t.Fatal("MoveTableRow did not undo cleanly")
	}
}

func TestDisplayMathAutoformatSingleLineAndInvariants(t *testing.T) {
	// "$$" + " " autoformats an empty paragraph into a math block cleanly.
	e := textEngine("")
	before := e.Doc.Clone()
	e.Replace(0, 0, "$$")
	e.Replace(2, 2, " ")
	if e.Doc.Blocks[0].Kind != "math" || e.Doc.PlainText() != "" || e.Caret != 0 {
		t.Fatalf("autoformat $$ + space did not open math: %+v caret=%d", e.Doc.Blocks[0], e.Caret)
	}
	encoded, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.Doc, decoded) {
		t.Fatal("round-trip changed math block")
	}
	e.Undo()
	if e.Doc.Blocks[0].Kind != "paragraph" || e.Doc.PlainText() != "$$" {
		t.Fatalf("undo of space autoformat: %+v", e.Doc.Blocks[0])
	}
	e.Redo()
	if e.Doc.Blocks[0].Kind != "math" {
		t.Fatal("redo lost math block")
	}
	e.Undo()
	e.Undo()
	if !reflect.DeepEqual(before, e.Doc) {
		t.Fatal("undo did not restore empty paragraph")
	}

	// Single-line "$$ a_x=b_y $$" compiles on Enter and leaves a paragraph below.
	s := textEngine("$$ a_x=b_y $$")
	s.SetSelection(Len("$$ a_x=b_y $$"), Len("$$ a_x=b_y $$"))
	beforeSingle := s.Doc.Clone()
	s.Enter(false)
	if len(s.Doc.Blocks) != 2 || s.Doc.Blocks[0].Kind != "math" || s.Doc.Blocks[0].Text() != "a_x=b_y" || s.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("single-line $$...$$ did not compile: %+v", s.Doc.Blocks)
	}
	s.Undo()
	if !reflect.DeepEqual(beforeSingle, s.Doc) {
		t.Fatal("single-line math compile did not undo in one step")
	}

	// Code-styled runs, headings, and table cells must ignore single-line math.
	codeRun := textEngine("$$ a_x=b_y $$")
	codeRun.Doc.Blocks[0].Runs[0].Style.Code = true
	codeRun.SetSelection(Len("$$ a_x=b_y $$"), Len("$$ a_x=b_y $$"))
	codeRun.Enter(false)
	if codeRun.Doc.Blocks[0].Kind == "math" {
		t.Fatal("code-styled run compiled as math")
	}
	heading := textEngine("$$ a_x=b_y $$")
	heading.Doc.Blocks[0].Kind = "heading"
	heading.SetSelection(Len("$$ a_x=b_y $$"), Len("$$ a_x=b_y $$"))
	heading.Enter(false)
	if heading.Doc.Blocks[0].Kind != "heading" {
		t.Fatal("heading compiled as math")
	}
	cellEng := NewEngine(New())
	cellEng.Insert(Table(1, 1))
	cell := cellEng.Doc.Leaves()[0]
	cellEng.Replace(cell.Start, cell.Start, "$$ a_x=b_y $$")
	cellEng.Enter(false)
	if cellEng.LeafAt(cellEng.Caret).Block.Kind != "cell" {
		t.Fatal("table cell left cell on single-line $$...$$")
	}

	// Inline trailing "$$" inside a math block exits and strips "$$"; escaped "\\$$" stays.
	m := textEngine("$$")
	m.SetSelection(2, 2)
	m.Enter(false)
	m.Replace(0, 0, "a_x=b_y$$")
	m.Enter(false)
	if len(m.Doc.Blocks) != 2 || m.Doc.Blocks[0].Kind != "math" || m.Doc.Blocks[0].Text() != "a_x=b_y" || m.Doc.Blocks[1].Kind != "paragraph" {
		t.Fatalf("inline $$ exit inside math failed: %+v", m.Doc.Blocks)
	}
	esc := textEngine("$$")
	esc.SetSelection(2, 2)
	esc.Enter(false)
	esc.Replace(0, 0, `a_x=b_y\$$`)
	esc.Enter(false)
	if len(esc.Doc.Blocks) != 1 || esc.Doc.Blocks[0].Kind != "math" || esc.Doc.Blocks[0].Text() != "a_x=b_y\\$$\n" {
		t.Fatalf("escaped \\$$ should stay in math block: %+v", esc.Doc.Blocks)
	}

	// Seeded random operations mixing math fences, enters, joins, undo, and redo.
	rng := rand.New(rand.NewSource(20261116))
	randEng := textEngine("")
	inputs := []string{"$", "$$", "$$ ", "$$ x_1 = y_2 $$", "x_1"}
	for step := range 300 {
		n := randEng.TextLength()
		a := rng.Intn(n + 1)
		randEng.SetSelection(a, a)
		switch rng.Intn(6) {
		case 0, 1:
			s := inputs[rng.Intn(len(inputs))]
			if s == "$$ " {
				randEng.Replace(a, a, "$$")
				randEng.Replace(randEng.Caret, randEng.Caret, " ")
			} else {
				randEng.Replace(a, a, s)
			}
		case 2:
			randEng.Enter(false)
		case 3:
			if !randEng.Join(true) && randEng.Caret > 0 {
				randEng.Replace(randEng.Caret-1, randEng.Caret, "")
			}
		case 4:
			randEng.Undo()
		case 5:
			randEng.Redo()
		}
		if err := randEng.Doc.Validate(); err != nil {
			t.Fatalf("invalid document at random step %d: %v", step, err)
		}
	}
}
