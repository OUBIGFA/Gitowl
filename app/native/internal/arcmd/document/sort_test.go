package document

import (
	"strings"
	"testing"
)

func column(t *Block, col int) string {
	var out []string
	for _, r := range t.Children {
		out = append(out, r.Children[col].Text())
	}
	return strings.Join(out, ",")
}

func TestSortTable(t *testing.T) {
	table := DelimitedTable([][]string{{"名称", "数量"}, {"file 10", "3"}, {"File 2", "12"}, {"", "1,000"}, {"apple", ""}})
	d := New()
	d.Blocks = []*Block{table}
	e := NewEngine(d)
	e.SortTable(table.ID, 0, false)
	if got := column(e.Doc.Blocks[0], 0); got != "名称,apple,File 2,file 10," {
		t.Fatal(got)
	}
	e.SortTable(table.ID, 1, true)
	if got := column(e.Doc.Blocks[0], 1); got != "数量,1,000,12,3," {
		t.Fatal("numbers by value, empty last", got)
	}
	e.Undo()
	if got := column(e.Doc.Blocks[0], 0); got != "名称,apple,File 2,file 10," {
		t.Fatal("undo", got)
	}
	e.Doc.Blocks[0].Children[1].Children[0].ColSpan = 2
	e.Doc.Blocks[0].Children[1].Children[1].Runs = nil
	if e.Doc.CanSortTable(table.ID) {
		t.Fatal("merged cells keep their rows")
	}
}

func TestDelimitedTablePadsRaggedRows(t *testing.T) {
	table := DelimitedTable([][]string{{"a", "b", "c"}, {"1"}})
	if len(table.Children[1].Children) != 3 || table.Children[1].Children[0].Text() != "1" {
		t.Fatal("ragged")
	}
	d := New()
	d.Blocks = []*Block{table}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}
