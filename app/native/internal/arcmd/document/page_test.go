package document

import (
	"slices"
	"strings"
	"testing"
)

func headings(d *Document) []string {
	var out []string
	for _, b := range d.Blocks {
		out = append(out, b.Text())
	}
	return out
}

func sectionDoc() *Document {
	d := New()
	d.Blocks = nil
	add := func(kind string, level int, text string) {
		b := NewBlock(kind, text)
		b.Level = level
		d.Blocks = append(d.Blocks, b)
	}
	add("heading", 1, "Title")
	add("heading", 2, "A")
	add("paragraph", 0, "a1")
	add("heading", 3, "A.1")
	add("paragraph", 0, "a2")
	add("heading", 2, "B")
	add("paragraph", 0, "b1")
	add("heading", 2, "C")
	return d
}

// A section moves with what it heads, as one step of the history, and
// never into itself or before the page title.
func TestMoveSection(t *testing.T) {
	d := sectionDoc()
	e := NewEngine(d)
	if end := e.Doc.Section(1); end != 5 {
		t.Fatalf("section of A ends at %d", end)
	}
	b, a := e.Doc.Blocks[5].ID, e.Doc.Blocks[1].ID
	e.MoveSection(b, a)
	want := []string{"Title", "B", "b1", "A", "a1", "A.1", "a2", "C"}
	if got := headings(e.Doc); !slices.Equal(got, want) {
		t.Fatalf("moved B up: %q", got)
	}
	e.MoveSection(a, "")
	want = []string{"Title", "B", "b1", "C", "A", "a1", "A.1", "a2"}
	if got := headings(e.Doc); !slices.Equal(got, want) {
		t.Fatalf("moved A to the end: %q", got)
	}
	e.Undo()
	e.Undo()
	if got := headings(e.Doc); !slices.Equal(got, headings(d)) {
		t.Fatalf("undo: %q", got)
	}
	v := e.Version()
	e.MoveSection(a, e.Doc.Blocks[3].ID)                  // into itself
	e.MoveSection(a, e.Doc.Blocks[0].ID)                  // before the title
	e.MoveSection(e.Doc.Blocks[0].ID, e.Doc.Blocks[5].ID) // the title
	if e.Version() != v {
		t.Error("an impossible move changed the document")
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatal(err)
	}
}

// The page's icon and cover round-trip, as steps of the history.
func TestPageIconAndCover(t *testing.T) {
	e := NewEngine(New())
	e.SetPageIcon("🚀")
	e.SetCover("gradient:sea")
	e.SetCover("../outside.png")
	text, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	d, err := Decode(text)
	if err != nil || d.Icon != "🚀" || d.Cover != "gradient:sea" {
		t.Fatalf("round trip %+v %v", d, err)
	}
	e.Undo()
	if e.Doc.Cover != "" || e.Doc.Icon != "🚀" {
		t.Errorf("undo cover: %+v", e.Doc)
	}
	e.SetCover("assets/cover.png")
	if e.Doc.Cover != "assets/cover.png" {
		t.Error("picture cover")
	}
	// The part of the picture shown is kept, bounded, undone in one step,
	// and starts from the middle again for another cover.
	e.SetCoverY(-0.4)
	e.SetCoverY(3)
	if e.Doc.CoverY != 1 {
		t.Fatalf("cover y %v, want bounded to 1", e.Doc.CoverY)
	}
	text, _ = e.Doc.Encode()
	if d, err := Decode(text); err != nil || d.CoverY != 1 {
		t.Fatalf("cover y round trip %+v %v", d, err)
	}
	e.Undo()
	if e.Doc.CoverY != -0.4 {
		t.Errorf("undo cover y: %v", e.Doc.CoverY)
	}
	e.SetCover("assets/other.png")
	if e.Doc.CoverY != 0 {
		t.Errorf("a new cover kept y %v", e.Doc.CoverY)
	}
	bad := strings.Replace(text, `"coverY": 1`, `"coverY": 2`, 1)
	if _, err := Decode(bad); err == nil {
		t.Error("a cover y out of range decoded")
	}
}

// Replacing the document and mapping its text are single steps.
func TestReplaceDocumentAndMapText(t *testing.T) {
	e := NewEngine(sectionDoc())
	other := New()
	other.Blocks[0].Runs = []Run{{Text: "see [[Old]]"}}
	other.Icon = "📚"
	e.ReplaceDocument(other)
	if got := headings(e.Doc); !slices.Equal(got, []string{"see [[Old]]"}) || e.Doc.Icon != "📚" {
		t.Fatalf("replaced: %q", got)
	}
	if e.MapText(func(s string) string { return s }) {
		t.Error("unchanged text reported a change")
	}
	if !e.MapText(func(s string) string { return "see [[New]]" }) || e.Doc.Blocks[0].Text() != "see [[New]]" {
		t.Error("map text")
	}
	e.Undo()
	e.Undo()
	if e.Doc.Blocks[1].Text() != "A" {
		t.Errorf("undo: %q", headings(e.Doc))
	}
}
