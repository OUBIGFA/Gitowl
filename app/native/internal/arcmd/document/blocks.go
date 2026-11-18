package document

// Operations on several top-level blocks at once, as a block selection
// makes them: each is one step of the history. IDs not at the top level are
// ignored, and the blocks act in page order whatever order the IDs come in.

// topSet is the top-level blocks of ids, with what folded ones among them
// hide, in page order, as indexes into Doc.Blocks.
func (e *Engine) topSet(ids []string) []int {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	hidden := e.Doc.HiddenBlocks()
	var out []int
	for i, b := range e.Doc.Blocks {
		if want[b.ID] || want[hidden[b.ID]] {
			out = append(out, i)
		}
	}
	return out
}

// exactSet is the top-level blocks of ids alone, in page order.
func (e *Engine) exactSet(ids []string) []int {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int
	for i, b := range e.Doc.Blocks {
		if want[b.ID] {
			out = append(out, i)
		}
	}
	return out
}

// BlocksFragment copies the top-level blocks ids, whole, for the clipboard.
func (e *Engine) BlocksFragment(ids []string) *Document {
	out := New()
	out.Blocks = nil
	for _, i := range e.topSet(ids) {
		out.Blocks = append(out.Blocks, e.Doc.Blocks[i].Clone())
	}
	if len(out.Blocks) == 0 {
		out.Blocks = []*Block{NewBlock("paragraph", "")}
	}
	return out
}

// DeleteBlocks removes the top-level blocks ids; the caret goes to the
// block after them, or before them at the end of the page.
func (e *Engine) DeleteBlocks(ids []string) {
	set := e.topSet(ids)
	if len(set) == 0 {
		return
	}
	e.Edit(func() {
		gone := map[int]bool{}
		for _, i := range set {
			gone[i] = true
		}
		var kept []*Block
		next := ""
		for i, b := range e.Doc.Blocks {
			if gone[i] {
				continue
			}
			if i > set[0] && next == "" {
				next = b.ID
			}
			kept = append(kept, b)
		}
		if len(kept) == 0 {
			kept = []*Block{NewBlock("paragraph", "")}
		}
		if next == "" {
			next = kept[len(kept)-1].ID
		}
		e.Doc.Blocks = kept
		e.FocusBlock(next)
	})
}

// MoveBlocks moves the top-level blocks ids together, keeping their order,
// to go before the block now at index to (len for the end).
func (e *Engine) MoveBlocks(ids []string, to int) {
	set := e.topSet(ids)
	if len(set) == 0 {
		return
	}
	e.Edit(func() {
		moving := map[int]bool{}
		var group []*Block
		for _, i := range set {
			moving[i] = true
			group = append(group, e.Doc.Blocks[i])
		}
		to = max(0, min(to, len(e.Doc.Blocks)))
		var rest []*Block
		at := -1
		for i, b := range e.Doc.Blocks {
			if i == to && at < 0 {
				at = len(rest)
			}
			if !moving[i] {
				rest = append(rest, b)
			}
		}
		if at < 0 {
			at = len(rest)
		}
		e.Doc.Blocks = append(append(append([]*Block(nil), rest[:at]...), group...), rest[at:]...)
		e.FocusBlock(group[0].ID)
	})
}

// ShiftBlocks moves the top-level blocks ids one place up (delta -1) or
// down (1) past the block beside them.
func (e *Engine) ShiftBlocks(ids []string, delta int) {
	set := e.topSet(ids)
	if len(set) == 0 {
		return
	}
	if delta < 0 {
		if set[0] == 0 {
			return
		}
		e.MoveBlocks(ids, set[0]-1)
		return
	}
	last := set[len(set)-1]
	if last+1 >= len(e.Doc.Blocks) {
		return
	}
	e.MoveBlocks(ids, last+2)
}

// SetBlocksKind turns the text blocks among ids into kind (at level, for a
// heading). Tables, columns, images, rules and formulas stay what they are.
func (e *Engine) SetBlocksKind(ids []string, kind string, level int) {
	// Only the blocks named: what a folded one hides keeps its kind.
	set := e.exactSet(ids)
	e.Edit(func() {
		for _, i := range set {
			b := e.Doc.Blocks[i]
			switch b.Kind {
			case "paragraph", "heading", "quote", "bullet", "numbered", "task", "code", "callout", "toggle":
			default:
				continue
			}
			if kind == "code" && b.Kind == "callout" || kind == "callout" && b.Kind == "code" {
				continue
			}
			b.Kind, b.Level = kind, level
			if kind != "heading" {
				b.Level = 0
			}
		}
	})
}

// IndentBlocks indents (delta 1) or outdents (-1) the blocks ids.
func (e *Engine) IndentBlocks(ids []string, delta int) {
	set := e.topSet(ids)
	e.Edit(func() {
		for _, i := range set {
			b := e.Doc.Blocks[i]
			b.Indent = max(0, min(12, b.Indent+delta))
		}
	})
}

// DuplicateBlocks puts a copy of the blocks ids after the last of them,
// with fresh IDs, and returns the copies' IDs.
func (e *Engine) DuplicateBlocks(ids []string) []string {
	set := e.topSet(ids)
	if len(set) == 0 {
		return nil
	}
	var copies []*Block
	var out []string
	for _, i := range set {
		b := e.Doc.Blocks[i].Clone()
		freshIDs(b)
		copies = append(copies, b)
		out = append(out, b.ID)
	}
	e.Edit(func() {
		at := set[len(set)-1] + 1
		e.Doc.Blocks = append(append(append([]*Block(nil), e.Doc.Blocks[:at]...), copies...), e.Doc.Blocks[at:]...)
		e.FocusBlock(copies[0].ID)
	})
	return out
}

// freshIDs gives b and everything it holds new IDs.
func freshIDs(b *Block) {
	b.ID = NewID()
	for _, child := range b.Children {
		freshIDs(child)
	}
}
