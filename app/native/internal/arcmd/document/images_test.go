package document

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestImageReplacementRandomUndoSequence(t *testing.T) {
	e := NewEngine(New())
	b := NewBlock("image", "Description")
	b.URL = "missing.png"
	e.Insert(b)
	encoded, _ := e.Doc.Encode()
	states := []string{encoded}
	rng := rand.New(rand.NewSource(93))
	for i := 0; i < 40; i++ {
		if rng.Intn(3) == 0 {
			e.SetSelection(0, 0)
			e.Replace(0, 0, "字")
		} else {
			if !e.ReplaceImage(b.ID, e.Doc.Blocks[0].URL, fmt.Sprintf("assets/%d.png", i)) {
				t.Fatal("valid replacement rejected")
			}
		}
		encoded, err := e.Doc.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(encoded); err != nil {
			t.Fatal("replacement produced an invalid document", err)
		}
		states = append(states, encoded)
	}
	for i := len(states) - 2; i >= 0; i-- {
		e.Undo()
		if encoded, _ := e.Doc.Encode(); encoded != states[i] {
			t.Fatalf("undo %d lost image data", i)
		}
	}
	for i := 1; i < len(states); i++ {
		e.Redo()
		if encoded, _ := e.Doc.Encode(); encoded != states[i] {
			t.Fatalf("redo %d lost image data", i)
		}
	}
}

func TestReplaceImageKeepsIdentityUndoAndEncoding(t *testing.T) {
	e := NewEngine(New())
	b := NewBlock("image", "A description")
	b.URL = "missing.png"
	e.Insert(b)
	before, _ := e.Doc.Encode()
	if !e.ReplaceImage(b.ID, "missing.png", "assets/fixed.png") {
		t.Fatal("replace")
	}
	if e.Doc.Blocks[0].ID != b.ID || e.Doc.Blocks[0].Text() != "A description" {
		t.Fatal("replacement changed image identity or description")
	}
	text, err := e.Doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Decode(text)
	if err != nil || doc.Blocks[0].URL != "assets/fixed.png" {
		t.Fatal("replacement did not round trip", err)
	}
	e.Undo()
	if got, _ := e.Doc.Encode(); got != before {
		t.Fatal("undo changed more than the image source")
	}
	e.Redo()
	if e.ReplaceImage(b.ID, "missing.png", "stale.png") || e.ReplaceImage("deleted", "assets/fixed.png", "other.png") {
		t.Fatal("stale image replacement accepted")
	}
}

func TestRelocateImagesKeepsUndoAndRoundTrip(t *testing.T) {
	doc := New()
	picture := NewBlock("image", "A picture")
	picture.URL = "assets/old.png"
	doc.Blocks = append(doc.Blocks, picture)
	doc.Cover = "assets/old.png"
	e := NewEngine(doc)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 30; i++ {
		e.SetSelection(rng.Intn(Len(e.Doc.Blocks[0].Text())+1), 0)
		e.Replace(e.Anchor, e.Caret, "x")
		if i%5 == 0 {
			e.SetCover("")
			e.SetCover("assets/old.png")
		}
	}
	e.Undo()
	e.RelocateImages(map[string]string{"assets/old.png": "assets/moved.png"})
	check := func() {
		t.Helper()
		e.Doc.MapImages(func(url string) string {
			if url == "assets/old.png" {
				t.Fatal("old URL restored")
			}
			return url
		})
		text, err := e.Doc.Encode()
		if err != nil {
			t.Fatal(err)
		}
		round, err := Decode(text)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := round.Encode(); again != text {
			t.Fatal("round trip changed document")
		}
	}
	check()
	for e.CanUndo() {
		e.Undo()
		check()
	}
	for e.CanRedo() {
		e.Redo()
		check()
	}
}
