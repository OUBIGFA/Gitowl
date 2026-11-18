package editor

import (
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui/markdown/parser"
)

const fragmentFormat transfer.Format = "application/vnd.arcmd.document+json"

func (e *Editor) copySelection(write func(string)) {
	r := e.Selection().Range
	if r.Start == r.End {
		return
	}
	text := e.slice(r.Start, r.End)
	fragment := e.Engine.Fragment()
	fragment.ImportSource = ""
	data, err := fragment.Encode()
	if err != nil {
		write(text)
		return
	}
	if !literalBlock(e.LeafAt(r.Start).Block.Kind) {
		text = trimNewline(ExportMarkdown(fragment))
	}
	e.writeClipboard(fragment, data, text)
}

func trimNewline(s string) string { return strings.TrimSuffix(s, "\n") }

// writeClipboard offers a fragment, encoded as data, with its text,
// Markdown and HTML.
func (e *Editor) writeClipboard(fragment *document.Document, data, text string) {
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(fragmentFormat, []byte(data)),
		transfer.Bytes(transfer.Text, []byte(text)),
		transfer.Bytes(transfer.Markdown, []byte(ExportMarkdown(fragment))),
		transfer.Bytes(transfer.HTML, []byte(ExportHTML(fragment))),
	)))
}

func (e *Editor) pasteSelection(read func() string) {
	if e.ReadOnly {
		return
	}
	clipboard := e.services.ReadClipboardData(fragmentFormat, transfer.FileList, transfer.PNG, transfer.HTML, transfer.Markdown, transfer.Text)
	if data, err := clipboard.Read(fragmentFormat); err == nil && len(data) < 16<<20 {
		if doc, err := document.Decode(string(data)); err == nil {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	// Files copied in Finder go in as dropped at the caret; a picture
	// copied from an app or a screenshot is PNG. Both come with text (the
	// files' names), which is pasted only when the host takes neither.
	if e.dropFiles != nil {
		if files, err := clipboard.Files(); err == nil && len(files) > 0 {
			e.UnmarkText()
			e.dropFiles(files, -1)
			return
		}
	}
	if e.pasteImage != nil {
		if png, err := clipboard.Read(transfer.PNG); err == nil && len(png) > 0 {
			e.UnmarkText()
			e.pasteImage(png)
			return
		}
	}
	text := read()
	if md, err := clipboard.Read(transfer.Markdown); err == nil && len(md) < 8<<20 && len(md) > 0 {
		text = string(md)
	}
	// A complete display formula copied from a chat or Markdown document
	// is math even when the clipboard also offers a styled HTML code block.
	// A formula editor accepts the body alone; other literal contexts keep
	// the text, including its delimiters.
	kind := e.Engine.LeafAt(e.Caret).Block.Kind
	if kind != "code" && kind != "raw" && kind != "cell" && !e.Engine.CurrentStyle().Code {
		if doc := pastedMath(text); doc != nil {
			if kind == "math" {
				if len(doc.Blocks) == 1 && doc.Blocks[0].Kind == "math" {
					e.ReplaceText(nil, doc.Blocks[0].Text())
					return
				}
			} else {
				e.UnmarkText()
				e.Engine.Paste(doc)
				e.sync()
				return
			}
		}
	}
	// Rich text from a browser or another editor keeps its structure;
	// HTML that holds only plain text pastes as the text.
	if data, err := clipboard.Read(transfer.HTML); err == nil && len(data) > 0 && len(data) < 8<<20 {
		if doc := ImportHTML(string(data)); rich(doc) && kind != "code" && kind != "raw" && kind != "math" {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	if !literalBlock(kind) && !e.CurrentStyle().Code && len(text) < 8<<20 {
		// Rows copied from a spreadsheet, as tab-separated lines.
		if doc := tabTable(text); doc != nil && kind != "cell" {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
		if doc := ImportMarkdown(text); rich(doc) {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	e.Buffer.ReplaceText(nil, text)
}

// pastedMath only opts plain clipboard text into Markdown import when a
// complete display formula exists. Unfinished delimiters, currency and
// dollar signs in fenced code keep the ordinary paste behavior.
func pastedMath(source string) *document.Document {
	if len(source) > 8<<20 || !strings.Contains(source, "$$") && !strings.Contains(source, `\[`) {
		return nil
	}
	parsed := parser.Parse(source)
	found := false
	for i := 1; i < parsed.Block(0).Next; i++ {
		b := parsed.Block(i)
		if b.Kind == parser.MathBlock {
			if b.Open {
				return nil
			}
			found = true
		}
	}
	if !found {
		return nil
	}
	return ImportMarkdown(source)
}

// rich reports whether a document holds more than plain paragraphs of
// plain text, worth pasting as structure.
func rich(doc *document.Document) bool {
	for _, b := range doc.Blocks {
		if b.Kind != "paragraph" {
			return true
		}
		for _, r := range b.Runs {
			if r.Style != (document.Style{}) {
				return true
			}
		}
	}
	return false
}
