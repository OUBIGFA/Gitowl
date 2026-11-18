package editor

// fenwick64 maintains suffix range-additions in O(log N) over float64 accumulators
// so height changes in virtualized documents update following rows, handles,
// decorations, folds, resizers, and outlines without linear scans.
type fenwick64 struct {
	tree []float64
}

func (f *fenwick64) reset(n int) {
	if n <= 0 {
		f.tree = f.tree[:0]
		return
	}
	need := n + 1
	if cap(f.tree) < need {
		f.tree = make([]float64, need)
	} else {
		f.tree = f.tree[:need]
		clear(f.tree)
	}
}

// addRange adds delta to all elements i >= first in O(log N).
func (f *fenwick64) addRange(first int, delta float32) {
	if delta == 0 || len(f.tree) <= 1 || first >= len(f.tree)-1 {
		return
	}
	if first < 0 {
		first = 0
	}
	d := float64(delta)
	for idx := first + 1; idx < len(f.tree); idx += idx & -idx {
		f.tree[idx] += d
	}
}

// query64 returns the accumulated Y delta at index i in O(log N).
func (f *fenwick64) query64(i int) float64 {
	if len(f.tree) <= 1 || i < 0 {
		return 0
	}
	idx := min(i+1, len(f.tree)-1)
	var sum float64
	for ; idx > 0; idx -= idx & -idx {
		sum += f.tree[idx]
	}
	return sum
}

func (e *Editor) indexedDecoration(i int) *decoration {
	d := &e.decorations[i]
	if e.hasLazyY {
		if off := e.decYOff.query64(i); off != d.yApplied {
			dy := float32(off - d.yApplied)
			d.rect.Y += dy
			if d.table.H > 0 {
				d.table.Y += dy
			}
			for k := range d.rows {
				d.rows[k] += dy
			}
			d.yApplied = off
		}
	}
	return d
}

func (e *Editor) indexedHandle(i int) *blockHandle {
	h := &e.handles[i]
	if e.hasLazyY {
		if off := e.handleYOff.query64(i); off != h.yApplied {
			dy := float32(off - h.yApplied)
			h.top += dy
			h.rect.Y += dy
			h.yApplied = off
		}
	}
	return h
}

func (e *Editor) indexedFold(i int) *foldButton {
	f := &e.foldButtons[i]
	if e.hasLazyY {
		if off := e.foldYOff.query64(i); off != f.yApplied {
			f.rect.Y += float32(off - f.yApplied)
			f.yApplied = off
		}
	}
	return f
}

func (e *Editor) indexedResizer(i int) *columnHandle {
	r := &e.resizers[i]
	if e.hasLazyY {
		if off := e.resizerYOff.query64(i); off != r.yApplied {
			r.y += float32(off - r.yApplied)
			r.yApplied = off
		}
	}
	return r
}

func (e *Editor) indexedOutline(i int) *outlineItem {
	item := &e.outline[i]
	if e.hasLazyY {
		if off := e.outlineYOff.query64(i); off != item.yApplied {
			item.y += float32(off - item.yApplied)
			item.yApplied = off
		}
	}
	return item
}
