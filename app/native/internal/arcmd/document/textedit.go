package document

import (
	"slices"
	"strings"
)

// TextChange describes one text-only edit without changing the block tree.
// Consumers may update their caches only if they observed the previous revision.
type TextChange struct {
	ID                         string
	Before, After              string
	Start, BeforeEnd, AfterEnd int
	Revision                   uint64
}

func (e *Engine) TextChangeSince(revision uint64) (TextChange, bool) {
	c := e.textChange
	return c, c.Revision == e.revision && revision+1 == c.Revision && c.ID != ""
}

// Text-only history owns a copy of the affected runs. Structural transactions
// retain full snapshots; neither representation aliases mutable document data.
type historyEntry struct {
	state   Snapshot
	blockID string
	runs    []Run
}

func (e *Engine) recordUndo(entry historyEntry) {
	e.undo = append(e.undo, entry)
	if len(e.undo) > 200 {
		e.undo[0] = historyEntry{}
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *Engine) captureHistory(id string) historyEntry {
	if id != "" {
		if leaf, ok := e.LeafByID(id); ok {
			return historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, id, slices.Clone(leaf.Block.Runs)}
		}
	}
	return historyEntry{state: e.Capture()}
}

func (e *Engine) restoreHistory(entry historyEntry) {
	if entry.blockID == "" {
		folded := e.folds()
		e.Restore(entry.state)
		e.keepFolds(folded)
		return
	}
	// Undoing later structural entries has already restored this block.
	e.ensureIndex()
	i, ok := e.leafIDs[entry.blockID]
	if !ok {
		panic("document: text history lost its block")
	}
	leaf := e.leafAtIndex(i)
	before := leaf.Block.Text()
	leaf.Block.Runs = slices.Clone(entry.runs)
	e.publishTextChange(i, before, leaf.Block.Text())
	e.Anchor, e.Caret, e.Typing, e.Explicit = entry.state.Anchor, entry.state.Caret, entry.state.Typing, entry.state.Explicit
}

// LeafState captures the mutable state of a single leaf block and selection so
// IME composition inside one paragraph can avoid cloning unrelated blocks.
type LeafState struct {
	BlockID       string
	Runs          []Run
	Anchor, Caret int
	Typing        Style
	Explicit      bool
}

// replaceText handles changes entirely inside one leaf, preserving the existing
// transaction/selection semantics without copying unrelated blocks for undo.
func (e *Engine) replaceText(a, z int, text string) bool {
	return e.replaceTextWithOptions(a, z, text, true, true)
}

func (e *Engine) replaceTextLiteral(a, z int, text string) bool {
	return e.replaceTextWithOptions(a, z, text, true, false)
}

func (e *Engine) replaceTextMode(a, z int, text string, committed bool) bool {
	return e.replaceTextWithOptions(a, z, text, committed, committed)
}

func (e *Engine) replaceTextWithOptions(a, z int, text string, committed, allowAutoformat bool) bool {
	if e.depth != 0 || strings.ContainsAny(text, "\r\n") {
		return false
	}
	a, z = max(0, min(a, z)), max(0, max(a, z))
	first := e.LeafAt(a)
	if z > first.End || a < first.Start {
		return false
	}
	b := first.Block
	before := b.Text()
	if strings.ContainsRune(before, '\n') && b.Kind != "code" && b.Kind != "cell" && b.Kind != "raw" && b.Kind != "math" {
		return false
	}
	a = first.Start + Len(before[:ByteOffset(before, a-first.Start)])
	z = first.Start + Len(before[:ByteOffset(before, z-first.Start)])
	style := e.CurrentStyle()
	style.NoMath = false
	runs := Compact(append(append(SliceRuns(b.Runs, 0, a-first.Start), Run{Text: text, Style: style}), SliceRuns(b.Runs, z-first.Start, first.End-first.Start)...))
	// Short Markdown prefixes at the start of a block still take the structural transaction path.
	if allowAutoformat && text == " " && (b.Kind == "paragraph" || b.Kind == "bullet") && a-first.Start <= 9 {
		return false
	}
	var entry historyEntry
	if committed {
		entry = historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, b.ID, slices.Clone(b.Runs)}
	}
	e.Anchor, e.Caret = a+Len(text), a+Len(text)
	e.Typing, e.Explicit = style, true
	if slices.Equal(b.Runs, runs) {
		return true
	}
	e.notifyBeforeChange()
	i := e.leafIDs[first.Block.ID]
	b.Runs = runs
	if committed {
		e.recordUndo(entry)
		e.version++
	}
	e.publishTextChange(i, before, b.Text())
	return true
}

// CaptureLeaf records one leaf's pre-composition state if [a, z) lies inside a
// single leaf without unsplit paragraph newlines.
func (e *Engine) CaptureLeaf(a, z int) (LeafState, bool) {
	if e.ReadOnly || e.depth != 0 {
		return LeafState{}, false
	}
	a, z = max(0, min(a, z)), max(0, max(a, z))
	first := e.LeafAt(a)
	if z > first.End || a < first.Start {
		return LeafState{}, false
	}
	b := first.Block
	if b.Kind != "code" && b.Kind != "cell" && b.Kind != "raw" && b.Kind != "math" && strings.ContainsRune(b.Text(), '\n') {
		return LeafState{}, false
	}
	return LeafState{
		BlockID:  b.ID,
		Runs:     slices.Clone(b.Runs),
		Anchor:   e.Anchor,
		Caret:    e.Caret,
		Typing:   e.Typing,
		Explicit: e.Explicit,
	}, true
}

