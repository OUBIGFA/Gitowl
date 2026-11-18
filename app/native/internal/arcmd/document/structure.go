package document

// Structural deletion. Text deletes freely inside a leaf and across sibling
// paragraphs, but never merges across the edge of a table cell, a column, a
// table or a column layout: an empty structure is removed as a whole, and a
// structure that still holds content is stepped over by moving the caret.

// isRule reports a block without text of its own.
func isRule(b *Block) bool { return b.Kind == "rule" }

// isMedia reports a block whose text is a caption of something else.
func isMedia(b *Block) bool { return b.Kind == "image" }

// isVerbatim reports a block whose text keeps newlines, so it cannot turn
// into a paragraph while it holds text.
func isVerbatim(b *Block) bool { return b.Kind == "code" || b.Kind == "raw" || b.Kind == "math" }

// Empty reports whether a block holds nothing: no text, no rule and no
// attachment anywhere inside it.
func Empty(b *Block) bool {
	if len(b.Children) == 0 {
		return !isRule(b) && !isMedia(b) && b.Text() == ""
	}
	for _, child := range b.Children {
		if !Empty(child) {
			return false
		}
	}
	return true
}

// topIndex is the index of a top-level block in the document.
func (e *Engine) topIndex(id string) int {
	for i, b := range e.Doc.Blocks {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// removeTop removes the top-level block at i and puts the caret where it
// was: at the end of the block before for a backward delete, else at the
// start of the block after. A document never becomes empty.
func (e *Engine) removeTop(i int, backward bool) {
	e.Doc.Blocks = append(e.Doc.Blocks[:i], e.Doc.Blocks[i+1:]...)
	if len(e.Doc.Blocks) == 0 {
		e.Doc.Blocks = []*Block{NewBlock("paragraph", "")}
	}
	if backward && i > 0 {
		e.caretAtEnd(e.Doc.Blocks[i-1])
		return
	}
	e.FocusBlock(e.Doc.Blocks[min(i, len(e.Doc.Blocks)-1)].ID)
}

// removeLeaf removes a leaf block. A top-level leaf goes like removeTop; a
// paragraph in a column leaves an empty paragraph behind if it was the last.
func (e *Engine) removeLeaf(leaf Leaf, backward bool) {
	if i := e.topIndex(leaf.Block.ID); i >= 0 {
		e.removeTop(i, backward)
		return
	}
	parent, index := e.find(leaf.Block.ID)
	if parent == nil {
		return
	}
	*parent = append((*parent)[:index], (*parent)[index+1:]...)
	if len(*parent) == 0 {
		*parent = []*Block{NewBlock("paragraph", "")}
	}
	if backward && index > 0 {
		e.caretAtEnd((*parent)[index-1])
		return
	}
	e.FocusBlock((*parent)[min(index, len(*parent)-1)].ID)
}

// caretAtEnd collapses the selection at the end of a block's last leaf.
func (e *Engine) caretAtEnd(b *Block) {
	end := -1
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Owner == b.ID || leaf.Block.ID == b.ID || e.contains(b, leaf.Block) {
			end = leaf.End
		}
	}
	if end >= 0 {
		e.Anchor, e.Caret = end, end
		e.Explicit = false
	}
}

func (e *Engine) contains(b, leaf *Block) bool {
	for _, child := range b.Children {
		if child == leaf || e.contains(child, leaf) {
			return true
		}
	}
	return false
}

// Join handles Backspace (backward) or Delete with a collapsed caret at the
// edge of its leaf, where plain text deletion would merge two blocks. It
// reports whether it handled the key; when it did not, the caller deletes
// text as usual.
func (e *Engine) Join(backward bool) bool {
	if e.Anchor != e.Caret {
		return false
	}
	leaf := e.LeafAt(e.Caret)
	if backward && e.Caret != leaf.Start || !backward && e.Caret != leaf.End {
		return false
	}
	leaves := e.Doc.Leaves()
	index := -1
	for i, leaf := range leaves {
		if e.Caret <= leaf.End {
			index = i
			break
		}
	}
	if index < 0 {
		return false
	}
	here := leaves[index]
	if backward && e.Caret != here.Start || !backward && e.Caret != here.End {
		return false
	}
	var there *Leaf
	if backward && index > 0 {
		there = &leaves[index-1]
	}
	if !backward && index+1 < len(leaves) {
		there = &leaves[index+1]
	}
	owner := e.Doc.Blocks[max(0, e.topIndex(here.Owner))]
	handled := true
	e.Edit(func() {
		switch {
		case len(owner.Children) > 0 && Empty(owner):
			// An emptied table or column layout goes as a whole.
			e.removeTop(e.topIndex(owner.ID), backward)
		case isRule(here.Block) || isMedia(here.Block) && here.Block.Text() == "":
			e.removeLeaf(here, backward)
		case backward && here.Block.Kind != "paragraph" && here.Block.Kind != "cell" && !isMedia(here.Block) && !(isVerbatim(here.Block) && here.Block.Text() != ""):
			// Backspace at the start of a styled block drops the style first,
			// as in Notion and Yuque: an emptied callout or heading becomes a
			// paragraph, and the next Backspace joins it to the block above.
			b := here.Block
			b.Kind, b.Level, b.Checked, b.Language, b.Color, b.URL = "paragraph", 0, false, "", "", ""
			e.Anchor, e.Caret = here.Start, here.Start
		case there == nil:
			handled = false
		case there.Parent != here.Parent || here.Block.Kind == "cell" || isMedia(here.Block) || isVerbatim(here.Block):
			// Different structures never merge. An empty paragraph beside a
			// structure is removed; otherwise the caret steps over the edge.
			if here.Block.Kind == "paragraph" && here.Block.Text() == "" && e.topIndex(here.Block.ID) >= 0 {
				e.removeLeaf(here, backward)
				return
			}
			e.step(there, backward)
		case isRule(there.Block):
			e.removeLeaf(*there, !backward)
			if !backward {
				e.Anchor, e.Caret = here.End, here.End
			}
		case isMedia(there.Block) || isVerbatim(there.Block) && there.Block.Text() != "":
			if here.Block.Text() == "" {
				e.removeLeaf(here, backward)
				return
			}
			e.step(there, backward)
		default:
			handled = false
		}
	})
	return handled
}

// step moves the caret over a structural edge into the neighbouring leaf.
func (e *Engine) step(there *Leaf, backward bool) {
	at := there.Start
	if backward {
		at = there.End
	}
	e.Anchor, e.Caret = at, at
	e.Explicit = false
}

// replaceAcross replaces a range whose ends lie in different structures, or
// in different cells of one row. Tables and column layouts the range covers
// and reaches past are removed; others keep their shape and lose only the
// covered text, so clearing a table's content leaves the table, which the
// next Backspace removes (see Join). When both ends are top-level text blocks, they join.
func (e *Engine) replaceAcross(a, z int, text string, style Style) {
	leaves := e.Doc.Leaves()
	first, last := e.LeafAt(a), e.LeafAt(z)
	span := map[string][2]int{}
	for _, leaf := range leaves {
		s, ok := span[leaf.Owner]
		if !ok {
			s[0] = leaf.Start
		}
		s[1] = leaf.End
		span[leaf.Owner] = s
	}
	covered := map[string]bool{}
	for _, b := range e.Doc.Blocks {
		if s, ok := span[b.ID]; ok && len(b.Children) > 0 && a <= s[0] && s[1] <= z && (a < s[0] || s[1] < z) {
			covered[b.ID] = true
		}
	}
	for _, leaf := range leaves {
		if leaf.End < a || leaf.Start > z || covered[leaf.Owner] {
			continue
		}
		prefix := SliceRuns(leaf.Block.Runs, 0, max(0, a-leaf.Start))
		suffix := SliceRuns(leaf.Block.Runs, max(0, z-leaf.Start), leaf.End-leaf.Start)
		leaf.Block.Runs = Compact(append(prefix, suffix...))
	}
	// The covered structures go; the first one's place takes the text if
	// nothing else can.
	var target *Block
	offset := 0
	at := -1
	kept := e.Doc.Blocks[:0:0]
	for i, b := range e.Doc.Blocks {
		if covered[b.ID] {
			if at < 0 {
				at = i
			}
			continue
		}
		kept = append(kept, b)
	}
	e.Doc.Blocks = kept
	firstTop, lastTop := e.topIndex(first.Block.ID), e.topIndex(last.Block.ID)
	switch {
	case !covered[first.Owner]:
		target, offset = first.Block, max(0, a-first.Start)
		if firstTop >= 0 && lastTop >= 0 && first.Block != last.Block {
			first.Block.Runs = Compact(append(first.Block.Runs, last.Block.Runs...))
			e.Doc.Blocks = append(e.Doc.Blocks[:lastTop], e.Doc.Blocks[lastTop+1:]...)
		}
	case !covered[last.Owner]:
		target = last.Block
	default:
		target = NewBlock("paragraph", "")
		target.Runs = nil
		at = min(at, len(e.Doc.Blocks))
		e.Doc.Blocks = append(append(append([]*Block(nil), e.Doc.Blocks[:at]...), target), e.Doc.Blocks[at:]...)
	}
	runs := target.Runs
	target.Runs = Compact(append(append(SliceRuns(runs, 0, offset), Run{Text: text, Style: style}), SliceRuns(runs, offset, Len(blockText(runs)))...))
	if len(e.Doc.Blocks) == 0 {
		e.Doc.Blocks = []*Block{NewBlock("paragraph", "")}
	}
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block == target {
			e.Anchor, e.Caret = leaf.Start+offset+Len(text), leaf.Start+offset+Len(text)
		}
	}
	e.Typing, e.Explicit = style, true
}

func blockText(runs []Run) string {
	b := Block{Runs: runs}
	return b.Text()
}
