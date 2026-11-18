package editor

// Tables show bars above their columns and left of their rows: a press on
// a bar selects its row or column and shows a toolbar to insert beside it
// or delete it; dragging a row bar reorders rows with live sliding neighbors;
// a "+" pin on a boundary previews a dashed seam and inserts a line there.

import (
	"fmt"
	"math"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

const (
	// tableBar is the thickness of the bars; tableBarGap parts them from
	// the grid.
	tableBar    = 10
	tableBarGap = 4
)

// tableSelection is a selected row or column of a table.
type tableSelection struct {
	id    string
	rows  bool
	index int
	ok    bool
}

type tableRowDrag struct {
	id      string
	from    int
	to      int
	startY  float32
	y       float32
	holding bool
	active  bool
}

type tableColDrag struct {
	id      string
	from    int
	to      int
	startX  float32
	x       float32
	holding bool
	active  bool
}

// columnBar and rowBar are the bar segments of column or row i.
func columnBar(d decoration, i int) ui.Rect {
	return ui.Rect{X: d.cols[i], Y: d.rect.Y - tableBarGap - tableBar, W: d.cols[i+1] - d.cols[i], H: tableBar}
}
func rowBar(d decoration, i int) ui.Rect {
	return ui.Rect{X: d.rect.X - tableBarGap - tableBar, Y: d.rows[i], W: tableBar, H: d.rows[i+1] - d.rows[i]}
}

// pin is the "+" on boundary k of the column bar (cols) or the row bar.
func pin(d decoration, cols bool, k int) ui.Rect {
	if cols {
		return ui.Rect{X: d.cols[k] - 8, Y: d.rect.Y - tableBarGap - tableBar/2 - 8, W: 16, H: 16}
	}
	return ui.Rect{X: d.rect.X - tableBarGap - tableBar/2 - 8, Y: d.rows[k] - 8, W: 16, H: 16}
}

// barsShown reports whether table d shows its bars: under the pointer,
// holding the caret, dragging a row/column, or with a line selected.
func (e *Editor) barsShown(d decoration) bool {
	if e.tableDrag.active && e.tableDrag.id == d.id {
		return true
	}
	if e.tableColDrag.active && e.tableColDrag.id == d.id {
		return true
	}
	if e.resizing != nil && e.resizing.id == d.id {
		return true
	}
	if e.tableSel.ok && e.tableSel.id == d.id {
		return true
	}
	if e.focused && e.Engine.LeafAt(e.Caret).Owner == d.id {
		return true
	}
	return e.hovered(ui.Rect{X: d.rect.X - 24, Y: d.rect.Y - 24, W: d.rect.W + 24, H: d.rect.H + 24}, 0)
}

// hoveredPin is the boundary pin under the pointer, if any.
func (e *Editor) hoveredPin(d decoration) (cols bool, k int, ok bool) {
	p := e.pointer
	if !e.pointerInside || e.dragging || e.tableDrag.holding || e.tableColDrag.holding || e.resizing != nil {
		return false, 0, false
	}
	near := func(r ui.Rect) bool { return p.X >= r.X && p.X <= r.X+r.W && p.Y >= r.Y && p.Y <= r.Y+r.H }
	for k := range d.cols {
		if near(pin(d, true, k)) {
			return true, k, true
		}
	}
	for k := range d.rows {
		if near(pin(d, false, k)) {
			return false, k, true
		}
	}
	return false, 0, false
}

// selectedLine is the table selection's decoration, if it still exists.
func (e *Editor) selectedLine() (decoration, bool) {
	if !e.tableSel.ok {
		return decoration{}, false
	}
	if di, ok := e.decorationIDs[e.tableSel.id]; ok {
		d := *e.indexedDecoration(di)
		if d.kind == "table" {
			n := len(d.cols) - 1
			if e.tableSel.rows {
				n = len(d.rows) - 1
			}
			if e.tableSel.index < n {
				return d, true
			}
		}
	}
	e.tableSel = tableSelection{}
	return decoration{}, false
}

func findParentBlocks(list *[]*document.Block, id string) (*[]*document.Block, int) {
	for i, b := range *list {
		if b.ID == id {
			return list, i
		}
		if b.Kind == "columns" || b.Kind == "column" {
			if p, idx := findParentBlocks(&b.Children, id); p != nil {
				return p, idx
			}
		}
	}
	return nil, -1
}

func (e *Editor) tableDecoration(id string) (decoration, bool) {
	if di, ok := e.decorationIDs[id]; ok {
		d := *e.indexedDecoration(di)
		if d.kind == "table" && d.id == id {
			return d, true
		}
	}
	return decoration{}, false
}

// tableDragShift returns the target vertical displacement for rowIdx while
// dragging another row in the same table.
func (e *Editor) tableDragShift(tableID string, rowIdx int) float32 {
	if !e.tableDrag.active || e.tableDrag.id != tableID || e.tableDrag.from == e.tableDrag.to {
		return 0
	}
	d, ok := e.tableDecoration(tableID)
	if !ok || e.tableDrag.from < 0 || e.tableDrag.from >= len(d.rows)-1 {
		return 0
	}
	dragH := d.rows[e.tableDrag.from+1] - d.rows[e.tableDrag.from]
	if e.tableDrag.from < e.tableDrag.to && rowIdx > e.tableDrag.from && rowIdx <= e.tableDrag.to {
		return -dragH
	}
	if e.tableDrag.to < e.tableDrag.from && rowIdx >= e.tableDrag.to && rowIdx < e.tableDrag.from {
		return dragH
	}
	return 0
}

func (e *Editor) tableDragLandingY(d decoration) float32 {
	from, to := e.tableDrag.from, e.tableDrag.to
	if from < 0 || from >= len(d.rows)-1 || to < 0 || to >= len(d.rows)-1 {
		return d.rect.Y
	}
	dragH := d.rows[from+1] - d.rows[from]
	if to <= from {
		return d.rows[to]
	}
	return d.rows[to+1] - dragH
}

// tableColDragShift returns the target horizontal displacement for colIdx while
// dragging another column in the same table.
func (e *Editor) tableColDragShift(tableID string, colIdx int) float32 {
	if !e.tableColDrag.active || e.tableColDrag.id != tableID || e.tableColDrag.from == e.tableColDrag.to {
		return 0
	}
	d, ok := e.tableDecoration(tableID)
	if !ok || e.tableColDrag.from < 0 || e.tableColDrag.from >= len(d.cols)-1 {
		return 0
	}
	dragW := d.cols[e.tableColDrag.from+1] - d.cols[e.tableColDrag.from]
	if e.tableColDrag.from < e.tableColDrag.to && colIdx > e.tableColDrag.from && colIdx <= e.tableColDrag.to {
		return -dragW
	}
	if e.tableColDrag.to < e.tableColDrag.from && colIdx >= e.tableColDrag.to && colIdx < e.tableColDrag.from {
		return dragW
	}
	return 0
}

func (e *Editor) tableColDragLandingX(d decoration) float32 {
	from, to := e.tableColDrag.from, e.tableColDrag.to
	if from < 0 || from >= len(d.cols)-1 || to < 0 || to >= len(d.cols)-1 {
		return d.rect.X
	}
	dragW := d.cols[from+1] - d.cols[from]
	if to <= from {
		return d.cols[to]
	}
	return d.cols[to+1] - dragW
}

func clampedResizeDelta(r *columnHandle, resizeX float32, movedFar bool) (delta float32, snapped bool) {
	if r == nil || r.col < 0 || r.col+1 >= len(r.widths) {
		return 0, false
	}
	raw := resizeX - r.x
	lo := 48 - r.widths[r.col]
	hi := r.widths[r.col+1] - 48
	equalDelta := (r.widths[r.col+1] - r.widths[r.col]) * 0.5
	if equalDelta >= lo && equalDelta <= hi && abs32(raw-equalDelta) <= 6 && (abs32(equalDelta) > 1 || movedFar) {
		return equalDelta, true
	}
	return max(lo, min(raw, hi)), false
}

func (e *Editor) tableDragPointer(ev ui.InputEvent) bool {
	if e.tableColDrag.holding {
		switch ev.Kind {
		case ui.InputPointerMove:
			e.tableColDrag.x = ev.X
			if !e.tableColDrag.active && abs32(ev.X-e.tableColDrag.startX) > 4 {
				e.tableColDrag.active = true
				e.tableSel = tableSelection{}
			}
			if e.tableColDrag.active {
				if d, ok := e.tableDecoration(e.tableColDrag.id); ok && len(d.cols) >= 3 {
					n := len(d.cols) - 1
					target := n - 1
					for i := 0; i < n; i++ {
						if ev.X < d.cols[i+1] {
							target = i
							break
						}
					}
					e.tableColDrag.to = target
				}
			}
			return true
		case ui.InputPointerUp:
			drag := e.tableColDrag
			e.tableColDrag = tableColDrag{}
			if drag.active && drag.from != drag.to {
				if d, ok := e.tableDecoration(drag.id); ok && e.motion.tableCols != nil {
					if tb := e.blockByID(drag.id); tb != nil && len(tb.Children) > 0 && len(tb.Children[0].Children)+1 == len(d.cols) {
						dragW := d.cols[drag.from+1] - d.cols[drag.from]
						for i, cell := range tb.Children[0].Children {
							shift := float32(0)
							if i == drag.from {
								shift = drag.x - drag.startX
							} else if drag.from < drag.to && i > drag.from && i <= drag.to {
								shift = -dragW
							} else if drag.to < drag.from && i >= drag.to && i < drag.from {
								shift = dragW
							}
							if s, ok := e.motion.tableCols[cell.ID]; ok {
								s.laidX += shift
								e.motion.tableCols[cell.ID] = s
							}
						}
					}
				}
				e.Engine.MoveTableColumn(drag.id, drag.from, drag.to)
				e.sync()
				e.RequestFocus = true
			}
			return true
		}
		return false
	}
	if !e.tableDrag.holding {
		return false
	}
	switch ev.Kind {
	case ui.InputPointerMove:
		e.tableDrag.y = ev.Y
		if !e.tableDrag.active && abs32(ev.Y-e.tableDrag.startY) > 4 {
			e.tableDrag.active = true
			e.tableSel = tableSelection{}
		}
		if e.tableDrag.active {
			if d, ok := e.tableDecoration(e.tableDrag.id); ok && len(d.rows) >= 3 {
				n := len(d.rows) - 1
				target := n - 1
				for i := 0; i < n; i++ {
					if ev.Y < d.rows[i+1] {
						target = i
						break
					}
				}
				e.tableDrag.to = target
			}
		}
		return true
	case ui.InputPointerUp:
		drag := e.tableDrag
		e.tableDrag = tableRowDrag{}
		if drag.active && drag.from != drag.to {
			if d, ok := e.tableDecoration(drag.id); ok && e.motion.tableRows != nil {
				if tb := e.blockByID(drag.id); tb != nil && len(tb.Children)+1 == len(d.rows) {
					for i, tr := range tb.Children {
						shift := float32(0)
						if i == drag.from {
							shift = drag.y - drag.startY
						} else if drag.from < drag.to && i > drag.from && i <= drag.to {
							shift = -(d.rows[drag.from+1] - d.rows[drag.from])
						} else if drag.to < drag.from && i >= drag.to && i < drag.from {
							shift = d.rows[drag.from+1] - d.rows[drag.from]
						}
						if s, ok := e.motion.tableRows[tr.ID]; ok {
							s.laidY += shift
							e.motion.tableRows[tr.ID] = s
						}
					}
				}
			}
			e.Engine.MoveTableRow(drag.id, drag.from, drag.to)
			e.sync()
			e.RequestFocus = true
		}
		return true
	}
	return false
}

// cellEnter handles Enter inside a table cell: moves down to the same column,
// appends a new row at the bottom, or exits out of a trailing empty row.
func (e *Editor) cellEnter() bool {
	if e.ReadOnly || e.Anchor != e.Caret {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return false
	}
	table, rowIdx, colIdx, ok := e.Doc.CellAt(leaf.Block.ID)
	if !ok || table == nil || len(table.Children) == 0 {
		return false
	}
	spanCols, spanRows := document.Span(leaf.Block)
	nextRow := rowIdx + spanRows

	focusCellAt := func(t *document.Block, r, c int) bool {
		if r < 0 || r >= len(t.Children) || len(t.Children[r].Children) == 0 {
			return false
		}
		c = max(0, min(c, len(t.Children[r].Children)-1))
		target := t.Children[r].Children[c]
		for ci := c; ci >= 0 && target.ColSpan < 0; ci-- {
			target = t.Children[r].Children[ci]
		}
		for ri := r; ri >= 0 && target.RowSpan < 0; ri-- {
			target = t.Children[ri].Children[min(c, len(t.Children[ri].Children)-1)]
		}
		if l, ok := e.Engine.LeafByID(target.ID); ok {
			e.SetSelection(l.End, l.End)
			e.reveal = true
			return true
		}
		return false
	}

	if nextRow < len(table.Children) {
		return focusCellAt(table, nextRow, colIdx)
	}

	// Double-Enter on a trailing blank data row at column 0 exits the table
	// cleanly into a paragraph below.
	if rowIdx >= 1 && colIdx == 0 {
		rowEmpty := true
		for _, cell := range table.Children[rowIdx].Children {
			if strings.TrimSpace(cell.Text()) != "" {
				rowEmpty = false
				break
			}
		}
		if rowEmpty {
			tableID := table.ID
			trimTrailing := len(table.Children) > 2
			e.Engine.Edit(func() {
				if trimTrailing {
					e.Engine.TableDelete(tableID, true, rowIdx)
				}
				if parent, idx := findParentBlocks(&e.Doc.Blocks, tableID); parent != nil {
					next := document.NewBlock("paragraph", "")
					*parent = append((*parent)[:idx+1], append([]*document.Block{next}, (*parent)[idx+1:]...)...)
					e.Engine.FocusBlock(next.ID)
				}
			})
			e.sync()
			e.reveal = true
			return true
		}
	}

	numCols := len(table.Children[rowIdx].Children)
	targetCol := colIdx
	if colIdx+spanCols >= numCols && numCols > 1 && strings.TrimSpace(table.Children[rowIdx].Children[0].Text()) != "" {
		targetCol = 0
	}
	tableID := table.ID
	e.Engine.ResizeTable(true, 1)
	e.sync()
	for _, b := range e.Doc.Blocks {
		if b.ID == tableID {
			return focusCellAt(b, len(b.Children)-1, targetCol)
		}
	}
	if updated, _, _, ok := e.Doc.CellAt(leaf.Block.ID); ok {
		return focusCellAt(updated, len(updated.Children)-1, targetCol)
	}
	return true
}

// paintTables draws the bars, pins, dashed seam previews, row/column drag slots,
// and live column-width resize feedback.
func (e *Editor) paintTables(p *ui.Painter, bounds ui.Rect) {
	pg := e.page
	for i := range e.visibleDecorations(24) {
		d := *e.indexedDecoration(i)
		// Include the bars and insertion pins above and beside the table.
		if d.kind != "table" || d.rect.Y+d.rect.H+24 < e.Scroll.Y || d.rect.Y-24 > e.Scroll.Y+e.viewport || len(d.cols) < 2 || !e.barsShown(d) {
			continue
		}
		ownerDY := e.ownerVisualDY(d.id)
		offset := func(r ui.Rect) ui.Rect {
			r.X += bounds.X
			r.Y += bounds.Y + ownerDY
			return r
		}
		sel := e.tableSel
		selected := func(rows bool, i int) bool { return sel.ok && sel.id == d.id && sel.rows == rows && sel.index == i }
		// The selected line's cells, red while its delete is under the
		// pointer.
		if sel.ok && sel.id == d.id {
			var r ui.Rect
			if sel.rows && sel.index < len(d.rows)-1 {
				ry := e.tableRowVisualDY(d.id, sel.index)
				r = ui.Rect{X: d.rect.X, Y: d.rows[sel.index] + ry, W: d.rect.W, H: d.rows[sel.index+1] - d.rows[sel.index]}
			} else if !sel.rows && sel.index < len(d.cols)-1 {
				cx := e.tableColVisualDX(d.id, sel.index)
				r = ui.Rect{X: d.cols[sel.index] + cx, Y: d.rect.Y, W: d.cols[sel.index+1] - d.cols[sel.index], H: d.rect.H}
			}
			fill := pg.CellSelected
			if e.tableDanger {
				fill = pg.DangerCell
			}
			p.Clip(offset(d.rect), 8, func() { p.Fill(offset(r), fill, 0) })
		}
		if id, r, ok := e.cellRange(); ok && id == d.id {
			cells := ui.Rect{X: d.cols[r.Col0], Y: d.rows[r.Row0], W: d.cols[r.Col1+1] - d.cols[r.Col0], H: d.rows[r.Row1+1] - d.rows[r.Row0]}
			p.Clip(offset(d.rect), 8, func() { p.Fill(offset(cells), pg.CellSelected, 0) })
		}
		for i := 0; i < len(d.cols)-1; i++ {
			cb := columnBar(d, i)
			cx := e.tableColVisualDX(d.id, i)
			if e.tableColDrag.active && e.tableColDrag.id == d.id && e.tableColDrag.from == i {
				cx += e.tableColDrag.x - e.tableColDrag.startX
			}
			vis := offset(cb)
			vis.X += cx
			e.paintBar(p, vis, cb, selected(false, i) || e.tableColDrag.active && e.tableColDrag.id == d.id && e.tableColDrag.from == i)
		}
		for i := 0; i < len(d.rows)-1; i++ {
			rb := rowBar(d, i)
			ry := e.tableRowVisualDY(d.id, i)
			if e.tableDrag.active && e.tableDrag.id == d.id && e.tableDrag.from == i {
				ry += e.tableDrag.y - e.tableDrag.startY
			}
			vis := offset(rb)
			vis.Y += ry
			e.paintBar(p, vis, rb, selected(true, i) || e.tableDrag.active && e.tableDrag.id == d.id && e.tableDrag.from == i)
		}
		if e.tableColDrag.active && e.tableColDrag.id == d.id && e.tableColDrag.from < len(d.cols)-1 {
			dragW := d.cols[e.tableColDrag.from+1] - d.cols[e.tableColDrag.from]
			slot := offset(ui.Rect{X: e.tableColDragLandingX(d), Y: d.rect.Y, W: dragW, H: d.rect.H})
			p.Fill(slot, pg.Marker.Alpha(0.08), 4)
			p.StrokeDashed(slot, pg.Marker.Alpha(0.65), 4, 1.25)

			liftedX := d.cols[e.tableColDrag.from] + (e.tableColDrag.x - e.tableColDrag.startX)
			lifted := offset(ui.Rect{X: liftedX, Y: d.rect.Y, W: dragW, H: d.rect.H})
			p.Shadow(lifted, 6, 0, 4, 14, 0, pg.MathShadow)
			p.Fill(lifted, pg.Popover.Alpha(0.88), 6)
			p.Stroke(lifted, pg.Marker.Alpha(0.75), 6, 1.5)
		}
		if e.tableDrag.active && e.tableDrag.id == d.id && e.tableDrag.from < len(d.rows)-1 {
			dragH := d.rows[e.tableDrag.from+1] - d.rows[e.tableDrag.from]
			slot := offset(ui.Rect{X: d.rect.X, Y: e.tableDragLandingY(d), W: d.rect.W, H: dragH})
			p.Fill(slot, pg.Marker.Alpha(0.08), 4)
			p.StrokeDashed(slot, pg.Marker.Alpha(0.65), 4, 1.25)

			liftedY := d.rows[e.tableDrag.from] + (e.tableDrag.y - e.tableDrag.startY)
			lifted := offset(ui.Rect{X: d.rect.X, Y: liftedY, W: d.rect.W, H: dragH})
			p.Shadow(lifted, 6, 0, 4, 14, 0, pg.MathShadow)
			p.Fill(lifted, pg.Popover.Alpha(0.88), 6)
			p.Stroke(lifted, pg.Marker.Alpha(0.75), 6, 1.5)
		}
		if e.resizing != nil && e.resizing.id == d.id {
			e.paintColumnResizePreview(p, bounds, d, ownerDY)
		}
		if cols, k, ok := e.hoveredPin(d); ok {
			r := offset(pin(d, cols, k))
			if cols {
				slot := ui.Rect{X: r.X + 4, Y: bounds.Y + d.rect.Y + ownerDY, W: 8, H: d.rect.H}
				p.Fill(slot, pg.Marker.Alpha(0.10), 3)
				p.StrokeDashed(slot, pg.Marker.Alpha(0.75), 3, 1)
			} else {
				slot := ui.Rect{X: bounds.X + d.rect.X, Y: r.Y + 4, W: d.rect.W, H: 8}
				p.Fill(slot, pg.Marker.Alpha(0.10), 3)
				p.StrokeDashed(slot, pg.Marker.Alpha(0.75), 3, 1)
			}
			p.Fill(r, pg.Marker, 8)
			p.Icon(iconPlus, ui.Rect{X: r.X + 3, Y: r.Y + 3, W: 10, H: 10}, pg.OnMarker)
		}
	}
}

func (e *Editor) paintColumnResizePreview(p *ui.Painter, bounds ui.Rect, d decoration, ownerDY float32) {
	r := e.resizing
	if r == nil || r.col < 0 || r.col+1 >= len(r.widths) {
		return
	}
	delta, snapped := clampedResizeDelta(r, e.resizeX, e.resizeMoved)
	lineX := bounds.X + r.x + delta
	topY := bounds.Y + d.rect.Y + ownerDY
	lineColor := e.page.Marker
	if snapped {
		lineColor = p.Theme().Accent
	}
	p.Fill(ui.Rect{X: lineX - 1, Y: topY, W: 2, H: d.rect.H}, lineColor, 1)

	w0 := r.widths[r.col] + delta
	w1 := r.widths[r.col+1] - delta
	total := max(1, r.width)
	pct0 := int(math.Round(float64(w0 * 100 / total)))
	pct1 := int(math.Round(float64(w1 * 100 / total)))
	label := fmt.Sprintf("%d%% · %d%%", pct0, pct1)
	span := ui.Span{Text: label, Size: 11.5, Weight: 600, Color: e.page.OnScrim, Features: "tnum"}
	tw, th := p.MeasureText(0, span)
	pw, ph := tw+14, float32(22)
	pill := ui.Rect{
		X: max(bounds.X+d.rect.X, min(lineX-pw/2, bounds.X+d.rect.X+d.rect.W-pw)),
		Y: topY - tableBarGap - tableBar - ph - 4,
		W: pw,
		H: ph,
	}
	p.Fill(pill, e.page.Scrim, 6)
	if snapped {
		p.Stroke(pill, lineColor.Alpha(0.85), 6, 1)
	}
	p.RichText(pill.X+7, pill.Y+(ph-th)/2, 0, span)
}

// paintBar draws one bar segment at r (local is its page rectangle).
func (e *Editor) paintBar(p *ui.Painter, r, local ui.Rect, selected bool) {
	pg := e.page
	fill := pg.BarFill
	switch {
	case selected && e.tableDanger:
		fill = pg.Danger
	case selected:
		fill = pg.BarSelected
	case e.hovered(local, 0):
		fill = pg.BarHover
	}
	if r.W > r.H {
		r.X, r.W = r.X+1, r.W-2
	} else {
		r.Y, r.H = r.Y+1, r.H-2
	}
	p.Fill(r, fill, 3)
}

// tableControl runs a press on table d's pins or bars.
func (e *Editor) tableControl(d decoration, x, y float32) bool {
	if len(d.cols) < 2 || !e.barsShown(d) {
		return false
	}
	in := func(r ui.Rect) bool { return x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H }
	insert := func(cols bool, k int) {
		e.tableSel = tableSelection{}
		e.Engine.TableInsert(d.id, !cols, k)
		e.sync()
		e.RequestFocus, e.reveal = true, true
	}
	for k := range d.cols {
		if in(pin(d, true, k)) {
			insert(true, k)
			return true
		}
	}
	for k := range d.rows {
		if in(pin(d, false, k)) {
			insert(false, k)
			return true
		}
	}
	for i := 0; i < len(d.cols)-1; i++ {
		if in(columnBar(d, i)) {
			e.tableSel = tableSelection{id: d.id, index: i, ok: true}
			if len(d.cols)-1 >= 2 {
				e.tableColDrag = tableColDrag{id: d.id, from: i, to: i, startX: x, x: x, holding: true}
			}
			return true
		}
	}
	for i := 0; i < len(d.rows)-1; i++ {
		if in(rowBar(d, i)) {
			e.tableSel = tableSelection{id: d.id, rows: true, index: i, ok: true}
			if len(d.rows)-1 >= 2 {
				e.tableDrag = tableRowDrag{id: d.id, from: i, to: i, startY: y, y: y, holding: true}
			}
			return true
		}
	}
	return false
}

// SelectTableLine selects row (rows) or column index of table id, as a
// press on its bar does.
func (e *Editor) SelectTableLine(id string, rows bool, index int) {
	e.tableSel = tableSelection{id: id, rows: rows, index: index, ok: true}
}

// deleteTableLine deletes the selected row or column.
func (e *Editor) deleteTableLine() {
	sel := e.tableSel
	e.tableSel, e.tableDanger = tableSelection{}, false
	e.Engine.TableDelete(sel.id, sel.rows, sel.index)
	e.sync()
	e.RequestFocus, e.reveal = true, true
}

// cellRange is the rectangle of cells a text selection spans when it
// runs across two or more cells of one table, grown over merged cells.
func (e *Editor) cellRange() (string, document.CellRange, bool) {
	sel := e.Selection().Range
	if sel.Start == sel.End {
		return "", document.CellRange{}, false
	}
	a, z := e.Engine.LeafAt(sel.Start), e.Engine.LeafAt(sel.End)
	if a.Block == z.Block || a.Block.Kind != "cell" || z.Block.Kind != "cell" || a.Owner != z.Owner {
		return "", document.CellRange{}, false
	}
	table, r0, c0, ok0 := e.Doc.CellAt(a.Block.ID)
	_, r1, c1, ok1 := e.Doc.CellAt(z.Block.ID)
	if !ok0 || !ok1 {
		return "", document.CellRange{}, false
	}
	r := document.CellRange{Row0: min(r0, r1), Col0: min(c0, c1), Row1: max(r0, r1), Col1: max(c0, c1)}
	return table.ID, document.Expand(table, r), true
}

// caretCell is the merged cell holding the caret, if any.
func (e *Editor) caretCell() (*document.Block, bool) {
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "cell" {
		return nil, false
	}
	cols, rows := document.Span(leaf.Block)
	return leaf.Block, cols > 1 || rows > 1
}

// cellFillButton opens the cell background panel.
func (e *Editor) cellFillButton(c *ui.Context) {
	b := ui.ButtonBase(c.Key("table:fill")).Size(24, 24).Radius(4).Center().Label(i18n.T("单元格背景颜色")).Tooltip(i18n.T("单元格背景颜色")).KeepFocus().Cursor(ui.CursorPointer)
	if b.Hovered() || e.cellFillOpen {
		b.Background(e.page.Hover)
	}
	b.Children(func() { ui.Icon(c, iconPaintBucket).FontSize(18).TextColor(e.page.CodeText) })
	if b.Clicked() {
		e.cellFillOpen, e.cellFillOpened = !e.cellFillOpen, true
	}
}

// cellFillPanel picks the background of the cells of r in table id.
func (e *Editor) cellFillPanel(c *ui.Context, x, y float32, id string, r document.CellRange, current string) {
	if !e.cellFillOpen {
		return
	}
	t := c.Theme()
	pg := e.page
	panel := ui.Column(c.Key("cell-fill-panel")).Absolute().Left(max(8, x)).Top(y).Width(230).Padding(4, 10, 12, 10)
	e.popover(c, panel).Children(func() {
		ui.Text(c, i18n.T("单元格背景颜色")).FontSize(12).TextColor(t.Text).Margin(7, 0, 4, 0)
		names := append([]string{""}, document.TextHighlights...)
		ui.Column(c).Gap(4).Children(func() {
			for row := 0; row < len(names); row += 8 {
				ui.Row(c.Key(row)).Justify(ui.SpaceBetween).Children(func() {
					for _, name := range names[row:min(row+8, len(names))] {
						label := i18n.T("无背景颜色")
						if name != "" {
							label = i18n.T("单元格背景：") + ColorName(name, true)
						}
						s := ui.ButtonBase(c.Key("cf:"+name)).Size(26, 26).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
						if current == name {
							s.Border(2, pg.CodeHover)
						} else if s.Hovered() {
							s.Background(pg.Hover)
						}
						s.Children(func() {
							box := ui.Box(c).Size(22, 22).Radius(2).Border(1, pg.Hover)
							if name == "" {
								box.Background(t.Background).Center().Children(func() {
									ui.Icon(c, iconSlash).FontSize(20).TextColor(pg.Placeholder)
								})
							} else {
								box.Background(pg.CellFills[name])
							}
						})
						if s.Clicked() {
							e.Engine.SetCellBackground(id, r, name)
							e.cellFillOpen = false
							e.RequestFocus = true
						}
					}
				})
			}
		})
	})
	if panel.PressedOutside() && !e.cellFillOpened {
		e.cellFillOpen = false
	}
	e.cellFillOpened = false
}

// tableToolbar floats over a selected row or column (insert before,
// insert after, background, delete) or over cells selected across (merge,
// split, background).
func (e *Editor) tableToolbar(c *ui.Context, surface ui.Element) {
	bounds := surface.Bounds()
	if id, r, ok := e.cellRange(); ok && e.focused && !e.dragging {
		e.cellToolbar(c, bounds, id, r)
		return
	}
	d, ok := e.selectedLine()
	if !ok {
		e.tableDanger, e.cellFillOpen = false, false
		return
	}
	sel := e.tableSel
	x, y := d.cols[sel.index], d.rect.Y-tableBarGap-tableBar-46
	before, after, remove := i18n.T("在左侧插入列"), i18n.T("在右侧插入列"), i18n.T("删除列")
	beforeIcon, afterIcon := iconInsertLeft, iconInsertRight
	lines := document.CellRange{Row0: 0, Col0: sel.index, Row1: len(d.rows) - 2, Col1: sel.index}
	if sel.rows {
		x, y = d.rect.X, d.rows[sel.index]-46
		before, after, remove = i18n.T("在上方插入行"), i18n.T("在下方插入行"), i18n.T("删除行")
		beforeIcon, afterIcon = iconInsertAbove, iconInsertBelow
		lines = document.CellRange{Row0: sel.index, Col0: 0, Row1: sel.index, Col1: len(d.cols) - 2}
	}
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("table-toolbar")).Absolute().Left(bounds.X + x).Top(bounds.Y + y).Height(40).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			insert := func(at int) {
				id, rows := sel.id, sel.rows
				e.tableSel, e.tableDanger = tableSelection{}, false
				e.Engine.TableInsert(id, rows, at)
				e.sync()
				e.RequestFocus, e.reveal = true, true
			}
			if markButton(c, e.page, "table:before", beforeIcon, before, false).Clicked() {
				insert(sel.index)
			}
			if markButton(c, e.page, "table:after", afterIcon, after, false).Clicked() {
				insert(sel.index + 1)
			}
			e.cellFillButton(c)
			if !sel.rows && e.Doc.CanSortTable(sel.id) {
				// A column sorts the rows below the header by it.
				for _, s := range []struct {
					key, tip string
					icon     *ui.SVG
					desc     bool
				}{{"table:sort-up", i18n.T("按此列升序排序"), iconSortUp, false}, {"table:sort-down", i18n.T("按此列降序排序"), iconSortDown, true}} {
					if markButton(c, e.page, s.key, s.icon, s.tip, false).Clicked() {
						id, col := sel.id, sel.index
						e.tableSel, e.tableDanger = tableSelection{}, false
						e.Engine.SortTable(id, col, s.desc)
						e.sync()
						e.RequestFocus = true
					}
				}
			}
			ui.Box(c).Size(1, 20).Background(c.Theme().Border)
			del := ui.ButtonBase(c.Key("table:delete")).Size(24, 24).Radius(4).Center().Label(remove).Tooltip(remove + " (⌫)").KeepFocus().Cursor(ui.CursorPointer)
			e.tableDanger = del.Hovered()
			if e.tableDanger {
				del.Background(e.page.DangerCell)
			}
			del.Children(func() { ui.Icon(c, iconTrash).FontSize(18).TextColor(e.page.Danger) })
			if del.Clicked() {
				e.deleteTableLine()
			}
		})
		e.cellFillPanel(c, bounds.X+x, bounds.Y+y+44, sel.id, lines, "")
	})
}