// PreviewLeafReplace updates one leaf in place during IME preedit without
// cloning unrelated blocks or triggering Markdown prefix autoformatting.
func (e *Engine) PreviewLeafReplace(a, z int, text string) bool {
	if e.ReadOnly {
		return true
	}
	return e.replaceTextWithOptions(a, z, text, false, false)
}

// RestoreLeaf restores a single leaf captured by CaptureLeaf without cloning the
// rest of the document and publishes a text change for incremental relayout.
func (e *Engine) RestoreLeaf(s LeafState) bool {
	e.ensureIndex()
	i, ok := e.leafIDs[s.BlockID]
	if !ok {
		return false
	}
	e.notifyBeforeChange()
	leaf := e.leafAtIndex(i)
	before := leaf.Block.Text()
	leaf.Block.Runs = slices.Clone(s.Runs)
	e.Anchor, e.Caret = s.Anchor, s.Caret
	e.Typing, e.Explicit = s.Typing, s.Explicit
	e.publishTextChange(i, before, leaf.Block.Text())
	return true
}

// CommitLeaf commits a single-leaf IME replacement against the pre-composition
// LeafState without cloning the document, returning false when structural work
// (such as newlines, multi-block ranges, or Markdown prefix autoformat) is needed.
func (e *Engine) CommitLeaf(s LeafState, a, z int, text string) bool {
	if e.ReadOnly {
		return true
	}
	if e.depth != 0 || strings.ContainsAny(text, "\r\n") {
		return false
	}
	e.ensureIndex()
	i, ok := e.leafIDs[s.BlockID]
	if !ok {
		return false
	}
	leaf := e.leafAtIndex(i)
	a, z = max(0, min(a, z)), max(0, max(a, z))
	if z > leaf.End || a < leaf.Start {
		return false
	}
	b := leaf.Block
	before := b.Text()
	if strings.ContainsRune(before, '\n') && b.Kind != "code" && b.Kind != "cell" && b.Kind != "raw" && b.Kind != "math" {
		return false
	}
	a = leaf.Start + Len(before[:ByteOffset(before, a-leaf.Start)])
	z = leaf.Start + Len(before[:ByteOffset(before, z-leaf.Start)])
	if text == " " && (b.Kind == "paragraph" || b.Kind == "bullet") && a-leaf.Start <= 9 {
		return false
	}
	style := e.CurrentStyle()
	style.NoMath = false
	runs := Compact(append(append(SliceRuns(b.Runs, 0, a-leaf.Start), Run{Text: text, Style: style}), SliceRuns(b.Runs, z-leaf.Start, leaf.End-leaf.Start)...))
	e.Anchor, e.Caret = a+Len(text), a+Len(text)
	e.Typing, e.Explicit = style, true

	netChanged := !slices.Equal(s.Runs, runs)
	previewChanged := !slices.Equal(b.Runs, runs)
	if netChanged || previewChanged {
		e.notifyBeforeChange()
	}
	if netChanged {
		entry := historyEntry{Snapshot{Anchor: s.Anchor, Caret: s.Caret, Typing: s.Typing, Explicit: s.Explicit}, s.BlockID, slices.Clone(s.Runs)}
		e.recordUndo(entry)
		e.version++
	}
	if previewChanged {
		b.Runs = runs
	}
	if previewChanged || netChanged {
		// Even when UnmarkText commits identical preview text (netChanged && !previewChanged),
		// publish a revision bump so caches watching Composing()/dirty() exit preedit cleanly.
		e.publishTextChange(i, before, b.Text())
	}
	return true
}

// ReplaceBlockRuns replaces one leaf block's runs atomically without cloning
// unrelated blocks, publishing a single-leaf TextChange and undo entry.
func (e *Engine) ReplaceBlockRuns(id string, runs []Run, anchor, caret int, typing Style, explicit bool) bool {
	if e.ReadOnly || e.depth != 0 {
		return false
	}
	e.ensureIndex()
	i, ok := e.leafIDs[id]
	if !ok {
		return false
	}
	leaf := e.leafAtIndex(i)
	b := leaf.Block
	next := Compact(runs)
	if !slices.Equal(b.Runs, next) {
		e.notifyBeforeChange()
		before := b.Text()
		entry := historyEntry{Snapshot{Anchor: e.Anchor, Caret: e.Caret, Typing: e.Typing, Explicit: e.Explicit}, id, slices.Clone(b.Runs)}
		b.Runs = next
		e.recordUndo(entry)
		e.version++
		e.publishTextChange(i, before, b.Text())
	}
	e.Anchor, e.Caret = anchor, caret
	e.Typing, e.Explicit = typing, explicit
	return true
}

func (e *Engine) publishTextChange(index int, before, after string) {
	leaf := e.leafAtIndex(index)
	delta := Len(after) - (leaf.End - leaf.Start)
	byteDelta := len(after) - len(before)
	if e.hiddenDoc == e.Doc && e.hiddenRevision == e.revision {
		e.hiddenRevision++
	}
	e.revision++
	e.textChange = TextChange{leaf.Block.ID, before, after, leaf.Start, leaf.End, leaf.End + delta, e.revision}
	e.leaves[index].End += delta
	e.leaves[index].ByteEnd += byteDelta
	e.unitIndex.add(index, delta)
	e.byteIndex.add(index, byteDelta)
	e.indexDoc, e.indexRevision = e.Doc, e.revision
}
