package document

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// CanSortTable reports whether table id can be sorted: it has rows below
// its header and no merged cells, which rows moving apart would break.
func (d *Document) CanSortTable(id string) bool {
	for _, b := range d.Blocks {
		if b.ID == id && b.Kind == "table" {
			return len(b.Children) > 2 && CoveredCells(b) == nil
		}
	}
	return false
}

// SortTable orders the rows of table id below its header by the text of
// column col, as one step of the history: numbers by value, other text in
// natural order (file 2 before file 10), ignoring case; empty cells last
// either way. The sort is stable.
func (e *Engine) SortTable(id string, col int, descending bool) {
	if !e.Doc.CanSortTable(id) {
		return
	}
	e.Edit(func() {
		parent, index := e.find(id)
		if parent == nil {
			return
		}
		table := (*parent)[index]
		if col < 0 || col >= len(table.Children[0].Children) {
			return
		}
		body := table.Children[1:]
		sort.SliceStable(body, func(i, j int) bool {
			a, b := strings.TrimSpace(body[i].Children[col].Text()), strings.TrimSpace(body[j].Children[col].Text())
			if (a == "") != (b == "") {
				return b == ""
			}
			if descending {
				return naturalLess(b, a)
			}
			return naturalLess(a, b)
		})
		e.FocusBlock(table.ID)
	})
}

// naturalLess compares as people sort: whole numbers by value, the rest
// by letter, ignoring case.
func naturalLess(a, b string) bool {
	if x, err := strconv.ParseFloat(strings.ReplaceAll(a, ",", ""), 64); err == nil {
		if y, err := strconv.ParseFloat(strings.ReplaceAll(b, ",", ""), 64); err == nil {
			return x < y
		}
	}
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}

// DelimitedTable builds a table from rows of fields, the first row its
// header. Ragged rows are padded with empty cells.
func DelimitedTable(rows [][]string) *Block {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	table := Table(len(rows), cols)
	for i, r := range rows {
		for j, field := range r {
			table.Children[i].Children[j].Runs = []Run{{Text: strings.ReplaceAll(field, "\r\n", "\n")}}
		}
	}
	return table
}
