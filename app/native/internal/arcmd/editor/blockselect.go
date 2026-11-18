package editor

// Block selection, as Notion's: whole top-level blocks selected together,
// which move, change type, indent, copy and delete as one. Escape selects
// the caret's block; ⇧↑ / ⇧↓ and ⇧-clicking a block's handle extend it;
// dragging down the page's left margin selects the blocks the pointer
// passes. While blocks are selected the caret hides and keys act on them.

import (
	"fmt"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// blockSelection is the selected top-level blocks, in page order, and the
// block a range grows from.
type blockSelection struct {
	ids    []string
	anchor string
	// marquee is a drag down the left margin, from y0.
	marquee bool
	y0      float32
	// pending is a range to select once the next layout has its blocks;
	// told how many blocks screen readers last heard were selected.
	pending [2]string
	told    int
}

// announceBlocks tells screen readers how many blocks are selected when
// that changes, as the page draws no caret for them to follow.
func (e *Editor) announceBlocks(c *ui.Context) {
	if n := len(e.blocks.ids); n != e.blocks.told {
		e.blocks.told = n
		if n > 0 {
			c.Announce(fmt.Sprintf(i18n.T("已选 %d 个内容块"), n))
		}
	}
}

// BlockSelection is the IDs of the selected blocks, in page order.
func (e *Editor) BlockSelection() []string { return append([]string(nil), e.blocks.ids...) }

// blockSelected reports whether top-level block id is selected.
func (e *Editor) blockSelected(id string) bool {
	for _, s := range e.blocks.ids {
		if s == id {
			return true
		}
	}
	return false
}

// clearBlocks ends the block selection.
func (e *Editor) clearBlocks() {
	e.blocks.ids, e.blocks.anchor = nil, ""
}

// handleIndex is the position of id among the visible top-level blocks.
func (e *Editor) handleIndex(id string) int {
	if i, ok := e.handleIDs[id]; ok {
		return i
	}
	return -1
}

// SelectBlocks selects the visible top-level blocks from from to to,
// either way round, with from as the anchor.
func (e *Editor) SelectBlocks(from, to string) {
	a, z := e.handleIndex(from), e.handleIndex(to)
	if a < 0 || z < 0 {
		return
	}
	e.blocks.anchor = from
	e.blocks.ids = nil
	for i := min(a, z); i <= max(a, z); i++ {
		e.blocks.ids = append(e.blocks.ids, e.handles[i].id)
	}
	e.UnmarkText()
	e.menuOpen, e.slashOpen, e.markPop.open, e.annotatePanel = false, false, false, false
}

// SelectCaretBlock selects the top-level block holding the caret.
func (e *Editor) SelectCaretBlock() bool {
	owner := e.Engine.LeafAt(e.Caret).Owner
	if e.handleIndex(owner) < 0 {
		return false
	}
	e.SelectBlocks(owner, owner)
	return true
}

// SelectAllBlocks selects every visible top-level block.
func (e *Editor) SelectAllBlocks() {
	if len(e.handles) > 0 {
		e.SelectBlocks(e.handles[0].id, e.handles[len(e.handles)-1].id)
	}
}

// pruneBlocks drops selected blocks no longer on the page or folded away.
func (e *Editor) pruneBlocks() {
	if p := e.blocks.pending; p[0] != "" {
		e.blocks.pending = [2]string{}
		e.SelectBlocks(p[0], p[1])
	}
	if len(e.blocks.ids) == 0 {
		return
	}
	var kept []string
	for _, h := range e.handles {
		if e.blockSelected(h.id) {
			kept = append(kept, h.id)
		}
	}
	e.blocks.ids = kept
	if len(kept) == 0 || e.handleIndex(e.blocks.anchor) < 0 {
		if len(kept) == 0 {
			e.clearBlocks()
		} else {
			e.blocks.anchor = kept[0]
		}
	}
}

// selectionEnds are the first and last selected blocks' positions.
func (e *Editor) selectionEnds() (int, int) {
	return e.handleIndex(e.blocks.ids[0]), e.handleIndex(e.blocks.ids[len(e.blocks.ids)-1])
}

// blockKeys handles a key while blocks are selected; any key it does not
// take ends the selection and goes on as usual.
func (e *Editor) blockKeys(ev ui.InputEvent, write func(string)) bool {
	if len(e.blocks.ids) == 0 {
		return false
	}
	mods, key := ev.Mods, ev.Key
	ids := e.BlockSelection()
	first, last := e.selectionEnds()
	focusEnd := func() {
		for _, leaf := range e.Doc.Leaves() {
			if leaf.Owner == ids[len(ids)-1] {
				e.SetSelection(leaf.End, leaf.End)
			}
		}
	}
	edit := func(run func()) bool {
		if e.ReadOnly {
			e.refused = true
			return true
		}
		run()
		e.sync()
		e.reveal = true
		return true
	}
	switch {
	case key == ui.KeyEscape && mods == 0:
		e.clearBlocks()
		return true
	case (key == ui.KeyBackspace || key == ui.KeyDelete) && mods == 0:
		return edit(func() { e.Engine.DeleteBlocks(ids); e.clearBlocks() })
	case key == ui.KeyEnter && mods == 0:
		e.clearBlocks()
		focusEnd()
		e.reveal = true
		return true
	case key == ui.KeyTab && mods&^ui.Shift == 0:
		delta := 1
		if mods&ui.Shift != 0 {
			delta = -1
		}
		return edit(func() { e.Engine.IndentBlocks(ids, delta) })
	case (key == ui.KeyUp || key == ui.KeyDown) && mods == ui.Cmd|ui.Shift:
		delta := 1
		if key == ui.KeyUp {
			delta = -1
		}
		return edit(func() { e.Engine.ShiftBlocks(ids, delta) })
	case (key == ui.KeyUp || key == ui.KeyDown) && mods == ui.Shift:
		// The end away from the anchor moves.
		anchor := e.handleIndex(e.blocks.anchor)
		end := first
		if anchor == first {
			end = last
		}
		if key == ui.KeyUp {
			end = max(0, end-1)
		} else {
			end = min(len(e.handles)-1, end+1)
		}
		e.SelectBlocks(e.blocks.anchor, e.handles[end].id)
		return true
	case (key == ui.KeyUp || key == ui.KeyDown) && mods == 0:
		at := first
		if key == ui.KeyUp {
			at = max(0, first-1)
		} else {
			at = min(len(e.handles)-1, last+1)
		}
		e.SelectBlocks(e.handles[at].id, e.handles[at].id)
		return true
	case key == ui.KeyA && mods == ui.Cmd && len(e.handles) > 0:
		e.SelectBlocks(e.handles[0].id, e.handles[len(e.handles)-1].id)
		return true
	case key == ui.KeyC && mods == ui.Cmd:
		e.copyBlocks(ids, write)
		return true
	case key == ui.KeyX && mods == ui.Cmd:
		if e.ReadOnly {
			e.copyBlocks(ids, write)
			e.refused = true
			return true
		}
		e.copyBlocks(ids, write)
		return edit(func() { e.Engine.DeleteBlocks(ids); e.clearBlocks() })
	case mods == ui.Cmd|ui.Alt:
		for i, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6} {
			if key == k {
				kind := "heading"
				if i == 0 {
					kind = "paragraph"
				}
				return edit(func() { e.Engine.SetBlocksKind(ids, kind, i) })
			}
		}
	}
	if key == ui.KeyUnknown {
		return true
	}
	e.clearBlocks()
	return false
}

