package editor

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestToggleMarkdownRoundTrip(t *testing.T) {
	doc := ImportMarkdown("# T\n\n<details>\n<summary>更多 &amp; 细节</summary>\n\n里面的话\n\n- 一项\n\n</details>\n\n后面")
	var kinds []string
	for _, b := range doc.Blocks {
		kinds = append(kinds, b.Kind)
	}
	if strings.Join(kinds, ",") != "heading,toggle,paragraph,bullet,paragraph" {
		t.Fatal(kinds)
	}
	toggle := doc.Blocks[1]
	if toggle.Text() != "更多 & 细节" || !toggle.Folded || doc.Blocks[2].Indent != 1 || doc.Blocks[3].Indent != 1 || doc.Blocks[4].Indent != 0 {
		t.Fatal("toggle content", toggle.Text(), toggle.Folded, doc.Blocks[2].Indent)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	md := ExportMarkdown(doc)
	if !strings.Contains(md, "<details>\n<summary>更多 &amp; 细节</summary>\n\n里面的话\n\n- 一项\n\n</details>\n\n后面") {
		t.Fatal(md)
	}
	again := ImportMarkdown(md)
	if len(again.Blocks) != len(doc.Blocks) || again.Blocks[1].Kind != "toggle" || again.Blocks[3].Indent != 1 {
		t.Fatal("round trip", ExportMarkdown(again))
	}
}

func TestSlashToggleAndClickFolds(t *testing.T) {
	e, tt := newTester("A")
	focus(e, tt)
	e.SetSelection(1, 1)
	tt.Key(0, ui.KeyEnter)
	tt.Type("/折叠")
	if !e.slashOpen {
		t.Fatal("slash menu")
	}
	tt.Key(0, ui.KeyEnter)
	if e.Doc.Blocks[1].Kind != "toggle" || len(e.Doc.Blocks) != 3 || e.Doc.Blocks[2].Indent != 1 {
		t.Fatal("toggle with a line of content", blockTexts(e), len(e.Doc.Blocks))
	}
	tt.Type("标题")
	tt.Key(0, ui.KeyEnter)
	tt.Type("内容")
	if e.Doc.Blocks[2].Text() != "内容" || e.Doc.Blocks[2].Indent != 1 {
		t.Fatal("writing inside", blockTexts(e))
	}
	tt.Frame()
	bounds, _ := tt.Find("文档")
	var f foldButton
	for _, b := range e.foldButtons {
		if b.id == e.Doc.Blocks[1].ID {
			f = b
		}
	}
	if !f.always {
		t.Fatal("a toggle's triangle always shows")
	}
	tt.ClickAt(bounds.X+f.rect.X+8, bounds.Y+f.rect.Y+8)
	if !e.Doc.Blocks[1].Folded || e.Doc.HiddenBlocks()[e.Doc.Blocks[2].ID] == "" {
		t.Fatal("clicking the triangle folds it")
	}
	tt.ClickAt(bounds.X+f.rect.X+8, bounds.Y+f.rect.Y+8)
	if e.Doc.Blocks[1].Folded {
		t.Fatal("clicking again opens it")
	}
}

func TestTabSeparatedPasteMakesTable(t *testing.T) {
	e, tt := newTester("")
	focus(e, tt)
	tt.SetClipboard("名称\t数量\n苹果\t3\n香蕉\t12\n")
	tt.Key(ui.Cmd, ui.KeyV)
	var table *document.Block
	for _, b := range e.Doc.Blocks {
		if b.Kind == "table" {
			table = b
		}
	}
	if table == nil || len(table.Children) != 3 || table.Children[2].Children[1].Text() != "12" {
		t.Fatal("tab-separated rows paste as a table", blockTexts(e))
	}
	// Prose with commas stays prose, unless asked.
	e2, tt2 := newTester("")
	focus(e2, tt2)
	tt2.SetClipboard("Hello, world\nGood, night")
	tt2.Key(ui.Cmd, ui.KeyV)
	if e2.Doc.Blocks[0].Kind == "table" {
		t.Fatal("commas alone are not a table")
	}
	tt2.SetClipboard("a,b\n1,2")
	if !e2.PasteAsTable() {
		t.Fatal("paste as table")
	}
	found := false
	for _, b := range e2.Doc.Blocks {
		found = found || b.Kind == "table"
	}
	if !found {
		t.Fatal("no table from CSV")
	}
}

func TestColumnToolbarSorts(t *testing.T) {
	e, tt := newTester("| 名称 | 数量 |\n|---|---|\n| b | 2 |\n| a | 10 |\n| c | 1 |")
	focus(e, tt)
	id := e.Doc.Blocks[0].ID
	e.SelectTableLine(id, false, 1)
	tt.Frame()
	if err := tt.Click("按此列降序排序"); err != nil {
		t.Fatal(err)
	}
	rows := e.Doc.Blocks[0].Children
	if rows[1].Children[1].Text() != "10" || rows[3].Children[1].Text() != "1" || rows[0].Children[0].Text() != "名称" {
		t.Fatal("sorted by the column, header kept")
	}
}
