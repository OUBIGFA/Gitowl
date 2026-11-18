package editor

import (
	"sort"

	"github.com/egoist/mygo/ui"
)

// Printing lays the document out on paper: Paginate breaks it into pages
// at the lines of its text, never through one, and PaintPage draws a page
// without the caret, hints or controls. The host renders the pages
// (ui.Render) and writes them out.

// Paginate lays the document out o.Width wide and returns the tops of its
// pages, each at most height tall, in the page's coordinates.
func (e *Editor) Paginate(o Options, height float32) []float32 {
	e.printing = true
	e.ready = false
	e.mathEditing, e.mermaidEditing, e.inlineEditing = "", "", ""
	e.page = pageOf(o.Page, o.Theme)
	e.layout(o)
	end := e.contentEnd + 24
	// Where a page may end: between top-level blocks, and between the
	// lines of a paragraph that is not in a table or columns.
	cuts := []float32{end}
	for _, h := range e.handles {
		cuts = append(cuts, h.top-4)
	}
	for _, r := range e.rows {
		if r.layout == nil || r.leaf.Block.ID != r.leaf.Owner || r.picture != nil || r.math != nil || (r.mermaid != nil && r.mermaidHidden) {
			continue
		}
		for _, l := range r.layout.Lines()[1:] {
			cuts = append(cuts, r.y+l.Rect.Y)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i] < cuts[j] })
	tops := []float32{0}
	top := float32(0)
	for top+height < end {
		next := top + height
		best := float32(-1)
		for _, c := range cuts {
			if c > top+height*0.3 && c <= next {
				best = c
			}
		}
		if best < 0 {
			best = next // nothing to break at: cut through
		}
		top = best
		tops = append(tops, top)
	}
	return tops
}

// PaintPage draws the part of the page from top, height tall, into r, as
// Paginate laid it out, with the page's colors from t. The page's next
// page starts at bottom, which the drawing stops above.
func (e *Editor) PaintPage(p *ui.Painter, r ui.Rect, top, bottom float32, t *ui.Theme) {
	e.printing = true
	e.Scroll.Y, e.viewport = top, bottom-top
	e.pointerInside = false
	e.pointer = ui.Point{X: -1e6, Y: -1e6}
	p.Clip(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: bottom - top}, 0, func() {
		e.paint(p, ui.Rect{X: r.X, Y: r.Y - top, W: r.W, H: r.H + top}, t, false)
	})
}

// PrintText is a selectable text cluster positioned in document coordinates.
type PrintText struct {
	Text   string
	Bounds ui.Rect
}

// PageText uses the same glyph geometry as PaintPage, including wrapped text
// and table cells. Hidden TeX source and image alt text are not printed twice.
func (e *Editor) PageText(top, bottom float32) []PrintText {
	var out []PrintText
	for _, row := range e.rows {
		if row.layout == nil || row.picture != nil || row.mathHidden || row.mermaidHidden || row.rect.Y+row.rect.H <= top || row.rect.Y >= bottom {
			continue
		}
		text := row.leaf.Block.Text()
		for _, line := range row.layout.Lines() {
			y := row.y + line.Rect.Y
			if y < top || y >= bottom {
				continue
			}
			for at := line.Start; at < line.End; {
				next := min(line.End, row.layout.NextBoundary(at))
				if next <= at {
					break
				}
				object := false
				for _, o := range row.layout.objects {
					if !o.script && at >= o.start && at < o.end {
						object = true
						break
					}
				}
				if object {
					at = next
					continue
				}
				rects := row.layout.SelectionRects(ui.TextInputRange{Start: at, End: next})
				if len(rects) > 0 {
					r := rects[0]
					for _, o := range row.layout.objects {
						if o.script && at == o.start {
							r = o.rect
							break
						}
					}
					r.X += row.x
					r.Y += row.y
					out = append(out, PrintText{Text: text[ui.UTF16ByteOffset(text, at):ui.UTF16ByteOffset(text, next)], Bounds: r})
				}
				at = next
			}
		}
	}
	return out
}
