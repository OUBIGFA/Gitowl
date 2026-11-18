package editor

import (
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
)

func isSafeLangClass(lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	for i := 0; i < len(lang); i++ {
		c := lang[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

func safeHTMLURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto", "file", "tel":
		return true
	}
	return false
}

// ExportHTML supplies semantic clipboard content without depending on a web
// runtime. Escaping also keeps raw Markdown/TeX and untrusted links inert.
func ExportHTML(doc *document.Document) string {
	var blocks func([]*document.Block) string
	runs := func(rs []document.Run) string {
		return exportHTMLRuns(rs, false)
	}
	blocks = func(bs []*document.Block) string {
		var out strings.Builder
		for i := 0; i < len(bs); i++ {
			b := bs[i]
			body := runs(b.Runs)
			tag := "p"
			switch b.Kind {
			case "heading":
				tag = fmt.Sprintf("h%d", max(1, min(6, b.Level)))
			case "quote", "callout":
				tag = "blockquote"
			case "bullet", "numbered", "task":
				var list func(int)
				list = func(depth int) {
					kind := bs[i].Kind
					listTag := "ul"
					if kind == "numbered" {
						listTag = "ol"
					}
					out.WriteString("<" + listTag + ">")
					for i < len(bs) && bs[i].Indent == depth && bs[i].Kind == kind {
						item := bs[i]
						out.WriteString("<li>")
						if item.Kind == "task" {
							if item.Checked {
								out.WriteString("☑ ")
							} else {
								out.WriteString("☐ ")
							}
						}
						out.WriteString(runs(item.Runs))
						i++
						for i < len(bs) && isHTMLList(bs[i]) && bs[i].Indent > depth {
							list(bs[i].Indent)
						}
						out.WriteString("</li>")
					}
					out.WriteString("</" + listTag + ">")
				}
				list(b.Indent)
				i--
				continue
			case "code":
				classAttr := ""
				if isSafeLangClass(b.Language) {
					classAttr = ` class="language-` + html.EscapeString(strings.ToLower(strings.TrimSpace(b.Language))) + `"`
				}
				out.WriteString("<pre><code" + classAttr + ">" + html.EscapeString(b.Text()) + "</code></pre>")
				continue
			case "toggle":
				var inner []*document.Block
				for i+1 < len(bs) && bs[i+1].Kind != "heading" && bs[i+1].Indent > b.Indent {
					c := *bs[i+1]
					c.Indent -= b.Indent + 1
					inner = append(inner, &c)
					i++
				}
				open := ""
				if !b.Folded {
					open = " open"
				}
				out.WriteString("<details" + open + "><summary>" + body + "</summary>" + blocks(inner) + "</details>")
				continue
			case "raw", "math":
				tag = "pre"
				body = html.EscapeString(b.Text())
				if b.Kind == "math" {
					body = "$$\n" + body + "\n$$"
				}
			case "rule":
				out.WriteString("<hr>")
				continue
			case "image":
				if safeHTMLURL(b.URL) {
					out.WriteString("<img src=\"" + html.EscapeString(b.URL) + "\" alt=\"" + html.EscapeString(b.Text()) + "\"")
					if b.ImageWidth > 0 {
						fmt.Fprintf(&out, " width=\"%.0f\"", b.ImageWidth)
					}
					out.WriteString(">")
				}
				continue
			case "table":
				out.WriteString("<table><tbody>")
				covered := document.CoveredCells(b)
				for i, row := range b.Children {
					out.WriteString("<tr>")
					for _, cell := range row.Children {
						if covered[cell] {
							continue
						}
						tag := "td"
						if i == 0 {
							tag = "th"
						}
						cols, rows := document.Span(cell)
						fmt.Fprintf(&out, "<%s colspan=\"%d\" rowspan=\"%d\">%s</%s>", tag, cols, rows, runs(cell.Runs), tag)
					}
					out.WriteString("</tr>")
				}
				out.WriteString("</tbody></table>")
				continue
			case "columns":
				ratios := columnRatioStrings(b)
				if len(ratios) == len(b.Children) {
					fmt.Fprintf(&out, `<div class="arcmd-columns" data-widths="%s" style="display:flex;gap:24px">`, strings.Join(ratios, ","))
				} else {
					out.WriteString(`<div class="arcmd-columns" style="display:flex;gap:24px">`)
				}
				for ci, col := range b.Children {
					flexStyle := "flex:1;min-width:0"
					if len(ratios) == len(b.Children) {
						flexStyle = "flex:" + ratios[ci] + ";min-width:0"
					}
					fmt.Fprintf(&out, `<div class="arcmd-column" style="%s">%s</div>`, flexStyle, blocks(col.Children))
				}
				out.WriteString(`</div>`)
				continue
			case "column":
				out.WriteString(blocks(b.Children))
				continue
			case "footnote":
				body = "[" + html.EscapeString(b.NoteLabel) + "] " + body
			}
			out.WriteString("<" + tag + ">" + body + "</" + tag + ">")
		}
		return out.String()
	}
	return "<html><body><!--StartFragment-->" + blocks(doc.Blocks) + "<!--EndFragment--></body></html>"
}

func isHTMLList(b *document.Block) bool {
	return b.Kind == "bullet" || b.Kind == "numbered" || b.Kind == "task"
}
