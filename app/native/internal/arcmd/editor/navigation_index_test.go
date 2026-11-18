package editor

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

// These references use the previous flattened-string behavior to check chunk
// boundaries, including positions between a CR and LF and surrogate halves.
func referenceLineRange(text string, at int) (int, int) {
	i := ui.UTF16ByteOffset(text, at)
	a := strings.LastIndexByte(text[:i], '\n') + 1
	z := len(text)
	if n := strings.IndexByte(text[i:], '\n'); n >= 0 {
		z = i + n
	}
	if z > a && text[z-1] == '\r' {
		z--
	}
	return ui.UTF16Len(text[:a]), ui.UTF16Len(text[:z])
}
func referenceWordBoundary(text string, at, direction int) int {
	pos := ui.UTF16ByteOffset(text, at)
	if direction < 0 {
		for pos > 0 {
			r, n := utf8.DecodeLastRuneInString(text[:pos])
			if !unicode.IsSpace(r) {
				break
			}
			pos -= n
		}
		for pos > 0 {
			r, n := utf8.DecodeLastRuneInString(text[:pos])
			if unicode.IsSpace(r) {
				break
			}
			pos -= n
		}
	} else {
		for pos < len(text) {
			r, n := utf8.DecodeRuneInString(text[pos:])
			if unicode.IsSpace(r) {
				break
			}
			pos += n
		}
		for pos < len(text) {
			r, n := utf8.DecodeRuneInString(text[pos:])
			if !unicode.IsSpace(r) {
				break
			}
			pos += n
		}
	}
	return ui.UTF16Len(text[:pos])
}
func referenceSelectWord(text string, at int) (int, int) {
	a, z := referenceLineRange(text, at)
	start, end := ui.UTF16ByteOffset(text, at), ui.UTF16ByteOffset(text, at)
	left, right := ui.UTF16ByteOffset(text, a), ui.UTF16ByteOffset(text, z)
	for start > left {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		start -= n
	}
	for end < right {
		r, n := utf8.DecodeRuneInString(text[end:])
		if strings.ContainsRune(" \t\r\n", r) {
			break
		}
		end += n
	}
	return ui.UTF16Len(text[:start]), ui.UTF16Len(text[:end])
}
func TestChunkedNavigationMatchesFlattenedText(t *testing.T) {
	d := document.New()
	d.Blocks = []*document.Block{document.NewBlock("paragraph", "前😀 e\u0301\t word\u2003next"), document.NewBlock("paragraph", ""), document.Table(2, 2), document.NewBlock("code", "first\r\n👨‍👩‍👧‍👦\n\r\nend\r"), document.NewBlock("paragraph", "  \u00a0  "), document.NewBlock("paragraph", "last😀")}
	e := FromDocument(d)
	check := func() {
		t.Helper()
		text := e.Doc.PlainText()
		for at := -2; at <= ui.UTF16Len(text)+2; at++ {
			a, z := referenceLineRange(text, at)
			if x, y := e.lineRange(at); x != a || y != z {
				t.Fatalf("line at %d: %d:%d, want %d:%d", at, x, y, a, z)
			}
			for _, direction := range []int{-1, 1} {
				if got, want := e.wordBoundary(at, direction), referenceWordBoundary(text, at, direction); got != want {
					t.Fatalf("word at %d direction %d: %d, want %d", at, direction, got, want)
				}
			}
			a, z = referenceSelectWord(text, at)
			e.selectWord(at)
			if e.Anchor != a || e.Caret != z {
				t.Fatalf("select at %d: %d:%d, want %d:%d", at, e.Anchor, e.Caret, a, z)
			}
		}
	}
	check()
	e.Engine.Replace(1, 1, "new😀 ")
	check()
	e.Undo()
	check()
	e.Redo()
	check()
}

func TestVirtualOutlineDragAcrossOffscreenRows(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint("cancel=", cancel), func(t *testing.T) {
			var md strings.Builder
			md.WriteString("# Title\n\n")
			for i := 1; i <= 300; i++ {
				fmt.Fprintf(&md, "## Heading %d\n\nbody %d\n\n", i, i)
			}
			e := New(md.String())
			tt := ui.NewTester(func(c *ui.Context) {
				w, _ := c.Size()
				View(c, e, Options{Width: w, Theme: c.Theme(), Outline: true, SetOutline: func(bool) {}})
			}, 1400, 650)
			tt.Frame()
			before := e.Doc.Clone()
			from, ok := tt.Find("跳到 Heading 1")
			if !ok {
				t.Fatal("missing source")
			}
			x, y := from.X+from.W/2, from.Y+from.H/2
			source := e.outline[1].id
			tt.Press(x, y)
			tt.Move(x, y+12)
			if e.outlineState.dragging != source {
				t.Fatal("drag did not start")
			}
			tt.Scroll(x, 400, 0, 7000)
			tt.Frame()
			if e.outlineState.dragging != source {
				t.Fatal("offscreen drag source was removed")
			}
			if tt.HasText("跳到 Heading 100") {
				t.Fatal("drag materialized unrelated offscreen rows")
			}
			var target string
			for i := 270; i > 230; i-- {
				name := fmt.Sprintf("跳到 Heading %d", i)
				if r, ok := tt.Find(name); ok && r.Y > 120 && r.Y+r.H < 600 {
					target = name
					x, y = r.X+r.W/2, r.Y+r.H/2
					break
				}
			}
			if target == "" {
				t.Fatal("missing drop target")
			}
			targetID := ""
			for _, item := range e.outline {
				if "跳到 "+item.text == target {
					targetID = item.id
				}
			}
			if cancel {
				tt.Key(0, ui.KeyEscape)
			}
			tt.Move(x, y)
			tt.Release(x, y)
			tt.Frame()
			if e.outlineState.dragging != "" {
				t.Fatal("drag did not end")
			}
			if cancel {
				if !reflect.DeepEqual(e.Doc, before) {
					t.Fatal("cancel moved section")
				}
				return
			}
			moved, at := -1, -1
			for i, b := range e.Doc.Blocks {
				if b.ID == source {
					moved = i
				}
				if b.ID == targetID {
					at = i
				}
			}
			if moved < 0 || at != moved+2 || e.Doc.Blocks[moved+1].Text() != "body 1" {
				t.Fatalf("section drop lost source/body: %d %d", moved, at)
			}
			e.Undo()
			tt.Frame()
			if !reflect.DeepEqual(e.Doc, before) {
				t.Fatal("undo did not restore section order")
			}
		})
	}
}

func TestSmallFontFootnoteReturnHitArea(t *testing.T) {
	e := New("Text[^proof].\n\n[^proof]: A note.")
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), FontSize: 12, LineHeight: 1.2})
	}, 900, 650)
	tt.Frame()
	e.SetSelection(4, 4)
	if !e.FollowFootnote() {
		t.Fatal("missing footnote")
	}
	tt.Frame()
	var note *row
	for i := range e.rows {
		if e.rows[i].leaf.Block.Kind == "footnote" {
			note = &e.rows[i]
			break
		}
	}
	if note == nil {
		t.Fatal("missing note row")
	}
	surface, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing surface")
	}
	// The existing return target is always 26 DIP high, even when the font
	// and line height make the row shorter than that target.
	tt.ClickAt(surface.X+note.rect.X+4, surface.Y+note.y+25-e.Scroll.Y)
	if e.Caret != 4 {
		t.Fatalf("return target changed with small font: caret %d", e.Caret)
	}
}
