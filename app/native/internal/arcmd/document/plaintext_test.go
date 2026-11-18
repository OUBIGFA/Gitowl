package document

import (
	"strings"
	"testing"
)

// PlainText walks the tree itself; it must say what the leaves say.
func TestPlainTextMatchesLeaves(t *testing.T) {
	table := Table(2, 2)
	table.Children[0].Children[0].Runs = []Run{{Text: "a"}, {Text: "b", Style: Style{Bold: true}}}
	table.Children[0].Children[0].ColSpan = 2
	table.Children[1].Children[1].Runs = []Run{{Text: "d"}}
	cols := Columns()
	cols.Children[1].Children[0].Runs = []Run{{Text: "right"}}
	d := &Document{Blocks: []*Block{NewBlock("heading", "Title"), table, cols, NewBlock("paragraph", "")}}
	var parts []string
	for _, l := range d.Leaves() {
		parts = append(parts, l.Block.Text())
	}
	if got, want := d.PlainText(), strings.Join(parts, "\n"); got != want {
		t.Fatalf("plain text %q, leaves %q", got, want)
	}
	if (&Document{}).PlainText() != "" {
		t.Fatal("empty document has text")
	}
}

func TestHasKind(t *testing.T) {
	cols := Columns()
	d := &Document{Blocks: []*Block{NewBlock("paragraph", "x"), cols}}
	if d.HasKind("footnote") || !d.HasKind("column") || !d.HasKind("paragraph") {
		t.Fatal("kinds before")
	}
	cols.Children[0].Children = append(cols.Children[0].Children, &Block{ID: NewID(), Kind: "footnote", NoteLabel: "1"})
	if !d.HasKind("footnote") {
		t.Fatal("nested footnote not found")
	}
}
