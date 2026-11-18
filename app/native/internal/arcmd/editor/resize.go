package editor

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// MyGo exposes the window's size but no live-resize begin/end state. A short
// quiet period coalesces native resize events, including frames between them.
// Schedule the final exact layout even if the user releases without more input.
const windowResizeQuiet = 120 * time.Millisecond

// Finishing a long document in one frame blocks new resize events for over
// 100 ms. Bound offscreen work per frame; visible text is never budgeted.
const resizeBatchShapes = 32

type windowResize struct {
	width   float32
	until   time.Time
	pending bool
}

func (e *Editor) layoutForView(c *ui.Context, o Options) {
	e.resizeBatch = e.windowResize.pending && e.resizePending && !o.Resizing
	e.resizeRemaining = 0
	if e.resizeBatch {
		e.resizeRemaining = resizeBatchShapes
		o.Resizing = true
	}
	batch := e.resizeBatch
	e.layout(o)
	e.resizeBatch, e.resizeRemaining = false, 0
	// Reflow above the reading anchor changes its absolute scroll offset.
	// Refresh the viewport at that new offset before painting, without doing
	// any more offscreen work. The latest width always wins if input resumes.
	if o.Resizing && e.resizePending && e.resizeScroll != e.Scroll.Y {
		e.layout(o)
	}
	e.materializeViewport(o)
	if !e.resizePending {
		e.windowResize.pending = false
	} else if batch {
		c.After(8 * time.Millisecond)
	}
}

func (r *windowResize) active(c *ui.Context) bool {
	width, _ := c.Size()
	now := c.Now()
	if r.width > 0 && width != r.width {
		r.until = now.Add(windowResizeQuiet)
	}
	r.width = width
	if now.Before(r.until) {
		c.After(r.until.Sub(now))
		return true
	}
	return false
}

// While a host resizes the viewport, retain the cached shaping of paragraphs
// well outside it. Visible paragraphs still wrap at the exact current width.
// Native window resizing finishes in bounded batches after release; explicit
// host resizes finish at once. Both preserve the reading anchor.
func (e *Editor) deferResizeShape(o Options, y, height float32) bool {
	if !o.Resizing || e.viewport <= 0 {
		return false
	}
	overscan := e.viewport / 2
	offscreen := y+height < e.Scroll.Y-overscan || y > e.Scroll.Y+e.viewport+overscan
	if offscreen && e.resizeBatch && e.resizeRemaining > 0 {
		e.resizeRemaining--
		return false
	}
	return offscreen
}

type resizeAnchor struct {
	id     string
	offset float32
}

func (e *Editor) resizeAnchor() resizeAnchor {
	if e.Scroll.Y <= 0 {
		return resizeAnchor{}
	}
	for i := range e.rowSpace.between(e.Scroll.Y+0.01, max(e.Scroll.Y+1, e.height+1000)) {
		r := e.indexedRow(i)
		if r.rect.Y+r.rect.H > e.Scroll.Y {
			return resizeAnchor{r.leaf.Block.ID, e.Scroll.Y - r.rect.Y}
		}
	}
	return resizeAnchor{}
}

func (e *Editor) restoreResizeAnchor(anchor resizeAnchor) {
	if anchor.id == "" {
		return
	}
	if idx, ok := e.rowIDs[anchor.id]; ok {
		r := e.indexedRow(idx)
		nextY := max(0, r.rect.Y+min(anchor.offset, max(0, r.rect.H-1)))
		dy := nextY - e.Scroll.Y
		e.Scroll.Y = nextY
		if e.scrollGlide.active && abs32(dy) > 0.01 {
			e.scrollGlide.to = max(0, e.scrollGlide.to+dy)
			e.scrollGlide.lastY = max(0, e.scrollGlide.lastY+dy)
		}
		return
	}
	for i := range e.rows {
		r := e.indexedRow(i)
		if r.leaf.Block.ID == anchor.id {
			nextY := max(0, r.rect.Y+min(anchor.offset, max(0, r.rect.H-1)))
			dy := nextY - e.Scroll.Y
			e.Scroll.Y = nextY
			if e.scrollGlide.active && abs32(dy) > 0.01 {
				e.scrollGlide.to = max(0, e.scrollGlide.to+dy)
				e.scrollGlide.lastY = max(0, e.scrollGlide.lastY+dy)
			}
			return
		}
	}
}
