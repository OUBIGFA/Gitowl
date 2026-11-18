package editor

import (
	"fmt"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func TestColumnsInsertionKeepsTypingFocus(t *testing.T) {
	for _, entry := range []string{"starter", "slash-keyboard", "slash-pointer"} {
		t.Run(entry, func(t *testing.T) {
			e, tt := newTester("# Title\n\n")
			switch entry {
			case "starter":
				if err := tt.Click("插入分栏"); err != nil {
					t.Fatal(err)
				}
			default:
				focus(e, tt)
				tt.Key(ui.Cmd, ui.KeyEnter)
				tt.Type("/columns")
				if entry == "slash-pointer" {
					if err := tt.Click("分栏"); err != nil {
						t.Fatal(err)
					}
				} else {
					tt.Key(0, ui.KeyEnter)
				}
			}
			// No click in the page and no explicit focus request after insertion.
			tt.Type("左栏内容")
			cols := e.Doc.Blocks[len(e.Doc.Blocks)-1]
			if cols.Kind != "columns" || cols.Children[0].Children[0].Text() != "左栏内容" {
				t.Fatalf("typing after %s did not reach the first column: %s", entry, e.Text())
			}
		})
	}
}

func TestColumnsKeyboardNavigationAndEmptyArea(t *testing.T) {
	for _, width := range []int{900, 440} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			e, tt := newTester("")
			tt.SetSize(width, 650)
			if err := tt.Click("插入分栏"); err != nil {
				t.Fatal(err)
			}
			cols := e.Doc.Blocks[0]
			var areas []ui.Rect
			for _, d := range e.decorations {
				if d.kind == "column" {
					areas = append(areas, d.rect)
				}
			}
			if len(areas) != 2 || areas[0].H < 60 || areas[1].H < 60 {
				t.Fatalf("empty columns have no usable writing areas: %v", areas)
			}
			if width == 900 && areas[0].Y != areas[1].Y || width == 440 && areas[1].Y < areas[0].Y+areas[0].H {
				t.Fatalf("unexpected column placement at width %d: %v", width, areas)
			}
			// Click the empty second column well below its text line.
			r := areas[1]
			tt.ClickAt(r.X+r.W/2, r.Y+r.H-8)
			tt.Type("右栏")
			if cols.Children[1].Children[0].Text() != "右栏" {
				t.Fatal("empty column did not accept a click across its writing area")
			}
			before := e.Text()
			tt.Key(ui.Shift, ui.KeyTab)
			if e.Engine.LeafAt(e.Caret).Block.ID != cols.Children[0].Children[0].ID || e.Text() != before {
				t.Fatal("Shift+Tab did not switch columns without modifying text")
			}
			tt.Type("左栏")
			tt.Key(0, ui.KeyTab)
			tt.Type("第二栏：")
			if cols.Children[0].Children[0].Text() != "左栏" || cols.Children[1].Children[0].Text() != "第二栏：右栏" {
				t.Fatal("Tab did not move typing to the second column", e.Text())
			}
			tt.Key(0, ui.KeyTab)
			if e.Engine.LeafAt(e.Caret).Block.ID != cols.Children[0].Children[0].ID {
				t.Fatal("Tab did not cycle back to the first column")
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			if e.Doc.Blocks[0].Children[1].Children[0].Text() != "右栏" {
				t.Fatal("column navigation added an undo step")
			}
			tt.Key(ui.Cmd|ui.Shift, ui.KeyZ)
			encoded, err := e.Doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := document.Decode(encoded)
			if err != nil || decoded.PlainText() != e.Text() {
				t.Fatal("column input did not survive saving", err)
			}
		})
	}
}

func TestColumnsPreserveCodeAndSelectionTab(t *testing.T) {
	for _, kind := range []string{"code", "bullet", "paragraph"} {
		t.Run(kind, func(t *testing.T) {
			e, tt := newTester("")
			if err := tt.Click("插入分栏"); err != nil {
				t.Fatal(err)
			}
			e.Engine.SetKind(kind, 0)
			tt.Type("text")
			first := e.Doc.Leaves()[0]
			if kind == "paragraph" {
				e.SetSelection(first.Start, first.End)
				tt.Frame()
			}
			tt.Key(0, ui.KeyTab)
			if e.Engine.LeafAt(e.Caret).Block.ID != first.Block.ID {
				t.Fatalf("Tab in %s unexpectedly switched columns", kind)
			}
		})
	}
}
