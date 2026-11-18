package editor

import (
	"strings"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func TestIndexedBufferPositions(t *testing.T) {
	d := document.New()
	d.Blocks = []*document.Block{document.NewBlock("paragraph", "前😀"), document.Table(2, 2), document.NewBlock("code", "e\u0301\r\nnext👨‍👩‍👧‍👦"), document.NewBlock("paragraph", "")}
	b := newBuffer(d)
	check := func() {
		t.Helper()
		text := b.Doc.PlainText()
		for at := -2; at <= ui.UTF16Len(text)+2; at++ {
			want := ui.UTF16ByteOffset(text, at)
			if got := b.byteOffset(at); got != want {
				t.Fatalf("UTF-16 offset %d: byte %d, want %d", at, got, want)
			}
			if got := b.clamp(at); got != ui.UTF16Len(text[:want]) {
				t.Fatalf("clamp %d: %d", at, got)
			}
		}
		for at := 0; at <= len(text); at++ {
			if got := b.textOffset(at); got != ui.UTF16Len(text[:at]) {
				t.Fatalf("byte offset %d: UTF-16 %d, want %d", at, got, ui.UTF16Len(text[:at]))
			}
		}
		if got := b.slice(0, document.Len(text)); got != text {
			t.Fatal("slice used stale text")
		}
	}
	check()
	// Mutate through the engine so the buffer must synchronize on access.
	b.Engine.Replace(1, 1, "世界")
	check()
	b.Engine.Insert(document.NewBlock("paragraph", "new😀"))
	check()
	b.Undo()
	check()
	b.Redo()
	check()
}

func TestLongTailGraphemesAndComposition(t *testing.T) {
	for _, glyph := range []string{"😀", "e\u0301", "👨‍👩‍👧‍👦", "🇨🇳"} {
		t.Run(glyph, func(t *testing.T) {
			e, tt := newTester(strings.Repeat("前文😀\n\n", 128) + "尾" + glyph)
			focus(e, tt)
			original := e.Text()
			e.SetSelection(e.Len(), e.Len())
			tt.Frame()
			tt.Key(0, ui.KeyBackspace)
			if e.Text() != strings.TrimSuffix(original, glyph) {
				t.Fatal("tail deletion split a grapheme or changed the prefix")
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			if e.Text() != original {
				t.Fatal("tail undo did not restore text")
			}
			tt.Compose("候选😀", 4)
			if e.Text() != original {
				t.Fatal("composition committed early")
			}
			tt.Type("中文😀")
			if e.Text() != original+"中文😀" {
				t.Fatal("composition committed at the wrong offset")
			}
			tt.Key(ui.Cmd, ui.KeyZ)
			if e.Text() != original {
				t.Fatal("composition did not undo as one transaction")
			}
		})
	}
}