// copyBlocks puts the blocks ids on the clipboard, whole.
func (e *Editor) copyBlocks(ids []string, write func(string)) {
	e.writeFragment(e.Engine.BlocksFragment(ids), "", write)
}

// blockPointer starts and drives a drag down the page's left margin,
// which selects the blocks it passes, and extends the selection to a block
// whose handle is ⇧-clicked.
func (e *Editor) blockPointer(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if ev.Mods == ui.Shift {
			for _, h := range e.handles {
				if ev.X >= h.rect.X && ev.X <= h.rect.X+h.rect.W && ev.Y >= h.rect.Y && ev.Y <= h.rect.Y+h.rect.H {
					from := e.blocks.anchor
					if from == "" {
						from = e.Engine.LeafAt(e.Caret).Owner
					}
					if e.handleIndex(from) < 0 {
						from = h.id
					}
					e.SelectBlocks(from, h.id)
					return true
				}
			}
		}
		// The margin left of the handles, beside the page's blocks.
		if ev.Mods == 0 && len(e.handles) > 0 && ev.X < e.padding-60 && ev.Y >= e.indexedHandle(0).top-8 && ev.Y <= e.contentEnd {
			e.blocks.marquee, e.blocks.y0 = true, ev.Y
			e.marqueeTo(ev.Y)
			return true
		}
	case ui.InputPointerMove:
		if e.blocks.marquee {
			e.marqueeTo(min(max(ev.Y, e.Scroll.Y), e.Scroll.Y+e.viewport))
			return true
		}
	case ui.InputPointerUp:
		if e.blocks.marquee {
			e.blocks.marquee = false
			return true
		}
	}
	return false
}

// marqueeTo selects the blocks between where the margin drag began and y.
func (e *Editor) marqueeTo(y float32) {
	top, bottom := min(e.blocks.y0, y), max(e.blocks.y0, y)
	from, to := -1, -1
	for j := range e.handles {
		h := *e.indexedHandle(j)
		end := e.contentEnd
		if j+1 < len(e.handles) {
			end = e.indexedHandle(j + 1).top
		}
		if end >= top && h.top <= bottom {
			if from < 0 {
				from = j
			}
			to = j
		}
	}
	if from < 0 {
		e.clearBlocks()
		return
	}
	anchor, end := from, to
	if y < e.blocks.y0 {
		anchor, end = to, from
	}
	e.SelectBlocks(e.handles[anchor].id, e.handles[end].id)
}

// paintBlockSelection fills the selected blocks, which are always one run,
// as one band under their text.
func (e *Editor) paintBlockSelection(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if len(e.blocks.ids) == 0 || e.printing {
		return
	}
	first, last := e.selectionEnds()
	if first < 0 || last < 0 {
		return
	}
	top := e.indexedHandle(first).top
	bottom := e.contentEnd
	if last+1 < len(e.handles) {
		bottom = e.indexedHandle(last + 1).top
	}
	p.Fill(ui.Rect{X: bounds.X + e.padding - 8, Y: bounds.Y + top - 4, W: e.width - 2*e.padding + 16, H: bottom - top}, t.Selection, 6)
}

// writeFragment puts a fragment on the clipboard as ArcMD's structure,
// Markdown, HTML and text; text "" is the fragment's Markdown.
func (e *Editor) writeFragment(fragment *document.Document, text string, write func(string)) {
	fragment.ImportSource = ""
	data, err := fragment.Encode()
	if err != nil {
		if text == "" {
			text = ExportMarkdown(fragment)
		}
		write(text)
		return
	}
	if text == "" {
		text = trimNewline(ExportMarkdown(fragment))
	}
	if e.services == (ui.Services{}) {
		write(text)
		return
	}
	e.writeClipboard(fragment, data, text)
}