// cellToolbar floats over cells selected across: merge, split and their
// background.
func (e *Editor) cellToolbar(c *ui.Context, bounds ui.Rect, id string, r document.CellRange) {
	d, _ := e.tableDecoration(id)
	if len(d.cols) <= r.Col1+1 || len(d.rows) <= r.Row1+1 {
		return
	}
	table, _, _, _ := e.Doc.CellAt(e.Engine.LeafAt(e.Selection().Range.Start).Block.ID)
	if table == nil {
		return
	}
	merged := false
	for ri := r.Row0; ri <= r.Row1; ri++ {
		for ci := r.Col0; ci <= r.Col1; ci++ {
			if cols, rows := document.Span(table.Children[ri].Children[ci]); cols > 1 || rows > 1 {
				merged = true
			}
		}
	}
	single := merged && func() bool {
		cols, rows := document.Span(table.Children[r.Row0].Children[r.Col0])
		return r.Row1-r.Row0+1 == rows && r.Col1-r.Col0+1 == cols
	}()
	x, y := d.cols[r.Col0], d.rows[r.Row0]-46
	current := table.Children[r.Row0].Children[r.Col0].Background
	ui.Overlay(c, func() {
		bar := ui.Row(c.Key("cell-toolbar")).Absolute().Left(bounds.X + x).Top(bounds.Y + y).Height(40).PaddingX(8).Gap(8).AlignItems(ui.Center)
		e.popover(c, bar).Children(func() {
			if !single && markButton(c, e.page, "cell:merge", iconMerge, i18n.T("合并单元格"), false).Clicked() {
				e.Engine.MergeCells(id, r)
				e.sync()
				e.RequestFocus = true
			}
			if merged && markButton(c, e.page, "cell:split", iconSplit, i18n.T("拆分单元格"), false).Clicked() {
				e.Engine.Edit(func() {
					for ri := r.Row0; ri <= r.Row1; ri++ {
						for ci := r.Col0; ci <= r.Col1; ci++ {
							if cell := table.Children[ri].Children[ci]; cell.ColSpan > 1 || cell.RowSpan > 1 {
								e.Engine.SplitCell(cell.ID)
							}
						}
					}
				})
				e.sync()
				e.RequestFocus = true
			}
			e.cellFillButton(c)
		})
		e.cellFillPanel(c, bounds.X+x, bounds.Y+y+44, id, r, current)
	})
}

// MergeSelectedCells merges the cells the selection runs across.
func (e *Editor) MergeSelectedCells() {
	if id, r, ok := e.cellRange(); ok {
		e.Engine.MergeCells(id, r)
		e.sync()
	}
}

// SplitCaretCell splits the merged cell holding the caret.
func (e *Editor) SplitCaretCell() {
	if cell, ok := e.caretCell(); ok {
		e.Engine.SplitCell(cell.ID)
		e.sync()
	}
}

// SortCaretColumn sorts the table holding the caret by the caret's column.
// It reports whether there was a table it could sort.
func (e *Editor) SortCaretColumn(descending bool) bool {
	cell, ok := e.caretCell()
	if !ok {
		return false
	}
	table, _, col, ok := e.Doc.CellAt(cell.ID)
	if !ok || !e.Doc.CanSortTable(table.ID) {
		return false
	}
	e.Engine.SortTable(table.ID, col, descending)
	e.sync()
	return true
}
