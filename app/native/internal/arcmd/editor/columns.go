package editor

import (
	"fmt"
	"math"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

const columnInset = float32(12)

func normalizedColumnWidths(b *document.Block, avail float32) []float32 {
	n := len(b.Children)
	widths := make([]float32, n)
	if n == 0 {
		return widths
	}
	sum := float32(0)
	if len(b.Widths) == n {
		for _, v := range b.Widths {
			if v > 0 {
				sum += v
			} else {
				sum = 0
				break
			}
		}
	}
	if sum > 0 {
		for i, v := range b.Widths {
			widths[i] = avail * v / sum
		}
	} else {
		each := avail / float32(n)
		for i := range widths {
			widths[i] = each
		}
	}
	minW := min(float32(64), avail/float32(n))
	for range widths {
		deficit, flex := float32(0), float32(0)
		for i, cw := range widths {
			if cw < minW {
				deficit += minW - cw
				widths[i] = minW
			} else {
				flex += cw - minW
			}
		}
		if deficit == 0 || flex == 0 {
			break
		}
		for i, cw := range widths {
			if cw > minW {
				widths[i] -= deficit * (cw - minW) / flex
			}
		}
	}
	return widths
}

// layoutColumns gives each column a stable writing area. Empty columns
// remain large enough to discover and click, including when stacked.
func (e *Editor) layoutColumns(b *document.Block, x, y, w float32, hidden map[string]string, layout func(*document.Block, float32, float32, float32) float32) float32 {
	const gap, minHeight = float32(24), float32(72)
	stacked := w < 480
	n := len(b.Children)
	var widths []float32
	avail := max(float32(n*48), w-float32(n-1)*gap)
	if !stacked {
		widths = normalizedColumnWidths(b, avail)
	}
	top, bottom := y, y
	cx := x
	colRights := make([]float32, n)
	for i, column := range b.Children {
		cw := w
		if !stacked {
			cw = widths[i]
		} else {
			cx = x
		}
		start := len(e.rows)
		cy := y + columnInset
		for _, child := range column.Children {
			if hidden[child.ID] == "" {
				cy += layout(child, cx+columnInset, cy, max(24, cw-2*columnInset))
			}
		}
		h := max(minHeight, cy-y+columnInset)
		r := ui.Rect{X: cx, Y: y, W: cw, H: h}
		empty := document.Empty(column)
		if empty && start < len(e.rows) && e.rows[start].leaf.Block.Kind == "paragraph" {
			for j := start; j < len(e.rows); j++ {
				e.rows[j].emptyColumn = true
			}
			if len(e.rows) == start+1 {
				// The entire empty area accepts a click, not just its text line.
				e.rows[start].rect = r
			}
		}
		e.decorations = append(e.decorations, decoration{kind: "column", rect: r, id: column.ID, ownerID: b.ID, column: column, empty: empty})
		colRights[i] = cx + cw
		bottom = max(bottom, y+h)
		if stacked {
			y = bottom + 12
		} else {
			cx += cw + gap
		}
	}
	if !stacked && n >= 2 {
		groupH := bottom - top
		for col := 0; col < n-1; col++ {
			seamX := colRights[col] + gap/2
			e.resizers = append(e.resizers, columnHandle{
				id:     b.ID,
				col:    col,
				x:      seamX,
				y:      top,
				h:      groupH,
				width:  avail,
				widths: widths,
			})
		}
	}
	return bottom - top + 12
}

func (e *Editor) hoveredResizer() *columnHandle {
	if e.ReadOnly || !e.pointerInside || e.dragging || e.dragID != "" {
		return nil
	}
	px, py := e.pointer.X, e.pointer.Y
	for i := range e.resizers {
		r := e.indexedResizer(i)
		if math.Abs(float64(px-r.x)) < 5 && py >= r.y && py <= r.y+r.h {
			return r
		}
	}
	return nil
}

// paintColumnsResizers draws the subtle middle-seam grip on hovered multi-column
// blocks and the live guide + percentage pill while resizing a column seam.
func (e *Editor) paintColumnsResizers(p *ui.Painter, bounds ui.Rect) {
	if e.ReadOnly {
		return
	}
	for i := range e.resizers {
		res := e.indexedResizer(i)
		if res.y+res.h < e.Scroll.Y-40 || res.y > e.Scroll.Y+e.viewport+40 {
			continue
		}
		blk := e.blockByID(res.id)
		if blk == nil || blk.Kind != "columns" {
			continue
		}
		ownerDY := e.ownerVisualDY(res.id)
		if e.resizing != nil && e.resizing.id == res.id && e.resizing.col == res.col {
			r := e.resizing
			delta, snapped := clampedResizeDelta(r, e.resizeX, e.resizeMoved)
			lineX := bounds.X + r.x + delta
			topY := bounds.Y + r.y + ownerDY
			lineColor := e.page.Marker
			if snapped {
				lineColor = p.Theme().Accent
			}
			p.Fill(ui.Rect{X: lineX - 1, Y: topY, W: 2, H: r.h}, lineColor, 1)

			w0 := r.widths[r.col] + delta
			w1 := r.widths[r.col+1] - delta
			total := max(1, r.width)
			pct0 := int(math.Round(float64(w0 * 100 / total)))
			pct1 := int(math.Round(float64(w1 * 100 / total)))
			label := fmt.Sprintf("%d%% · %d%%", pct0, pct1)
			span := ui.Span{Text: label, Size: 11.5, Weight: 600, Color: e.page.OnScrim, Features: "tnum"}
			tw, th := p.MeasureText(0, span)
			pw, ph := tw+14, float32(22)
			minX := bounds.X + e.padding
			maxX := bounds.X + max(e.padding, e.width-e.padding-pw)
			pill := ui.Rect{
				X: max(minX, min(lineX-pw/2, maxX)),
				Y: topY - ph - 6,
				W: pw,
				H: ph,
			}
			p.Fill(pill, e.page.Scrim, 6)
			if snapped {
				p.Stroke(pill, lineColor.Alpha(0.85), 6, 1)
			}
			p.RichText(pill.X+7, pill.Y+(ph-th)/2, 0, span)
			continue
		}
		if e.resizing != nil {
			continue
		}
		nearSeam := e.pointerInside && math.Abs(float64(e.pointer.X-res.x)) < 6 && e.pointer.Y >= res.y && e.pointer.Y <= res.y+res.h
		inGroup := e.hoverID == res.id || (e.focused && e.Engine.LeafAt(e.Caret).Owner == res.id)
		if !nearSeam && !inGroup {
			continue
		}
		gripH := min(float32(28), max(16, res.h*0.32))
		grip := ui.Rect{
			X: bounds.X + res.x - 2,
			Y: bounds.Y + res.y + ownerDY + (res.h-gripH)/2,
			W: 4,
			H: gripH,
		}
		col := e.page.BarFill
		if nearSeam {
			col = e.page.BarHover
		}
		p.Fill(grip, col, 2)
	}
}

// paintColumn keeps the scaffold quiet once there is content, and never
// participates in document storage or printing.
func (e *Editor) paintColumn(p *ui.Painter, r ui.Rect, d decoration) {
	active := e.focused && e.Engine.LeafAt(e.Caret).Parent == &d.column.Children
	if !d.empty && !active && !e.hovered(d.rect, 0) {
		return
	}
	p.StrokeDashed(r, e.page.TableLine, 8, 1)
}

// columnTab switches between sibling columns without inserting whitespace.
// Lists, code, table cells and selections keep their existing Tab behavior.
func (e *Editor) columnTab(backward bool) bool {
	if e.Anchor != e.Caret || e.mark != nil {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	switch leaf.Block.Kind {
	case "paragraph", "heading", "quote", "callout":
	default:
		return false
	}
	var group *document.Block
	index := 0
	var find func([]*document.Block)
	find = func(blocks []*document.Block) {
		for _, b := range blocks {
			if b.Kind == "columns" {
				for i, col := range b.Children {
					if &col.Children == leaf.Parent {
						group, index = b, i
						return
					}
				}
			}
			find(b.Children)
			if group != nil {
				return
			}
		}
	}
	find(e.Doc.Blocks)
	if group == nil {
		return false
	}
	step := 1
	if backward {
		step = -1
	}
	column := group.Children[(index+step+len(group.Children))%len(group.Children)]
	// Find the first visible leaf, also for nested content and folded sections.
	ids := make(map[string]bool)
	var collect func(*document.Block)
	collect = func(b *document.Block) {
		ids[b.ID] = true
		for _, child := range b.Children {
			collect(child)
		}
	}
	collect(column)
	for i := range e.rows {
		r := e.indexedRow(i)
		if ids[r.leaf.Block.ID] {
			e.SetSelection(r.leaf.Start, r.leaf.Start)
			e.reveal, e.vertical = true, false
			return true
		}
	}
	return false
}
