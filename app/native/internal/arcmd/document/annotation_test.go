package document

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnnotationEditUndoAndRoundTrip(t *testing.T) {
	e := NewEngine(&Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{NewBlock("paragraph", "hand drawn")}})
	e.SetSelection(0, 4)
	e.SetAnnotation("circle", "blue")
	runs := e.Doc.Blocks[0].Runs
	if len(runs) != 2 || runs[0].Text != "hand" || runs[0].Style.Annotation != "circle" || runs[0].Style.AnnotationColor != "blue" || runs[1].Style.Annotation != "" {
		t.Fatalf("runs = %+v", runs)
	}
	s, err := e.Doc.Encode()
	if err != nil || !strings.Contains(s, `"annotation": "circle"`) {
		t.Fatal(s, err)
	}
	decoded, err := Decode(s)
	if err != nil || !reflect.DeepEqual(decoded, e.Doc) {
		t.Fatal("round trip", err)
	}
	// An older version has no annotations: one that claims them is damaged.
	if _, err := Decode(strings.Replace(s, `"version": 8`, `"version": 6`, 1)); err == nil {
		t.Fatal("accepted annotations in v6")
	}
	// Taking the mark away clears its color and merges the runs again.
	e.SetSelection(0, 10)
	e.SetAnnotation("", "red")
	if runs := e.Doc.Blocks[0].Runs; len(runs) != 1 || runs[0].Style != (Style{}) {
		t.Fatalf("cleared runs = %+v", runs)
	}
	e.Undo()
	if e.Doc.Blocks[0].Runs[0].Style.Annotation != "circle" {
		t.Fatal("undo lost the annotation")
	}
	// A caret carries the mark into what is typed next.
	e.SetSelection(10, 10)
	e.SetAnnotation("wavy", "")
	e.Replace(10, 10, "!")
	if last := e.Doc.Blocks[0].Runs[len(e.Doc.Blocks[0].Runs)-1]; last.Text != "!" || last.Style.Annotation != "wavy" {
		t.Fatalf("typed run = %+v", last)
	}
	e.ReadOnly = true
	e.SetSelection(0, 10)
	e.SetAnnotation("box", "")
	if e.Doc.Blocks[0].Runs[0].Style.Annotation == "box" {
		t.Fatal("read-only document took an annotation")
	}
}

func TestValidateRejectsBadAnnotations(t *testing.T) {
	for _, style := range []string{
		`{"annotation":"scribble"}`,
		`{"annotationColor":"red"}`,
		`{"annotation":"wavy","annotationColor":"pink"}`,
	} {
		text := `{"format":"arcmd","version":7,"blocks":[{"id":"p","kind":"paragraph","runs":[{"text":"x","style":` + style + `}]}]}`
		if _, err := Decode(text); err == nil {
			t.Fatal("accepted", style)
		}
	}
}
