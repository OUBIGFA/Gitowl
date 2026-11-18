package editor

import (
	"time"

	"github.com/egoist/mygo/ui"
)

type selectionDrag struct {
	moved   bool
	scrollY float32
	at      time.Time
}

func (e *Editor) extendSelection(x, y float32) {
	if at, ok := e.IndexForPoint(ui.Point{X: x, Y: y}); ok {
		e.SetSelection(e.Anchor, at)
	}
}

// stepSelectionDrag follows the pointer in viewport coordinates, which
// stay fixed while the document scrolls. Revealing the caret here would
// fight manual scrolling, especially when typewriter mode centers it.
func (e *Editor) stepSelectionDrag(c *ui.Context, scroll ui.Element, reserve float32) {
	if !e.dragging {
		return
	}
	d := &e.selectionDrag
	scrolled := e.Scroll.Y != d.scrollY
	d.moved = d.moved || scrolled
	dt := float32(min(max(c.Now().Sub(d.at), 0), 50*time.Millisecond).Seconds())
	d.at = c.Now()
	// A press alone must preserve word/line selection from multiple
	// clicks, and must not start scrolling when it happens near an edge.
	if !d.moved {
		return
	}
	x, y, _ := scroll.PointerPosition()
	band := min(float32(32), e.viewport/2)
	var speed float32
	if band > 0 {
		if y < band {
			speed = -480 * min(1, (band-y)/band)
		} else if y > e.viewport-band {
			speed = 480 * min(1, (y-e.viewport+band)/band)
		}
	}
	// Manual scrolling owns this frame. In particular, do not write an
	// offset that would cancel the scroll container's mouse-wheel glide.
	if speed != 0 {
		if !scrolled {
			e.Scroll.Y = min(max(0, e.Scroll.Y+speed*dt), e.Scroll.MaxY)
		}
		if speed < 0 && e.Scroll.Y > 0 || speed > 0 && e.Scroll.Y < e.Scroll.MaxY {
			c.After(time.Second / 60)
		}
	}
	d.scrollY = e.Scroll.Y
	before := e.Caret
	e.extendSelection(x-reserve, min(max(y, 0), e.viewport)+e.Scroll.Y)
	if e.Caret != before {
		// Inline and block math may switch their editing layout with the
		// selection; let the next frame update that layout too.
		c.After(0)
	}
}
