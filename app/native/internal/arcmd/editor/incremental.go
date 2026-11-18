package editor

import (
	"slices"
	"sort"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
)

// relayoutText reshapes one top-level block and translates the following
// geometry. Tables rebuild their own cells together; columns and missing
// revisions retain the complete-layout fallback.
func (e *Editor) relayoutText(o Options) bool {
	if e.Blank() != e.laidBlank {
		return false // the starter's room comes or goes
	}
	change, ok := e.TextChangeSince(e.cacheRevision)
	if !ok {
		return false
	}
	i, found := e.rowIDs[change.ID]
	if !found {
		return false
	}
	old := *e.indexedRow(i)
	leaf := e.Engine.LeafAt(change.Start)
	b := leaf.Block
	if b.ID != change.ID || old.leaf.Block.ID != change.ID {
		return false
	}
	if old.leaf.Owner != b.ID {
		return e.relayoutTable(o, change, i)
	}
	rows, decorations := e.rows, e.decorations
	e.rows, e.decorations = nil, nil
	layout := e.blockLayouter(o, func(string) document.Leaf { return leaf }, nil, true)
	layout(b, e.padding, old.rect.Y, max(80, o.Width-2*e.padding))
	updated, decorated := e.rows[0], e.decorations
	e.rows, e.decorations = rows, decorations
	updated.number = old.number
	updated.yApplied = e.rowYOff.query64(i)
	e.rows[i] = updated
	e.rowSpace.update(i, updated.rect)
	dy := updated.rect.H - old.rect.H
	decStart, decEnd := -1, -1
	if j, ok := e.decorationIDs[b.ID]; ok && len(decorated) > 0 {
		dec := decorated[0]
		dec.yApplied = e.decYOff.query64(j)
		e.decorations[j] = dec
		e.decorationSpace.update(j, dec.rect)
		decStart, decEnd = j, j+1
	}
	if b.Kind == "heading" {
		// The heading's identity and level did not change; only its label
		// and presence in the outline can change in a text transaction.
		at := slices.IndexFunc(e.outline, func(item outlineItem) bool { return item.id == b.ID })
		text := b.Text()
		switch {
		case at >= 0 && text == "":
			if e.hasLazyY {
				return false
			}
			e.outline = slices.Delete(e.outline, at, at+1)
		case at >= 0:
			e.outline[at].text = text
		case text != "":
			if e.hasLazyY {
				return false
			}
			at = sort.Search(len(e.outline), func(j int) bool { return e.outline[j].y >= old.rect.Y })
			e.outline = slices.Insert(e.outline, at, outlineItem{b.ID, text, b.Level, old.rect.Y, 0})
		}
	}
	handleShift := float32(0)
	if j, ok := e.handleIDs[b.ID]; ok {
		h := e.indexedHandle(j)
		caret := updated.layout.Caret(0)
		previous := h.rect.Y
		h.rect.Y = updated.y + caret.Y + max(caret.H, 20)/2 - 12
		handleShift = h.rect.Y - previous
	}
	e.shiftFollowing(i+1, old.rect.Y+old.rect.H, dy, decStart, decEnd, b.ID, handleShift)
	return true
}

