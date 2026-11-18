package editor

import "github.com/egoist/mygo/ui"

// paintDim fades every top-level block but the caret's, so the paragraph
// being written stands out, as focus modes do.
func (e *Editor) paintDim(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if !e.dim || e.printing || len(e.handles) == 0 {
		return
	}
	owner := e.Engine.LeafAt(e.Caret).Owner
	winTop, winBottom := e.motionWindow()
	hs, he := e.visibleHandleRange(winTop, winBottom)
	for offset := 0; offset < he-hs; offset++ {
		j := hs + offset
		h := *e.indexedHandle(j)
		if h.id == owner {
			continue
		}
		bottom := e.contentEnd
		if j+1 < len(e.handles) {
			bottom = e.indexedHandle(j + 1).top
		}
		dy := e.ownerVisualDY(h.id)
		if bottom+dy < e.Scroll.Y || h.top+dy > e.Scroll.Y+e.viewport {
			continue
		}
		glow := float32(0)
		if !e.reduceMotion && e.motion.dimGlow != nil {
			glow = e.motion.dimGlow[h.id]
		}
		alpha := float32(0.62) * (1 - glow)
		if alpha <= 0.005 {
			continue
		}
		// Up to the next block, the gap between them included, across the
		// whole page.
		p.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + h.top + dy - 4, W: e.width, H: bottom - h.top}, t.Background.Alpha(alpha), 0)
	}
}
