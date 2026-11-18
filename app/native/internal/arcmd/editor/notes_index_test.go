package editor

import (
	"maps"
	"math/rand"
	"slices"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
)

func TestIncrementalFootnotesMatchCompleteIndex(t *testing.T) {
	e := New("plain\n\n[^b] before [^a]\n\n`[^a]`\n\n```text\n[^a]\n```\n\n[^a]: Alpha\n\n[^b]: Beta\n\n[^c]: Unreferenced")
	e.collectNotes()
	if e.notes["b"].number != 1 || e.notes["a"].number != 2 || e.notes["c"].number != 3 {
		t.Fatal("initial reference ordering")
	}
	check := func() {
		t.Helper()
		e.collectNotes()
		full := FromDocument(e.Doc)
		full.collectNotes()
		if !maps.Equal(e.notes, full.notes) || e.noteKey() != full.noteKey() || !slices.Equal(e.noteOrder, full.noteOrder) || !slices.EqualFunc(e.noteRefs, full.noteRefs, func(a, b noteReferences) bool {
			return a.id == b.id && a.start == b.start && slices.Equal(a.labels, b.labels)
		}) {
			t.Fatalf("incremental footnotes differ: %v / %v", e.notes, full.notes)
		}
	}
	// References added/removed before existing ones must renumber all labels.
	e.Engine.Replace(0, 0, "[^c] ")
	check()
	if e.notes["c"].number != 1 || e.notes["b"].number != 2 {
		t.Fatal("new first reference did not renumber notes")
	}
	e.Undo()
	check()
	e.Redo()
	check()
	// Structural changes and formatting invalidate the text-only shortcut.
	e.SetSelection(0, 4)
	e.Engine.Toggle("code")
	check()
	e.Engine.InsertFootnote()
	check()
	e.Undo()
	check()
	rng := rand.New(rand.NewSource(61))
	for range 250 {
		leaves := e.Doc.Leaves()
		leaf := leaves[rng.Intn(len(leaves))]
		a, z := leaf.Start+rng.Intn(leaf.End-leaf.Start+1), leaf.Start+rng.Intn(leaf.End-leaf.Start+1)
		e.SetSelection(a, z)
		switch rng.Intn(8) {
		case 0:
			e.Undo()
		case 1:
			e.Redo()
		case 2:
			e.Engine.Toggle("code")
		case 3:
			e.Engine.SetLink("https://example.com")
		default:
			e.Engine.Replace(a, z, []string{"[^a]", "[^c][^b]", "\\[^a]", "中文😀", "", "$[^b]$"}[rng.Intn(6)])
		}
		check()
	}
}

func TestFootnoteIndexGainsDefinitionsAndSkipsLiterals(t *testing.T) {
	d := document.New()
	d.Blocks[0].Runs = []document.Run{{Text: "[^b]", Style: document.Style{NoMath: true}}, {Text: "[^a]"}}
	e := FromDocument(d)
	e.collectNotes()
	e.Engine.Replace(0, 0, "prefix😀")
	e.collectNotes()
	if len(e.notes) != 0 {
		t.Fatal("undefined references were numbered")
	}
	for _, label := range []string{"a", "b", "a"} {
		b := document.NewBlock("footnote", "definition")
		b.NoteLabel = label
		e.Engine.InsertAt(len(e.Doc.Blocks), b)
	}
	e.collectNotes()
	if len(e.notes) != 2 || e.notes["a"].number != 1 || e.notes["b"].number != 2 || e.notes["a"].id != e.Doc.Blocks[1].ID {
		t.Fatal("definitions or literal references indexed incorrectly", e.notes)
	}
	before := e.notes["b"].start
	e.Engine.Replace(0, 0, "😀")
	e.collectNotes()
	if e.notes["b"].start != before+2 {
		t.Fatal("footnote target did not follow a UTF-16 edit")
	}
}
