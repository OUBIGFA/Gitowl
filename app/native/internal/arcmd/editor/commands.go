package editor

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
)

// Commands the host offers in its command bar.

// StartWikiLink types "[[" at the caret, which opens the list of documents
// to link to.
func (e *Editor) StartWikiLink() {
	e.ReplaceText(nil, "[[")
	e.wiki.dismissed = -1
	e.RequestFocus = true
}

// InsertMath inserts a math block after the caret's block and puts the
// caret in it.
func (e *Editor) InsertMath() {
	e.Engine.Insert(document.NewBlock("math", ""))
	e.RequestFocus = true
}

// EditPageIcon gives the page an icon when it has none and opens the icon
// picker.
func (e *Editor) EditPageIcon() {
	if e.Doc.Icon == "" {
		e.Engine.SetPageIcon(pageEmoji[0])
	}
	e.header.iconOpen, e.header.coverOpen = true, false
}

// EditCover gives the page a cover when it has none and opens the cover
// picker.
func (e *Editor) EditCover() {
	if e.Doc.Cover == "" {
		e.Engine.SetCover("gradient:" + document.CoverGradients[0])
	}
	e.header.coverOpen, e.header.iconOpen = true, false
}

// sectionHeadings are the indexes of the top-level headings of the
// section level of the heading holding the caret: the caret's section
// heading, and its siblings.
func (e *Editor) caretSection() int {
	owner := e.Engine.LeafAt(e.Caret).Owner
	at := -1
	for i, b := range e.Doc.Blocks {
		if b.Kind == "heading" && i > 0 {
			at = i
		}
		if b.ID == owner {
			break
		}
	}
	return at
}

// MoveSectionAtCaret moves the section the caret is in before the section
// above it of the same level (delta -1) or after the one below (+1), as
// one step of the history.
func (e *Editor) MoveSectionAtCaret(delta int) {
	i := e.caretSection()
	if i <= 0 {
		return
	}
	blocks := e.Doc.Blocks
	level := max(1, blocks[i].Level)
	id := blocks[i].ID
	if delta < 0 {
		for j := i - 1; j > 0; j-- {
			b := blocks[j]
			if b.Kind == "heading" && max(1, b.Level) < level {
				return // the top of its parent section
			}
			if b.Kind == "heading" && max(1, b.Level) == level {
				e.Engine.MoveSection(id, b.ID)
				return
			}
		}
		return
	}
	end := e.Doc.Section(i)
	if end >= len(blocks) || blocks[end].Kind != "heading" || max(1, blocks[end].Level) != level {
		return
	}
	after := e.Doc.Section(end)
	before := ""
	if after < len(blocks) {
		before = blocks[after].ID
	}
	e.Engine.MoveSection(id, before)
}

// InsertToggle turns the caret's empty paragraph, or a new one below, into
// a toggle with a line of content under it.
func (e *Editor) InsertToggle() {
	for _, it := range insertions {
		if it.kind == "toggle" {
			leaf := e.Engine.LeafAt(e.Caret)
			if leaf.Block.Kind != "paragraph" || leaf.Block.Text() != "" {
				e.Engine.Insert(document.NewBlock("paragraph", ""))
				e.sync()
			}
			e.insertItem(it)
			return
		}
	}
}
