package document

import (
	"strconv"
	"strings"
)

// SetScript toggles one mutually exclusive baseline style.
func (e *Engine) SetScript(script string) {
	if e.ReadOnly || script != "" && script != "sup" && script != "sub" {
		return
	}
	if e.CurrentStyle().Script == script {
		script = ""
	}
	if e.Anchor == e.Caret {
		s := e.CurrentStyle()
		s.Script = script
		e.Typing, e.Explicit = s, true
		return
	}
	e.applyFormat(func(s *Style) { s.Script = script })
}

func (e *Engine) SetImageWidth(id string, width float32) {
	if !(width >= 0 && width <= 10000) {
		return
	}
	e.Edit(func() {
		if p, i := e.find(id); p != nil && (*p)[i].Kind == "image" {
			(*p)[i].ImageWidth = width
		}
	})
}

// InsertFootnote creates a unique definition and reference in one undo step.
func (e *Engine) InsertFootnote() {
	e.Edit(func() {
		used := map[string]bool{}
		for _, l := range e.Doc.Leaves() {
			if l.Block.Kind == "footnote" {
				used[l.Block.NoteLabel] = true
			}
		}
		n := 1
		for used[strconv.Itoa(n)] {
			n++
		}
		label := strconv.Itoa(n)
		e.Replace(min(e.Anchor, e.Caret), max(e.Anchor, e.Caret), "[^"+label+"]")
		b := NewBlock("footnote", "")
		b.NoteLabel = label
		e.Doc.Blocks = append(e.Doc.Blocks, b)
		e.FocusBlock(b.ID)
	})
}

// Pasted definitions get fresh labels when the destination already owns one.
// References without a copied definition continue to refer to the destination.
func (e *Engine) pastedNotes(fragment *Document) *Document {
	used := map[string]bool{}
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.Kind == "footnote" {
			used[leaf.Block.NoteLabel] = true
		}
	}
	renamed := map[string]string{}
	for _, leaf := range fragment.Leaves() {
		b := leaf.Block
		if b.Kind != "footnote" {
			continue
		}
		label := b.NoteLabel
		if used[label] {
			for n := 2; ; n++ {
				next := label + "-" + strconv.Itoa(n)
				if !used[next] {
					renamed[label] = next
					used[next] = true
					break
				}
			}
		} else {
			used[label] = true
		}
	}
	if len(renamed) == 0 {
		return fragment
	}
	out := fragment.Clone()
	var pairs []string
	for old, next := range renamed {
		pairs = append(pairs, "[^"+old+"]", "[^"+next+"]")
	}
	replace := strings.NewReplacer(pairs...)
	for _, leaf := range out.Leaves() {
		b := leaf.Block
		if next, ok := renamed[b.NoteLabel]; ok && b.Kind == "footnote" {
			b.NoteLabel = next
		}
		if isVerbatim(b) || b.Kind == "image" {
			continue
		}
		for i := range b.Runs {
			if !b.Runs[i].Style.Code {
				b.Runs[i].Text = replace.Replace(b.Runs[i].Text)
			}
		}
	}
	return out
}
