package document

import "strings"

// Cell styles and merged cells. A merged cell keeps every cell of the grid: the top left one spans the
// others, which stay empty and out of the text, so rows stay rectangular.

// CellRange is a rectangle of a table's cells, inclusive.
type CellRange struct {
	Row0, Col0, Row1, Col1 int
}

// table is block id when it is a table.
func (e *Engine) table(id string) *Block {
	if parent, index := e.find(id); parent != nil && (*parent)[index].Kind == "table" {
		return (*parent)[index]
	}
	return nil
}

// CellAt finds a cell: its table and position.
func (d *Document) CellAt(cellID string) (table *Block, row, col int, ok bool) {
	var walk func([]*Block) bool
	walk = func(blocks []*Block) bool {
		for _, b := range blocks {
			if b.Kind == "table" {
				for r, tr := range b.Children {
					for c, cell := range tr.Children {
						if cell.ID == cellID {
							table, row, col, ok = b, r, c, true
							return true
						}
					}
				}
			}
			if walk(b.Children) {
				return true
			}
		}
		return false
	}
	walk(d.Blocks)
	return
}

// Expand grows r until no merged cell crosses its edge.
func Expand(table *Block, r CellRange) CellRange {
	for {
		grown := r
		for ri, row := range table.Children {
			for ci, cell := range row.Children {
				cols, rows := Span(cell)
				r1, c1 := ri+rows-1, ci+cols-1
				if ri <= grown.Row1 && r1 >= grown.Row0 && ci <= grown.Col1 && c1 >= grown.Col0 {
					grown.Row0, grown.Col0 = min(grown.Row0, ri), min(grown.Col0, ci)
					grown.Row1, grown.Col1 = max(grown.Row1, r1), max(grown.Col1, c1)
				}
			}
		}
		if grown == r {
			return r
		}
		r = grown
	}
}

// SetCellBackground fills the cells of r in table id; "" clears.
func (e *Engine) SetCellBackground(id string, r CellRange, name string) {
	e.Edit(func() {
		table := e.table(id)
		if table == nil {
			return
		}
		for ri := max(0, r.Row0); ri <= min(r.Row1, len(table.Children)-1); ri++ {
			row := table.Children[ri]
			for ci := max(0, r.Col0); ci <= min(r.Col1, len(row.Children)-1); ci++ {
				row.Children[ci].Background = name
			}
		}
	})
}

// MergeCells merges r, grown over merged cells it crosses, into its top
// left cell; their texts join on separate lines.
func (e *Engine) MergeCells(id string, r CellRange) {
	e.Edit(func() {
		table := e.table(id)
		if table == nil {
			return
		}
		r = Expand(table, r)
		if r.Row0 == r.Row1 && r.Col0 == r.Col1 {
			return
		}
		anchor := table.Children[r.Row0].Children[r.Col0]
		runs := anchor.Runs
		for ri := r.Row0; ri <= r.Row1; ri++ {
			for ci := r.Col0; ci <= r.Col1; ci++ {
				cell := table.Children[ri].Children[ci]
				if cell == anchor {
					continue
				}
				if cell.Text() != "" {
					if len(runs) > 0 && strings.TrimSpace(blockText(runs)) != "" {
						runs = append(runs, Run{Text: "\n"})
					}
					runs = append(runs, cell.Runs...)
				}
				cell.Runs, cell.ColSpan, cell.RowSpan = nil, 0, 0
			}
		}
		anchor.Runs = Compact(runs)
		anchor.ColSpan, anchor.RowSpan = r.Col1-r.Col0+1, r.Row1-r.Row0+1
		e.FocusBlock(anchor.ID)
	})
}

// SplitCell undoes the merge of cell id; the cells it covered come back
// empty.
func (e *Engine) SplitCell(cellID string) {
	e.Edit(func() {
		if _, _, _, ok := e.Doc.CellAt(cellID); !ok {
			return
		}
		if parent, index := e.find(cellID); parent != nil {
			cell := (*parent)[index]
			cell.ColSpan, cell.RowSpan = 0, 0
		}
		e.FocusBlock(cellID)
	})
}

