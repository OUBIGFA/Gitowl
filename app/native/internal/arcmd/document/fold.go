package document

// Folding: a folded heading hides what follows it up to the next
// heading of its level or above; a folded list item or toggle hides the
// blocks after it indented deeper, which for a toggle are its content. Whether a block is folded is saved with the document
// but kept out of the undo history, so undo never folds or unfolds.

func isListKind(kind string) bool {
	return kind == "bullet" || kind == "numbered" || kind == "task"
}

// foldReach is how many blocks after blocks[i] it hides when folded.
func foldReach(blocks []*Block, i int) int {
	b := blocks[i]
	n := 0
	for _, next := range blocks[i+1:] {
		switch {
		case b.Kind == "heading":
			if next.Kind == "heading" && next.Level <= b.Level {
				return n
			}
		case isListKind(b.Kind) || b.Kind == "toggle":
			if next.Kind == "heading" || next.Indent <= b.Indent {
				return n
			}
		default:
			return 0
		}
		n++
	}
	return n
}

// siblingLists are the lists of blocks folding works in: the page and the
// columns of column layouts.
func siblingLists(d *Document) [][]*Block {
	out := [][]*Block{d.Blocks}
	for _, b := range d.Blocks {
		if b.Kind == "columns" {
			for _, column := range b.Children {
				out = append(out, column.Children)
			}
		}
	}
	return out
}

// CanFold reports whether block id can fold: a heading, or a list item with
// deeper items after it (or one already folded).
func (d *Document) CanFold(id string) bool {
	for _, list := range siblingLists(d) {
		for i, b := range list {
			if b.ID != id {
				continue
			}
			switch {
			case b.Kind == "heading" || b.Kind == "toggle":
				return true
			case isListKind(b.Kind):
				return b.Folded || i+1 < len(list) && list[i+1].Kind != "heading" && list[i+1].Indent > b.Indent
			}
			return false
		}
	}
	return false
}

// FoldableBlocks computes the fold controls once for a layout, rather than
// searching the complete document separately for every block.
func (d *Document) FoldableBlocks() map[string]bool {
	out := make(map[string]bool)
	for _, list := range siblingLists(d) {
		for i, b := range list {
			if b.Kind == "heading" || b.Kind == "toggle" || isListKind(b.Kind) && (b.Folded || i+1 < len(list) && list[i+1].Kind != "heading" && list[i+1].Indent > b.Indent) {
				out[b.ID] = true
			}
		}
	}
	return out
}

// HiddenBlocks are the blocks folded away, by ID, each with the folded
// block that hides it.
func (d *Document) HiddenBlocks() map[string]string {
	hidden := map[string]string{}
	for _, list := range siblingLists(d) {
		for i := 0; i < len(list); i++ {
			b := list[i]
			if !b.Folded {
				continue
			}
			n := foldReach(list, i)
			for _, h := range list[i+1 : i+1+n] {
				hidden[h.ID] = b.ID
				// What a hidden column layout holds is hidden with it.
				for _, column := range h.Children {
					for _, inner := range column.Children {
						hidden[inner.ID] = b.ID
					}
				}
			}
			i += n
		}
	}
	return hidden
}

// ToggleFold folds or unfolds block id. The change is saved but is not a
// step of the undo history.
func (e *Engine) ToggleFold(id string) {
	if !e.Doc.CanFold(id) {
		return
	}
	if parent, index := e.find(id); parent != nil {
		e.notifyBeforeChange()
		(*parent)[index].Folded = !(*parent)[index].Folded
		e.version++
		e.revision++
	}
}

// Reveal unfolds whatever hides block id, as when the caret or a search
// moves into a folded section. It reports whether anything unfolded.
func (e *Engine) Reveal(id string) bool {
	changed := false
	for {
		by, ok := e.hiddenBlocks()[id]
		if !ok {
			return changed
		}
		if parent, index := e.find(by); parent != nil {
			e.notifyBeforeChange()
			(*parent)[index].Folded = false
			e.version++
			e.revision++
			changed = true
		} else {
			return changed
		}
	}
}

// folds records which blocks are folded, to carry over undo and redo.
func (e *Engine) folds() map[string]bool {
	out := map[string]bool{}
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			if b.Folded {
				out[b.ID] = true
			}
			walk(b.Children)
		}
	}
	walk(e.Doc.Blocks)
	return out
}

// keepFolds puts back folds after undo or redo restored a document.
func (e *Engine) keepFolds(folded map[string]bool) {
	var walk func([]*Block)
	walk = func(blocks []*Block) {
		for _, b := range blocks {
			b.Folded = folded[b.ID] && (b.Kind == "heading" || isListKind(b.Kind) || b.Kind == "toggle")
			walk(b.Children)
		}
	}
	walk(e.Doc.Blocks)
}

// Text edits cannot change section visibility. Reuse the folded-block lookup
// while the caret moves or text changes; structural/fold revisions rebuild it.
func (e *Engine) hiddenBlocks() map[string]string {
	if e.depth > 0 {
		return e.Doc.HiddenBlocks()
	}
	if e.hiddenDoc != e.Doc || e.hiddenRevision != e.revision {
		e.hidden = e.Doc.HiddenBlocks()
		e.hiddenDoc, e.hiddenRevision = e.Doc, e.revision
	}
	return e.hidden
}
