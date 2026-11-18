package document

import "testing"

// Compare cached coordinates with the serialized text, not another index.
func assertLeafOffsets(t *testing.T, e *Engine) {
	t.Helper()
	text := e.Doc.PlainText()
	start := 0
	for _, leaf := range e.Doc.Leaves() {
		end := start + len(leaf.Block.Text())
		for _, at := range []int{leaf.Start, leaf.End} {
			got := e.LeafAt(at)
			if got.Block.ID != leaf.Block.ID || got.ByteStart != start || got.ByteEnd != end || got.Start != Len(text[:start]) || got.End != Len(text[:end]) {
				t.Fatalf("stale UTF-16/byte index at %d: %#v", at, got)
			}
		}
		for _, at := range []int{start, end} {
			got := e.LeafAtByte(at)
			if got.Block.ID != leaf.Block.ID || got.ByteStart != start || got.ByteEnd != end {
				t.Fatalf("stale byte lookup at %d: %#v", at, got)
			}
		}
		start = end + 1
	}
}

func TestLeafOffsetsDuringTransactions(t *testing.T) {
	e := textEngine("中文😀")
	assertLeafOffsets(t, e)
	e.Edit(func() {
		e.Replace(0, 0, "👨‍👩‍👧‍👦")
		assertLeafOffsets(t, e)
		e.Insert(Table(2, 2))
		assertLeafOffsets(t, e)
		e.Replace(e.Caret, e.Caret, "e\u0301")
		assertLeafOffsets(t, e)
	})
	assertLeafOffsets(t, e)
	e.Undo()
	assertLeafOffsets(t, e)
	e.Redo()
	assertLeafOffsets(t, e)
}

func TestIndexedTextRangesAndHistory(t *testing.T) {
	e := textEngine("中文😀")
	e.Insert(Table(2, 2))
	e.Insert(Columns())
	e.Insert(NewBlock("code", "line\n👨‍👩‍👧‍👦"))
	check := func() {
		t.Helper()
		text := e.Doc.PlainText()
		n := Len(text)
		if e.TextLength() != n {
			t.Fatal("indexed length changed")
		}
		for a := -1; a <= n+1; a++ {
			for z := a; z <= n+1; z++ {
				want := text[ByteOffset(text, max(0, a)):ByteOffset(text, max(0, z))]
				if got := e.TextRange(a, z); got != want {
					t.Fatalf("range %d:%d: %q / %q", a, z, got, want)
				}
			}
		}
		for _, leaf := range e.Doc.Leaves() {
			got, ok := e.LeafByID(leaf.Block.ID)
			if !ok || got.Start != leaf.Start || got.ByteStart != leaf.ByteStart {
				t.Fatal("ID index changed position")
			}
		}
	}
	check()
	for range 260 {
		e.Replace(1, 1, "x")
	}
	// The bounded undo queue must retain the newest 200 edits in order.
	for range 200 {
		e.Undo()
	}
	if e.CanUndo() {
		t.Fatal("history exceeded its bound")
	}
	check()
	e.Redo()
	check()
	before := e.Doc.PlainText()
	revision, version := e.Revision(), e.Version()
	e.PreviewReplace(0, 1, "候选😀")
	if _, ok := e.TextChangeSince(revision); !ok || e.Version() != version {
		t.Fatal("preedit did not publish an uncommitted text change")
	}
	if before == e.Doc.PlainText() {
		t.Fatal("preedit missing")
	}
	check()
}
