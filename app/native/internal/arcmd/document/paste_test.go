package document

import (
	"reflect"
	"testing"
)

func TestPasteCaretAtInsertedEnd(t *testing.T) {
	paragraphs := New()
	paragraphs.Blocks = []*Block{NewBlock("paragraph", "第一段"), NewBlock("paragraph", "最后一段😀")}
	table := Table(2, 2)
	table.Children[1].Children[0].Runs = []Run{{Text: "最后一个单元格😀"}}
	table.Children[1].Children[0].ColSpan = 2
	columns := Columns()
	columns.Children[1].Children = []*Block{table}
	for _, tc := range []struct {
		name   string
		blocks []*Block
	}{
		{"paragraphs", paragraphs.Blocks},
		{"table", []*Block{table}},
		{"columns", []*Block{columns}},
		{"empty-last-paragraph", append(paragraphs.Clone().Blocks, NewBlock("paragraph", ""))},
	} {
		for _, destination := range []string{"empty", "selection", "column"} {
			t.Run(tc.name+"/"+destination, func(t *testing.T) {
				fragment := New()
				fragment.Blocks = tc.blocks
				original := fragment.Clone()
				e := textEngine("")
				prefix, suffix := "", ""
				if destination != "empty" {
					e = textEngine("BeforeREPLACEAfter")
					if destination == "column" {
						cols := Columns()
						cols.Children[0].Children = e.Doc.Blocks
						cols.Children[1].Children[0].Runs = []Run{{Text: "Other column"}}
						e.Doc.Blocks = []*Block{cols}
						suffix = "\nOther column"
					}
					e.SetSelection(13, 6)
					prefix, suffix = "Before\n", "\nAfter"+suffix
				}
				before := e.Capture()
				e.Paste(fragment)
				want := Len(prefix + fragment.PlainText())
				if e.Anchor != want || e.Caret != want {
					t.Fatalf("paste selection = %d,%d; want %d,%d", e.Anchor, e.Caret, want, want)
				}
				if got := e.Doc.PlainText(); got != prefix+fragment.PlainText()+suffix {
					t.Fatalf("paste changed surrounding text: %q", got)
				}
				if !reflect.DeepEqual(fragment, original) {
					t.Fatal("paste mutated the clipboard fragment")
				}
				after := e.Capture()
				encoded, err := e.Doc.Encode()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := Decode(encoded)
				if err != nil || !reflect.DeepEqual(loaded, e.Doc) {
					t.Fatalf("pasted model did not round trip: %v", err)
				}
				e.Undo()
				if !reflect.DeepEqual(e.Capture(), before) {
					t.Fatal("undo did not restore the document and selection")
				}
				e.Redo()
				if !reflect.DeepEqual(e.Capture(), after) {
					t.Fatal("redo did not restore the document and pasted caret")
				}
			})
		}
	}
}
