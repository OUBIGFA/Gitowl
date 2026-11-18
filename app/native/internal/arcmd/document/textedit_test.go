package document

import (
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTextHistoryMatchesFullTransactions(t *testing.T) {
	doc := New()
	doc.Blocks = []*Block{NewBlock("paragraph", "中文😀 hello"), Table(2, 2), Columns(), NewBlock("paragraph", "last")}
	fast, full := NewEngine(doc), NewEngine(doc)
	rng := rand.New(rand.NewSource(42))
	for step := 0; step < 600; step++ {
		leaves := fast.Doc.Leaves()
		leaf := leaves[rng.Intn(len(leaves))]
		a, z := leaf.Start+rng.Intn(leaf.End-leaf.Start+1), leaf.Start+rng.Intn(leaf.End-leaf.Start+1)
		fast.SetSelection(a, z)
		full.SetSelection(a, z)
		switch rng.Intn(10) {
		case 0:
			fast.Undo()
			full.Undo()
		case 1:
			fast.Redo()
			full.Redo()
		case 2:
			fast.Toggle("bold")
			full.Toggle("bold")
		case 3:
			fast.ToggleFold(fast.Doc.Blocks[0].ID)
			full.ToggleFold(full.Doc.Blocks[0].ID)
		default:
			text := []string{"a", " ", "", "中文😀", "e\u0301", "\u2028", "#"}[rng.Intn(7)]
			fast.Replace(a, z, text)
			full.Edit(func() { full.Replace(a, z, text) })
		}
		if !reflect.DeepEqual(fast.Doc, full.Doc) || fast.Anchor != full.Anchor || fast.Caret != full.Caret || fast.Typing != full.Typing || fast.Explicit != full.Explicit {
			t.Fatalf("step %d diverged from full-snapshot transactions", step)
		}
		if err := fast.Doc.Validate(); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		for _, want := range fast.Doc.Leaves() {
			for _, at := range []int{want.Start, want.End} {
				got := fast.LeafAt(at)
				if got.Block.ID != want.Block.ID || got.Start != want.Start || got.End != want.End {
					t.Fatalf("step %d: stale leaf index at %d", step, at)
				}
			}
		}
		assertLeafOffsets(t, fast)
	}
	encoded, err := fast.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, fast.Doc) {
		t.Fatal("text history changed the saved format", err)
	}
}

func TestTextHistoryDoesNotAliasAndKeepsFoldState(t *testing.T) {
	e := textEngine("a")
	e.Replace(1, 1, "b")
	e.SetKind("heading", 2)
	e.ToggleFold(e.Doc.Blocks[0].ID)
	e.Replace(2, 2, "c")
	snapshot := e.Capture()
	e.Undo()
	if e.Doc.PlainText() != "ab" || !e.Doc.Blocks[0].Folded {
		t.Fatal("text undo changed folding")
	}
	e.Redo()
	e.Doc.Blocks[0].Runs[0].Text = "external"
	if snapshot.Doc.PlainText() != "abc" {
		t.Fatal("capture aliases current runs")
	}
	e.Undo()
	if e.Doc.PlainText() != "ab" {
		t.Fatal("history aliases current runs")
	}
}

func TestUTF16LengthAndBatchFoldControls(t *testing.T) {
	for _, text := range []string{"", "ASCII", "中文😀", "e\u0301", "👨‍👩‍👧‍👦", "\xff\xfe"} {
		if Len(text) != len(utf16.Encode([]rune(text))) {
			t.Fatalf("UTF-16 length changed for %q", text)
		}
	}
	d := New()
	d.Blocks = []*Block{NewBlock("heading", "Title"), NewBlock("paragraph", "body"), NewBlock("bullet", "parent"), NewBlock("bullet", "child"), Columns(), Table(2, 2)}
	d.Blocks[0].Level = 1
	d.Blocks[3].Indent = 1
	d.Blocks[4].Children[0].Children = []*Block{NewBlock("heading", "inner")}
	d.Blocks[4].Children[0].Children[0].Level = 2
	batch := d.FoldableBlocks()
	for _, list := range siblingLists(d) {
		for _, b := range list {
			if batch[b.ID] != d.CanFold(b.ID) {
				t.Fatal("batch fold controls differ", b.ID)
			}
		}
	}
}

func TestSameDocumentAndSkeletonDigest(t *testing.T) {
	a := New()
	a.Blocks = []*Block{
		NewBlock("heading", "Title"),
		NewBlock("paragraph", "body text"),
		Table(2, 2),
	}
	a.Blocks[0].Level = 1
	b := a.Clone()
	if !SameDocument(a, b) {
		t.Fatal("SameDocument returned false for exact clone")
	}
	da, err := a.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if db, err := b.Digest(); err != nil || da != db {
		t.Fatalf("Digest mismatch on clone: %v", err)
	}
	if a.SkeletonDigest() != b.SkeletonDigest() {
		t.Fatal("SkeletonDigest mismatch on clone")
	}

	// Mutating leaf runs preserves SkeletonDigest while failing SameDocument.
	b.Blocks[1].Runs = []Run{{Text: "modified body", Style: Style{Bold: true}}}
	if SameDocument(a, b) {
		t.Fatal("SameDocument missed leaf run change")
	}
	if a.SkeletonDigest() != b.SkeletonDigest() {
		t.Fatal("SkeletonDigest changed on leaf-only run edit")
	}

	// Structural or metadata changes invalidate SkeletonDigest.
	c := a.Clone()
	c.Blocks = append(c.Blocks, NewBlock("paragraph", "extra"))
	if c.SkeletonDigest() == a.SkeletonDigest() {
		t.Fatal("SkeletonDigest did not change after adding a block")
	}
}

