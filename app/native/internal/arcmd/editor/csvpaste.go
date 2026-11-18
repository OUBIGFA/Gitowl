package editor

// Tables from delimited text: rows copied from a spreadsheet arrive as
// tab-separated lines, which paste as a table; comma-separated text
// becomes one through the command bar, since prose has commas too.

import (
	"encoding/csv"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

// parseDelimited reads text as rows of fields separated by sep, quoted as
// CSV quotes them. It needs two rows or more, two fields or more in the
// first, and every row as wide as the first (strict, for tabs) or any
// width (else).
func parseDelimited(text string, sep rune, strict bool) ([][]string, bool) {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" || len(text) > 4<<20 {
		return nil, false
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 || len(rows[0]) < 2 || len(rows) > 2000 || len(rows[0]) > 64 {
		return nil, false
	}
	if strict {
		for _, row := range rows {
			if len(row) != len(rows[0]) {
				return nil, false
			}
		}
	}
	return rows, true
}

// tabTable is the table tab-separated text makes, or nil when it is not
// spreadsheet rows.
func tabTable(text string) *document.Document {
	if !strings.Contains(text, "\t") {
		return nil
	}
	rows, ok := parseDelimited(text, '\t', true)
	if !ok {
		return nil
	}
	doc := document.New()
	doc.Blocks = []*document.Block{document.DelimitedTable(rows)}
	return doc
}

// PasteAsTable pastes the clipboard's text as a table, its fields
// separated by tabs, commas or semicolons, whichever makes one. It reports
// whether there was a table to paste.
func (e *Editor) PasteAsTable() bool {
	if e.ReadOnly || e.services == (ui.Services{}) {
		return false
	}
	text := e.services.ReadClipboard()
	for _, sep := range []rune{'\t', ',', ';'} {
		if rows, ok := parseDelimited(text, sep, false); ok {
			doc := document.New()
			doc.Blocks = []*document.Block{document.DelimitedTable(rows)}
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			e.reveal = true
			return true
		}
	}
	return false
}
