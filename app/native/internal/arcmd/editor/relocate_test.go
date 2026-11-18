package editor

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestRelocateImagesDuringComposition(t *testing.T) {
	e, tt := newTester("# Test\n\n![picture](assets/old.png)\n\nText")
	focus(e, tt)
	e.Engine.SetCover("assets/old.png")
	end := e.Len()
	e.SetSelection(end, end)
	tt.Frame()
	tt.Compose("输入", 2)
	e.RelocateImages(map[string]string{"assets/old.png": "assets/new.png"})
	if e.Committed().Cover != "assets/new.png" {
		t.Fatal("committed composition retained old cover")
	}
	if _, ok := e.MarkedRange(); !ok {
		t.Fatal("move committed composition unexpectedly")
	}
	tt.Type("输入")
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Cover != "assets/new.png" {
		t.Fatal("composition undo restored old cover")
	}
}
