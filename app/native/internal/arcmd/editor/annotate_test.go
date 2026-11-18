package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func TestAnnotationPanelAppliesMarks(t *testing.T) {
	e, tt := newTester("Hand drawn notes")
	focus(e, tt)
	e.SetSelection(5, 10)
	tt.Frame()
	if err := tt.Click("选择标注"); err != nil {
		t.Fatal(err)
	}
	if !e.annotatePanel {
		t.Fatal("panel did not open")
	}
	if err := tt.Click("标注颜色：绿色"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("圈出"); err != nil {
		t.Fatal(err)
	}
	r := e.Doc.Blocks[0].Runs[1]
	if r.Text != "drawn" || r.Style.Annotation != "circle" || r.Style.AnnotationColor != "green" {
		t.Fatal("annotation", e.Doc.Blocks[0].Runs)
	}
	// The new mark draws itself, then stays drawn.
	if e.drawIn.end != 10 {
		t.Fatal("no draw-in", e.drawIn)
	}
	tt.Advance(annotationDraw)
	tt.Frame()
	tt.Frame()
	if e.drawIn.end != 0 {
		t.Fatal("draw-in never finished", e.drawIn)
	}
	// Picking the same mark again takes it away; undo brings it back.
	if err := tt.Click("圈出"); err != nil {
		t.Fatal(err)
	}
	if e.Doc.Blocks[0].Runs[0].Style.Annotation != "" || len(e.Doc.Blocks[0].Runs) != 1 {
		t.Fatal("toggle off", e.Doc.Blocks[0].Runs)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.Doc.Blocks[0].Runs[1].Style.Annotation != "circle" {
		t.Fatal("undo", e.Doc.Blocks[0].Runs)
	}
	tt.Key(0, ui.KeyEscape)
	if e.annotatePanel {
		t.Fatal("escape")
	}
	// The pen applies the last mark to another selection.
	e.SetSelection(0, 4)
	tt.Frame()
	if err := tt.Click("手绘标注"); err != nil {
		t.Fatal(err)
	}
	if s := e.Doc.Blocks[0].Runs[0].Style; s.Annotation != "circle" || s.AnnotationColor != "green" {
		t.Fatal("apply last", s)
	}
	// Markdown preserves hand-drawn annotations losslessly; the panel's remover clears them.
	md := ExportMarkdown(e.Doc)
	if !strings.Contains(md, `data-mark="circle"`) {
		t.Fatalf("missing annotation markup in %q", md)
	}
	round := ImportMarkdown(md)
	if round.PlainText() != "Hand drawn notes" ||
		round.Blocks[0].Runs[0].Style.Annotation != "circle" ||
		round.Blocks[0].Runs[0].Style.AnnotationColor != "green" ||
		round.Blocks[0].Runs[2].Style.Annotation != "circle" {
		t.Fatalf("roundtrip %q -> %+v", md, round.Blocks[0].Runs)
	}
	e.SetSelection(0, 16)
	tt.Frame()
	tt.Click("选择标注")
	if err := tt.Click("移除标注"); err != nil {
		t.Fatal(err)
	}
	for _, r := range e.Doc.Blocks[0].Runs {
		if r.Style.Annotation != "" {
			t.Fatal("remove", e.Doc.Blocks[0].Runs)
		}
	}
}

func TestAnnotationRunsJoinAcrossStyles(t *testing.T) {
	e := New("a **bold** b")
	e.SetSelection(0, 8)
	e.Annotate("box", "")
	runs := annotationRuns(e.Doc.Blocks[0].Runs)
	if len(runs) != 1 || runs[0].start != 0 || runs[0].end != 8 || runs[0].variant != "box" {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestFlattenSVGFollowsEveryLook(t *testing.T) {
	for _, look := range annotationLooks {
		for _, s := range look.strokes {
			subs := flattenSVG(s.d)
			if len(subs) == 0 || len(subs[0]) < 2 {
				t.Fatalf("%s: %q flattened to %v", look.name, s.d, subs)
			}
			for _, sub := range subs {
				for _, pt := range sub {
					if pt[0] < -1 || pt[1] < -1 || pt[0] > look.view[0]+1 || pt[1] > look.view[1]+1 {
						t.Fatalf("%s: point %v outside its view box", look.name, pt)
					}
				}
			}
		}
	}
	// Every variant the document allows has a look and a name.
	for _, name := range document.Annotations {
		if _, ok := lookOf(name); !ok || AnnotationNames[name] == "" {
			t.Fatal("missing", name)
		}
	}
}

// clickAt presses and lets go at a document position.
func clickAt(e *Editor, tt *ui.Tester, at int) {
	r, _, _ := e.BoundsForRange(ui.TextInputRange{Start: at, End: at})
	b, _ := tt.Find("文档")
	x, y := b.X+r.X+2, b.Y+r.Y+r.H/2
	// Apart from the last click, so it is not a double click.
	tt.Advance(time.Second)
	tt.Press(x, y)
	tt.Release(x, y)
	tt.Frame()
}

func TestClickOnMarkedTextOpensMarkBar(t *testing.T) {
	e, tt := newTester("Hand drawn notes")
	focus(e, tt)
	e.SetSelection(5, 10)
	e.Annotate("wavy", "")
	tt.Advance(time.Second)
	tt.Frame()
	clickAt(e, tt, 7)
	if !e.markPop.open || e.markPop.start != 5 || e.markPop.end != 10 {
		t.Fatal("mark bar", e.markPop)
	}
	// The click still put the caret in the text.
	if sel := e.Selection().Range; sel.Start != 7 || sel.End != 7 {
		t.Fatal("caret", sel)
	}
	if err := tt.Click("改为方框"); err != nil {
		t.Fatal(err)
	}
	if s := e.Doc.Blocks[0].Runs[1].Style; s.Annotation != "box" || e.Caret != 7 || !e.markPop.open {
		t.Fatal("restyle", s, e.Caret, e.markPop)
	}
	if err := tt.Click("标注颜色：红色"); err != nil {
		t.Fatal(err)
	}
	if s := e.Doc.Blocks[0].Runs[1].Style; s.AnnotationColor != "red" {
		t.Fatal("recolor", s)
	}
	tt.Key(0, ui.KeyEscape)
	if e.markPop.open {
		t.Fatal("escape")
	}
	clickAt(e, tt, 6)
	if err := tt.Click("移除标注"); err != nil {
		t.Fatal(err)
	}
	if len(e.Doc.Blocks[0].Runs) != 1 || e.markPop.open {
		t.Fatal("remove", e.Doc.Blocks[0].Runs, e.markPop)
	}
	// Plain text opens nothing; typing in marked text puts the bar away.
	clickAt(e, tt, 2)
	if e.markPop.open {
		t.Fatal("bar over plain text")
	}
	e.SetSelection(0, 4)
	e.Annotate("circle", "")
	clickAt(e, tt, 2)
	if !e.markPop.open {
		t.Fatal("no bar over circle", e.markPop, e.Selection().Range, e.Doc.Blocks[0].Runs)
	}
	tt.Type("x")
	tt.Frame()
	if e.markPop.open {
		t.Fatal("bar outlived an edit")
	}
}

func TestPaddedMarksOpenSpaceAroundText(t *testing.T) {
	runs := []document.Run{{Text: "ab"}, {Text: "cd", Style: document.Style{Annotation: "box"}}, {Text: "e"}}
	spans := padAnnotations([]ui.Span{{Text: "abcde"}}, runs, 10)
	var text string
	spaced := map[string]float32{}
	for _, s := range spans {
		text += s.Text
		if s.LetterSpacing != 0 {
			spaced[s.Text] = s.LetterSpacing
		}
	}
	// The character before the stretch and its last one take the box's
	// 0.4em each; the text itself is unchanged.
	if text != "abcde" || len(spaced) != 2 || spaced["b"] != 4 || spaced["d"] != 4 {
		t.Fatalf("spans = %+v", spans)
	}
	// A wavy line needs no room.
	runs[1].Style.Annotation = "wavy"
	if spans := padAnnotations([]ui.Span{{Text: "abcde"}}, runs, 10); len(spans) != 1 {
		t.Fatalf("wavy spans = %+v", spans)
	}
}

func TestMarksDrawThemselvesWhenADocumentOpens(t *testing.T) {
	doc := New("one two").Doc
	doc.Blocks[0].Runs = []document.Run{{Text: "one", Style: document.Style{Annotation: "circle"}}, {Text: " "}, {Text: "two", Style: document.Style{Annotation: "wavy"}}}
	for _, on := range []bool{false, true} {
		e := FromDocument(doc)
		tt := ui.NewTester(func(c *ui.Context) { View(c, e, Options{Width: 800, Theme: c.Theme(), DrawAnnotations: on}) }, 800, 600)
		tt.Frame()
		tt.Frame()
		if drawing := !e.openReveal.at.IsZero(); drawing != on {
			t.Fatalf("option %v: drawing %v", on, drawing)
		}
		if on && len(e.openReveal.order) != 2 || on && e.openReveal.order[4] != 1 {
			t.Fatal("order", e.openReveal.order)
		}
		tt.Advance(openStaggerMax + annotationDraw)
		tt.Frame()
		tt.Frame()
		if !e.openReveal.at.IsZero() {
			t.Fatal("never finished")
		}
	}
}

func TestReadOnlyTypingSaysWhy(t *testing.T) {
	e, tt := newTester("Locked")
	focus(e, tt)
	e.SetReadOnly(true)
	e.RequestFocus = true
	tt.Frame()
	tt.Type("x")
	tt.Frame()
	if e.Text() != "Locked" || e.refusedAt.IsZero() {
		t.Fatal("no notice", e.Text(), e.refusedAt)
	}
	tt.Advance(readOnlyHold + readOnlyFade)
	tt.Frame()
	tt.Frame()
	if !e.refusedAt.IsZero() {
		t.Fatal("notice stayed")
	}
	// Unlocked, typing works and says nothing.
	e.SetReadOnly(false)
	tt.Type("!")
	tt.Frame()
	if e.Text() != "Locked!" && e.Text() != "!Locked" || !e.refusedAt.IsZero() {
		t.Fatal("unlocked", e.Text(), e.refusedAt)
	}
}

func TestCornerCountsTheSelection(t *testing.T) {
	e := New("你好 world")
	tt := ui.NewTester(func(c *ui.Context) { e.Corner(c) }, 400, 40)
	tt.Frame()
	if !tt.HasText("3 字") {
		t.Fatal("total")
	}
	e.SetSelection(0, 2)
	tt.Frame()
	if !tt.HasText("已选 2 / 3 字") {
		t.Fatal("selection count")
	}
}