// A cell can change the heights of other cells in its row, including merged
// cells. Reuse the full table layouter so those constraints stay identical,
// without walking or reshaping unrelated blocks in the document.
func (e *Editor) relayoutTable(o Options, change document.TextChange, rowIndex int) bool {
	owner := e.indexedRow(rowIndex).leaf.Owner
	table := e.blockByID(owner)
	if table == nil || table.Kind != "table" || e.resizePending || o.Resizing {
		return false
	}
	di, found := e.decorationIDs[owner]
	decEnd := di + 1
	if !found {
		return false
	}
	oldTable := *e.indexedDecoration(di)
	if oldTable.kind != "table" {
		return false
	}
	from, to := rowIndex, rowIndex+1
	for from > 0 && e.indexedRow(from-1).leaf.Owner == owner {
		from--
	}
	for to < len(e.rows) && e.indexedRow(to).leaf.Owner == owner {
		to++
	}
	leaves := make(map[string]document.Leaf, to-from)
	for k := from; k < to; k++ {
		leaf, ok := e.Engine.LeafByID(e.indexedRow(k).leaf.Block.ID)
		if !ok {
			return false
		}
		leaves[leaf.Block.ID] = leaf
	}
	rows, decorations, resizers := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = nil, nil, nil
	layout := e.blockLayouter(o, func(id string) document.Leaf { return leaves[id] }, nil, true)
	start := oldTable.rect.Y - 22
	height := layout(table, oldTable.rect.X, start, oldTable.rect.W)
	updated, decorated, handles := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = rows, decorations, resizers
	if len(updated) != to-from || len(decorated) == 0 {
		return false
	}
	// Text-only edits preserve cells, decorations and resize handles.
	for k := range updated {
		r := updated[k]
		r.yApplied = e.rowYOff.query64(from + k)
		e.rows[from+k] = r
		e.rowSpace.update(from+k, r.rect)
	}
	decStart := decEnd - len(decorated)
	for k := range decorated {
		dc := decorated[k]
		dc.yApplied = e.decYOff.query64(decStart + k)
		e.decorations[decStart+k] = dc
		e.decorationSpace.update(decStart+k, dc.rect)
	}
	next := 0
	for j := range e.resizers {
		if e.resizers[j].id == owner && next < len(handles) {
			rs := handles[next]
			rs.yApplied = e.resizerYOff.query64(j)
			e.resizers[j] = rs
			next++
		}
	}
	if e.shouldVirtualize() && len(table.Children) > 0 && len(table.Children[0].Children) > 0 {
		e.recordTableHeight(table, oldTable.rect.W, len(table.Children[0].Children), tableStamp(table), height-44)
	}
	oldHeight := oldTable.rect.H + 44
	e.shiftFollowing(to, start+oldHeight, height-oldHeight, decStart, decEnd, owner, 0)
	return true
}

func (e *Editor) shiftFollowing(after int, bottom, dy float32, decStart, decEnd int, owner string, handleShift float32) {
	if dy == 0 && handleShift == 0 {
		return
	}
	if e.hasLazyY {
		if hi, ok := e.handleIDs[owner]; ok && hi < len(e.handleEnds) {
			ends := e.handleEnds[hi]
			if handleShift != 0 && ends.foldEnd > 0 && ends.foldEnd <= len(e.foldButtons) && e.foldButtons[ends.foldEnd-1].id == owner {
				e.indexedFold(ends.foldEnd - 1).rect.Y += handleShift
			}
			if dy != 0 {
				e.rowYOff.addRange(ends.rowEnd, dy)
				e.rowSpace.shiftSuffix(ends.rowEnd, dy)
				e.decYOff.addRange(ends.decEnd, dy)
				e.decorationSpace.shiftSuffix(ends.decEnd, dy)
				e.handleYOff.addRange(hi+1, dy)
				e.foldYOff.addRange(ends.foldEnd, dy)
				e.resizerYOff.addRange(ends.resEnd, dy)
				e.outlineYOff.addRange(ends.outEnd, dy)
				e.height += dy
				e.contentEnd += dy
			}
			return
		}
	}
	for j := after; j < len(e.rows); j++ {
		r := e.indexedRow(j)
		r.rect.Y += dy
		r.y += dy
		if f := r.picture; f != nil {
			f.bounds.Y += dy
			for i := range f.actions {
				f.actions[i].bounds.Y += dy
			}
		}
	}
	e.rowSpace.refitSuffix(after)
	firstDecoration := len(e.decorations)
	for j := range e.decorations {
		if j >= decStart && j < decEnd {
			continue
		}
		d := e.indexedDecoration(j)
		if d.rect.Y >= bottom {
			firstDecoration = min(firstDecoration, j)
			d.rect.Y += dy
			if d.table.H > 0 {
				d.table.Y += dy
			}
			for k := range d.rows {
				d.rows[k] += dy
			}
		}
	}
	e.decorationSpace.refitSuffix(firstDecoration)
	for j := range e.handles {
		h := e.indexedHandle(j)
		if h.id != owner && h.top >= bottom {
			h.top += dy
			h.rect.Y += dy
		}
	}
	for j := range e.resizers {
		rs := e.indexedResizer(j)
		if rs.y >= bottom {
			rs.y += dy
		}
	}
	for j := range e.foldButtons {
		f := e.indexedFold(j)
		if f.id == owner {
			f.rect.Y += handleShift
		} else if f.rect.Y >= bottom {
			f.rect.Y += dy
		}
	}
	for j := range e.outline {
		item := e.indexedOutline(j)
		if item.y >= bottom {
			item.y += dy
		}
	}
	e.height += dy
	e.contentEnd += dy
}