// MoveTableRow reorders row from to index to within table id as one history
// step, normalizing any multi-row cell spans afterwards.
func (e *Engine) MoveTableRow(id string, from, to int) {
	e.Edit(func() {
		table := e.table(id)
		if table == nil {
			return
		}
		n := len(table.Children)
		if from < 0 || from >= n {
			return
		}
		to = max(0, min(to, n-1))
		if from == to {
			return
		}
		row := table.Children[from]
		rest := append(append([]*Block(nil), table.Children[:from]...), table.Children[from+1:]...)
		table.Children = append(append(append([]*Block(nil), rest[:to]...), row), rest[to:]...)
		normalizeSpans(table)
		if len(row.Children) > 0 {
			e.FocusBlock(row.Children[0].ID)
		}
	})
}

// MoveTableColumn reorders column from to index to within table id as one
// history step, moving column widths in lockstep and normalizing merged spans.
func (e *Engine) MoveTableColumn(id string, from, to int) {
	e.Edit(func() {
		table := e.table(id)
		if table == nil || len(table.Children) == 0 {
			return
		}
		cols := len(table.Children[0].Children)
		if from < 0 || from >= cols {
			return
		}
		to = max(0, min(to, cols-1))
		if from == to {
			return
		}
		for _, row := range table.Children {
			if len(row.Children) != cols {
				continue
			}
			cell := row.Children[from]
			rest := append(append([]*Block(nil), row.Children[:from]...), row.Children[from+1:]...)
			row.Children = append(append(append([]*Block(nil), rest[:to]...), cell), rest[to:]...)
		}
		if len(table.Widths) == cols {
			w := table.Widths[from]
			rest := append(append([]float32(nil), table.Widths[:from]...), table.Widths[from+1:]...)
			table.Widths = append(append(append([]float32(nil), rest[:to]...), w), rest[to:]...)
		}
		normalizeSpans(table)
		if len(table.Children[0].Children) > to {
			e.FocusBlock(table.Children[0].Children[to].ID)
		}
	})
}

// shiftSpans keeps merged cells whole when a row (rows) or column is
// inserted (delta 1) or deleted (delta -1) at line at, before the change.
// A merge the line passes through grows or shrinks; a deleted line that
// holds a merge's anchor hands it to the next cell down or right.
func shiftSpans(table *Block, rows bool, at, delta int) {
	for ri, row := range table.Children {
		for ci, cell := range row.Children {
			cols, span := Span(cell)
			start, length := ri, span
			if !rows {
				start, length = ci, cols
			}
			if length == 1 {
				continue
			}
			grow := func(n int) {
				if rows {
					cell.RowSpan = n
				} else {
					cell.ColSpan = n
				}
			}
			switch {
			case delta > 0 && start < at && at < start+length:
				grow(length + 1)
			case delta < 0 && start < at && at < start+length:
				grow(length - 1)
			case delta < 0 && start == at:
				// The anchor's line goes: the next cell takes over.
				var next *Block
				if rows && ri+1 < len(table.Children) {
					next = table.Children[ri+1].Children[ci]
				} else if !rows && ci+1 < len(row.Children) {
					next = row.Children[ci+1]
				}
				if next != nil {
					next.Runs, next.Background = cell.Runs, cell.Background
					next.ColSpan, next.RowSpan = cell.ColSpan, cell.RowSpan
					if rows {
						next.RowSpan = length - 1
					} else {
						next.ColSpan = length - 1
					}
				}
				cell.ColSpan, cell.RowSpan = 0, 0
			}
		}
	}
}

// normalizeSpans makes merged cells valid after any reshaping: inside the
// table, not overlapping, and with the cells they cover empty.
func normalizeSpans(table *Block) {
	taken := map[[2]int]bool{}
	for ri, row := range table.Children {
		for ci, cell := range row.Children {
			if taken[[2]int{ri, ci}] {
				cell.ColSpan, cell.RowSpan, cell.Runs = 0, 0, nil
				continue
			}
			cols, rows := Span(cell)
			cols = min(cols, len(row.Children)-ci)
			rows = min(rows, len(table.Children)-ri)
			// Shrink rather than overlap a merge found earlier.
			for rr := ri; rr < ri+rows; rr++ {
				for cc := ci; cc < ci+cols; cc++ {
					if (rr != ri || cc != ci) && taken[[2]int{rr, cc}] {
						cols, rows = 1, 1
					}
				}
			}
			cell.ColSpan, cell.RowSpan = 0, 0
			if cols > 1 {
				cell.ColSpan = cols
			}
			if rows > 1 {
				cell.RowSpan = rows
			}
			for rr := ri; rr < ri+rows; rr++ {
				for cc := ci; cc < ci+cols; cc++ {
					taken[[2]int{rr, cc}] = true
				}
			}
		}
	}
}
