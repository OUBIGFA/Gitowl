package editor

import (
	"sort"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// Code, formulas, tables, columns and the like keep Enter to themselves,
// so a place between two of them, or after one that ends the document,
// cannot be reached by typing. A gap is that place: under the pointer it
// shows where a paragraph would go and the pointer turns into a hand, and
// a click adds the paragraph.

// gap is a place for a new paragraph before the top-level block at index
// (len for the end of the document); end tells the two looks apart.
type gap struct {
	index int
	rect  ui.Rect
	end   bool
}

// closed reports a block that typing does not continue past.
func closed(b *document.Block) bool {
	switch b.Kind {
	case "code", "raw", "math", "table", "columns", "image", "rule", "callout":
		return true
	}
	return false
}

// gaps are the places a click adds a paragraph: the strip between two
// closed blocks, and a row below a closed block that ends the document.
func (e *Editor) gaps() []gap {
	if e.ReadOnly || e.printing {
		return nil
	}
	blocks := e.Doc.Blocks
	at := func(h blockHandle) *document.Block {
		if h.index < 0 || h.index >= len(blocks) || blocks[h.index].ID != h.id {
			return nil
		}
		return blocks[h.index]
	}
	x, w := e.padding, e.width-2*e.padding
	var out []gap
	for j, h := range e.handles {
		b := at(h)
		if b == nil || !closed(b) {
			continue
		}
		if j+1 < len(e.handles) {
			next := e.handles[j+1]
			if nb := at(next); nb != nil && closed(nb) && next.index == h.index+1 {
				out = append(out, gap{index: next.index, rect: ui.Rect{X: x, Y: next.top - 10, W: w, H: 12}})
			}
		} else if h.index == len(blocks)-1 {
			out = append(out, gap{index: len(blocks), rect: ui.Rect{X: x, Y: e.contentEnd + 6, W: w, H: 30}, end: true})
		}
	}
	return out
}

// gapAt is the gap at a point of the page.
func (e *Editor) gapAt(px, py float32) (gap, bool) {
	if e.ReadOnly || e.printing || len(e.handles) == 0 {
		return gap{}, false
	}
	x, w := e.padding, e.width-2*e.padding
	if px < x || px > x+w {
		return gap{}, false
	}
	blocks := e.Doc.Blocks
	at := func(h blockHandle) *document.Block {
		if h.index < 0 || h.index >= len(blocks) || blocks[h.index].ID != h.id {
			return nil
		}
		return blocks[h.index]
	}
	last := e.handles[len(e.handles)-1]
	if last.index == len(blocks)-1 && py >= e.contentEnd+6 && py <= e.contentEnd+36 {
		if b := at(last); b != nil && closed(b) {
			return gap{index: len(blocks), rect: ui.Rect{X: x, Y: e.contentEnd + 6, W: w, H: 30}, end: true}, true
		}
	}
	start := sort.Search(len(e.handles), func(i int) bool { return e.handles[i].top+2 >= py })
	for k := start; k < len(e.handles); k++ {
		next := e.handles[k]
		if next.top-10 > py {
			break
		}
		if k == 0 {
			continue
		}
		h := e.handles[k-1]
		if next.index == h.index+1 && py >= next.top-10 && py <= next.top+2 {
			if b, nb := at(h), at(next); b != nil && closed(b) && nb != nil && closed(nb) {
				return gap{index: next.index, rect: ui.Rect{X: x, Y: next.top - 10, W: w, H: 12}}, true
			}
		}
	}
	return gap{}, false
}

// hoveredGap is the gap under the pointer.
func (e *Editor) hoveredGap() (gap, bool) {
	if !e.pointerInside || e.dragging || e.dragID != "" || e.resizing != nil {
		return gap{}, false
	}
	return e.gapAt(e.pointer.X, e.pointer.Y)
}

// fillGap adds an empty paragraph in a gap and puts the caret in it, as
// one step of the history.
func (e *Editor) fillGap(g gap) {
	e.UnmarkText()
	e.Engine.InsertAt(g.index, document.NewBlock("paragraph", ""))
	e.sync()
	e.RequestFocus, e.reveal, e.vertical = true, true, false
}

// paintGap shows the gap under the pointer: a line where the paragraph
// would go between two blocks, a row saying so at the end of the document.
func (e *Editor) paintGap(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	g, ok := e.hoveredGap()
	if !ok {
		return
	}
	r := g.rect
	r.X += bounds.X
	r.Y += bounds.Y
	if !g.end {
		p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H/2 - 1, W: r.W, H: 2}, t.Accent.Alpha(.6), 1)
		return
	}
	p.Fill(r, e.page.Hover, 6)
	p.Icon(iconPlus, ui.Rect{X: r.X + 10, Y: r.Y + (r.H-16)/2, W: 16, H: 16}, e.page.Caption)
	span := ui.Span{Text: i18n.T("点击添加段落"), Size: 14, Color: e.page.Caption}
	_, h := p.MeasureText(0, span)
	p.RichText(r.X+32, r.Y+(r.H-h)/2, 0, span)
}