func TestSplitRunsByNewlineMatchesSliceRuns(t *testing.T) {
	runs := Compact([]Run{
		{Text: "first 😀\nsecond ", Style: Style{Bold: true}},
		{Text: "styled\n\ntrailing", Style: Style{Italic: true}},
		{Text: "\n"},
	})
	got := splitRunsByNewline(runs)
	plain := (&Block{Runs: runs}).Text()
	parts := strings.Split(plain, "\n")
	if len(got) != len(parts) {
		t.Fatalf("line count = %d, want %d", len(got), len(parts))
	}
	offset := 0
	for i, part := range parts {
		want := SliceRuns(runs, offset, offset+Len(part))
		if !slices.Equal(got[i], want) {
			t.Fatalf("line %d mismatch: got %+v, want %+v", i, got[i], want)
		}
		offset += Len(part) + 1
	}
}

func TestSingleLeafIMECompositionAndFormatting(t *testing.T) {
	doc := New()
	doc.Blocks = []*Block{NewBlock("paragraph", "hello world"), NewBlock("paragraph", "second")}
	fast, full := NewEngine(doc), NewEngine(doc)

	// Single-leaf preedit -> candidate updates -> commit matches Snapshot path.
	fast.SetSelection(6, 11)
	full.SetSelection(6, 11)
	ls, ok := fast.CaptureLeaf(6, 11)
	if !ok {
		t.Fatal("expected single-leaf CaptureLeaf to succeed")
	}
	fullSnap := full.Capture()

	if !fast.PreviewLeafReplace(6, 11, "ni") {
		t.Fatal("PreviewLeafReplace failed")
	}
	full.PreviewReplace(6, 11, "ni")
	if !fast.PreviewLeafReplace(6, 8, "ni'hao") {
		t.Fatal("second PreviewLeafReplace failed")
	}
	full.PreviewReplace(6, 8, "ni'hao")

	revBeforeCommit := fast.Revision()
	if !fast.CommitLeaf(ls, 6, 12, "你好") {
		t.Fatal("CommitLeaf failed")
	}
	if ch, ok := fast.TextChangeSince(revBeforeCommit); !ok || ch.After != "hello 你好" {
		t.Fatalf("CommitLeaf did not publish incremental TextChange: ok=%v ch=%+v", ok, ch)
	}
	fullPreview := full.Capture()
	full.Restore(fullSnap)
	full.Edit(func() {
		full.Restore(fullPreview)
		full.Replace(6, 12, "你好")
	})
	if !SameDocument(fast.Doc, full.Doc) || fast.Anchor != full.Anchor || fast.Caret != full.Caret {
		t.Fatalf("IME commit diverged: fast=%q(%d,%d) full=%q(%d,%d)", fast.Doc.PlainText(), fast.Anchor, fast.Caret, full.Doc.PlainText(), full.Anchor, full.Caret)
	}

	// Single-leaf formatting publishes TextChange and undoes cleanly.
	revFmt := fast.Revision()
	fast.SetSelection(0, 5)
	full.SetSelection(0, 5)
	fast.Toggle("bold")
	full.Toggle("bold")
	if ch, ok := fast.TextChangeSince(revFmt); !ok || ch.Before != ch.After {
		t.Fatalf("single-leaf Toggle did not publish style-only TextChange: ok=%v ch=%+v", ok, ch)
	}
	if !SameDocument(fast.Doc, full.Doc) {
		t.Fatal("single-leaf Toggle diverged")
	}
	fast.Undo()
	fast.Undo()
	full.Undo()
	full.Undo()
	if !SameDocument(fast.Doc, doc) {
		t.Fatalf("undo sequence did not restore original document: got %q", fast.Doc.PlainText())
	}

	// UnmarkText pattern: commit identical preview text must still advance version and revision.
	fast.SetSelection(0, 0)
	ls2, ok := fast.CaptureLeaf(0, 0)
	if !ok || !fast.PreviewLeafReplace(0, 0, "中文") {
		t.Fatal("preedit failed before UnmarkText test")
	}
	verBefore := fast.Version()
	revBefore := fast.Revision()
	if !fast.CommitLeaf(ls2, 0, 2, "中文") {
		t.Fatal("CommitLeaf on identical preview text failed")
	}
	if fast.Version() != verBefore+1 {
		t.Fatalf("version did not increment on UnmarkText-style commit")
	}
	if _, ok := fast.TextChangeSince(revBefore); !ok {
		t.Fatal("revision/TextChange did not publish on UnmarkText-style commit")
	}

	// Markdown prefix at start of block defers to structural replacement.
	fast.SetSelection(0, 0)
	ls3, ok := fast.CaptureLeaf(0, 0)
	if !ok || !fast.PreviewLeafReplace(0, 0, "#") {
		t.Fatal("prefix preedit failed")
	}
	if fast.CommitLeaf(ls3, 0, 1, " ") {
		t.Fatal("CommitLeaf unexpectedly swallowed start-of-block space prefix")
	}
	fast.RestoreLeaf(ls3)
}
