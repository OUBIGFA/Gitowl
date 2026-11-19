package editor

import (
	"strconv"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ImportHTML turns HTML copied from a browser, Feishu, Notion or a word
// processor into a document: headings, paragraphs, lists and tasks, quotes,
// code, tables, rules and images, with bold, italic, underline, strike,
// code and links. Styles, scripts and what ArcMD cannot show are left out;
// the text always survives.
func ImportHTML(source string) *document.Document {
	root, err := html.Parse(strings.NewReader(source))
	doc := document.New()
	doc.Blocks = nil
	if err != nil {
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", strings.TrimSpace(source))}
		return doc
	}
	h := &htmlImporter{}
	h.walk(root, document.Style{})
	h.flush()
	doc.Blocks = h.blocks
	if len(doc.Blocks) == 0 {
		doc.Blocks = []*document.Block{document.NewBlock("paragraph", "")}
	}
	return doc
}

type htmlImporter struct {
	blocks []*document.Block
	// cur is the block text goes in, made on the first text.
	cur *document.Block
	// kind, level, indent and checked are what a new block becomes, set by
	// the elements around the text.
	kind    string
	level   int
	indent  int
	checked *bool
	// lists are the kinds of the lists the text is in, innermost last.
	lists []string
	pre   bool
}

// flush ends the block being written; an empty one is dropped.
func (h *htmlImporter) flush() {
	if h.cur == nil {
		return
	}
	b := h.cur
	h.cur = nil
	b.Runs = document.Compact(b.Runs)
	if b.Kind != "code" {
		// Collapse the whitespace HTML ignores; trim the ends.
		for i := range b.Runs {
			b.Runs[i].Text = collapseSpace(b.Runs[i].Text)
		}
		if len(b.Runs) > 0 {
			b.Runs[0].Text = strings.TrimLeft(b.Runs[0].Text, " ")
			last := len(b.Runs) - 1
			b.Runs[last].Text = strings.TrimRight(b.Runs[last].Text, " ")
		}
		b.Runs = document.Compact(b.Runs)
	} else {
		text := strings.TrimSuffix(b.Text(), "\n")
		b.Runs = []document.Run{{Text: text}}
	}
	if len(b.Runs) == 0 && b.Kind != "task" && b.Kind != "bullet" && b.Kind != "numbered" {
		return
	}
	if len(b.Runs) == 0 {
		b.Runs = []document.Run{{Text: ""}}
	}
	h.blocks = append(h.blocks, b)
}

func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// text adds text in the style to the block being written, starting one.
func (h *htmlImporter) text(s string, style document.Style) {
	if h.cur == nil {
		if !h.pre && strings.TrimSpace(strings.ReplaceAll(s, " ", " ")) == "" {
			return
		}
		kind := h.kind
		if kind == "" {
			kind = "paragraph"
		}
		h.cur = &document.Block{ID: document.NewID(), Kind: kind, Level: h.level, Indent: h.indent}
		if h.checked != nil {
			h.cur.Kind, h.cur.Checked = "task", *h.checked
		}
	}
	h.cur.Runs = append(h.cur.Runs, document.Run{Text: s, Style: style})
}

// block runs inside with what new blocks become set, flushing around it.
func (h *htmlImporter) block(kind string, level int, inside func()) {
	h.flush()
	saved, savedLevel := h.kind, h.level
	h.kind, h.level = kind, level
	inside()
	h.flush()
	h.kind, h.level = saved, savedLevel
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// styled reads the inline styles word processors write instead of tags.
func styled(n *html.Node, style document.Style) document.Style {
	css := strings.ToLower(strings.ReplaceAll(attr(n, "style"), " ", ""))
	if css == "" {
		return style
	}
	hasDataMark := attr(n, "data-mark") != ""
	if strings.Contains(css, "font-weight:bold") || strings.Contains(css, "font-weight:700") || strings.Contains(css, "font-weight:600") {
		style.Bold = true
	}
	if strings.Contains(css, "font-style:italic") {
		style.Italic = true
	}
	if !hasDataMark && (strings.Contains(css, "text-decoration:underline") || strings.Contains(css, "text-decoration-line:underline")) {
		style.Underline = true
	}
	if !hasDataMark && strings.Contains(css, "line-through") {
		style.Strike = true
	}
	if strings.Contains(css, "vertical-align:super") {
		style.Script = "sup"
	}
	if strings.Contains(css, "vertical-align:sub") {
		style.Script = "sub"
	}
	return style
}

func (h *htmlImporter) children(n *html.Node, style document.Style) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		h.walk(c, style)
	}
}

func (h *htmlImporter) walk(n *html.Node, style document.Style) {
	switch n.Type {
	case html.TextNode:
		h.text(n.Data, style)
		return
	case html.DocumentNode:
		h.children(n, style)
		return
	case html.ElementNode:
	default:
		return
	}
	style = applyNodeRichStyle(n, styled(n, style))
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Meta, atom.Link, atom.Noscript, atom.Template, atom.Button, atom.Svg:
		return
	case atom.B, atom.Strong:
		style.Bold = true
	case atom.I, atom.Em, atom.Cite:
		style.Italic = true
	case atom.Sup:
		style.Script = "sup"
	case atom.Sub:
		style.Script = "sub"
	case atom.U, atom.Ins:
		style.Underline = true
	case atom.S, atom.Del, atom.Strike:
		style.Strike = true
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		if !h.pre {
			style.Code = true
		}
	case atom.A:
		// Pasted HTML uses the same allowed schemes as exported links.
		if href := attr(n, "href"); href != "" && safeHTMLURL(href) {
			style.Link = href
		}
	case atom.Br:
		if h.pre {
			h.text("\n", style)
			return
		}
		// A line break inside a paragraph begins another, as ArcMD's
		// paragraphs hold no line breaks.
		h.flush()
		return
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.Data[1] - '0')
		h.block("heading", level, func() { h.children(n, style) })
		return
	case atom.P, atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Main, atom.Aside, atom.Nav, atom.Figure, atom.Figcaption, atom.Dt, atom.Dd, atom.Address, atom.Center:
		if n.DataAtom == atom.Div && hasHTMLClass(n, "arcmd-columns") {
			h.flush()
			if cols := columnsOfHTML(n); cols != nil {
				h.blocks = append(h.blocks, cols)
				return
			}
		}
		if h.kind == "quote" || h.kind == "bullet" || h.kind == "numbered" {
			// Paragraphs inside a quote or a list item stay what they are in.
			h.flush()
			h.children(n, style)
			h.flush()
			return
		}
		h.block(h.kind, h.level, func() { h.children(n, style) })
		return
	case atom.Blockquote:
		h.block("quote", 0, func() { h.children(n, style) })
		return
	case atom.Pre:
		h.flush()
		h.pre = true
		h.block("code", 0, func() {
			h.cur = &document.Block{ID: document.NewID(), Kind: "code", Language: codeLanguage(n)}
			h.children(n, document.Style{})
		})
		h.pre = false
		return
	case atom.Hr:
		h.flush()
		h.blocks = append(h.blocks, document.NewBlock("rule", ""))
		return
	case atom.Img:
		h.flush()
		src := attr(n, "src")
		if src == "" || strings.HasPrefix(src, "data:") {
			if alt := attr(n, "alt"); alt != "" {
				h.text(alt, style)
			}
			return
		}
		// A picture elsewhere is a link to it: ArcMD shows pictures kept
		// beside the document.
		label := attr(n, "alt")
		if label == "" {
			label = i18n.T("图片")
		}
		h.text(label, document.Style{Link: src})
		h.flush()
		return
	case atom.Ul, atom.Ol, atom.Menu:
		kind := "bullet"
		if n.DataAtom == atom.Ol {
			kind = "numbered"
		}
		h.flush()
		h.lists = append(h.lists, kind)
		savedIndent := h.indent
		h.indent = min(12, len(h.lists)-1)
		h.children(n, style)
		h.flush()
		h.lists = h.lists[:len(h.lists)-1]
		h.indent = savedIndent
		return
	case atom.Li:
		kind := "bullet"
		if len(h.lists) > 0 {
			kind = h.lists[len(h.lists)-1]
		}
		h.flush()
		saved, savedChecked := h.kind, h.checked
		h.kind, h.checked = kind, taskState(n)
		h.children(n, style)
		h.flush()
		h.kind, h.checked = saved, savedChecked
		return
	case atom.Input:
		return
	case atom.Table:
		h.flush()
		if t := tableOf(n); t != nil {
			h.blocks = append(h.blocks, t)
		}
		return
	}
	h.children(n, style)
}

// taskState is whether a list item is a task, by the check box it starts
// with, and whether it is done.
func taskState(li *html.Node) *bool {
	var found *bool
	var look func(*html.Node, int)
	look = func(n *html.Node, depth int) {
		for c := n.FirstChild; c != nil && found == nil && depth < 3; c = c.NextSibling {
			if c.Type == html.ElementNode && c.DataAtom == atom.Input && strings.EqualFold(attr(c, "type"), "checkbox") {
				checked := false
				for _, a := range c.Attr {
					if strings.EqualFold(a.Key, "checked") {
						checked = true
					}
				}
				found = &checked
				return
			}
			if c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol) {
				continue
			}
			look(c, depth+1)
		}
	}
	look(li, 0)
	return found
}

func hasHTMLClass(n *html.Node, want string) bool {
	for _, cl := range strings.Fields(attr(n, "class")) {
		if strings.EqualFold(cl, want) {
			return true
		}
	}
	return false
}

func columnsOfHTML(n *html.Node) *document.Block {
	var cols []*document.Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Div {
			continue
		}
		inner := &htmlImporter{}
		inner.children(c, document.Style{})
		inner.flush()
		children := inner.blocks
		if len(children) == 0 {
			children = []*document.Block{document.NewBlock("paragraph", "")}
		}
		cols = append(cols, &document.Block{
			ID:       document.NewID(),
			Kind:     "column",
			Children: children,
		})
	}
	if len(cols) < 2 || len(cols) > 5 {
		return nil
	}
	return &document.Block{
		ID:       document.NewID(),
		Kind:     "columns",
		Widths:   parseDataWidthsAttr(attr(n, "data-widths"), len(cols)),
		Children: cols,
	}
}

// codeLanguage reads a code block's language from its class, as
// "language-go" or "lang-go".
func codeLanguage(pre *html.Node) string {
	for n := pre; n != nil; n = n.FirstChild {
		for _, class := range strings.Fields(attr(n, "class")) {
			for _, prefix := range []string{"language-", "lang-"} {
				if lang, ok := strings.CutPrefix(class, prefix); ok {
					return strings.ToLower(lang)
				}
			}
		}
		if n.FirstChild == nil || n.FirstChild.Type != html.ElementNode {
			break
		}
	}
	return ""
}

// tableOf makes a table block of an HTML table, its cells' text with its
// styles; rows are padded to the widest.
func tableOf(n *html.Node) *document.Block {
	return tableOfMode(n, false)
}

func tableOfMode(n *html.Node, exactWhitespace bool) *document.Block {
	var colWidths []float32
	var rawRows [][]*html.Node
	var collect func(*html.Node)
	collect = func(cur *html.Node) {
		for c := cur.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Colgroup:
				for col := c.FirstChild; col != nil; col = col.NextSibling {
					if col.Type == html.ElementNode && col.DataAtom == atom.Col {
						colWidths = append(colWidths, parseHTMLWidth(col))
					}
				}
			case atom.Col:
				colWidths = append(colWidths, parseHTMLWidth(c))
			case atom.Tr:
				var tds []*html.Node
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						tds = append(tds, td)
					}
				}
				if len(tds) > 0 {
					rawRows = append(rawRows, tds)
				}
			case atom.Table:
				// A table inside a cell is not followed.
			default:
				collect(c)
			}
		}
	}
	collect(n)
	if len(rawRows) == 0 {
		return nil
	}

	occupied := map[[2]int]bool{}
	placed := map[[2]int]*document.Block{}
	numRows := len(rawRows)
	numCols := 0

	for r, tds := range rawRows {
		c := 0
		for _, td := range tds {
			for occupied[[2]int{r, c}] {
				c++
			}
			colSpan := parseSpanAttr(attr(td, "colspan"))
			rowSpan := parseSpanAttr(attr(td, "rowspan"))
			// Clamp spans that would collide with an already occupied cell.
			for dc := 1; dc < colSpan; dc++ {
				if occupied[[2]int{r, c + dc}] {
					colSpan = dc
					break
				}
			}
			cell := document.NewBlock("cell", "")
			if exactWhitespace {
				if runs := parseInlineHTMLNodes(td, document.Style{}); len(runs) > 0 {
					cell.Runs = runs
				}
			} else {
				inner := &htmlImporter{kind: "paragraph"}
				inner.children(td, document.Style{})
				inner.flush()
				var runs []document.Run
				for i, b := range inner.blocks {
					if i > 0 {
						runs = append(runs, document.Run{Text: "\n"})
					}
					runs = append(runs, b.Runs...)
				}
				if runs = document.Compact(runs); len(runs) > 0 {
					cell.Runs = runs
				}
			}
			if align := parseBlockAlign(td); align != "" {
				cell.Align = align
			}
			if bg := parseCellBackground(td); bg != "" {
				cell.Background = bg
			}
			if colSpan > 1 {
				cell.ColSpan = colSpan
			}
			if rowSpan > 1 {
				cell.RowSpan = rowSpan
			}
			placed[[2]int{r, c}] = cell
			for dr := 0; dr < rowSpan; dr++ {
				for dc := 0; dc < colSpan; dc++ {
					occupied[[2]int{r + dr, c + dc}] = true
				}
			}
			numRows = max(numRows, r+rowSpan)
			numCols = max(numCols, c+colSpan)
			c += colSpan
		}
	}
	if numCols == 0 {
		return nil
	}
	table := document.Table(numRows, numCols)
	for pos, cell := range placed {
		table.Children[pos[0]].Children[pos[1]] = cell
	}
	if len(colWidths) == numCols {
		valid := true
		for _, w := range colWidths {
			if !(w > 0 && w <= 10000) {
				valid = false
				break
			}
		}
		if valid {
			table.Widths = colWidths
		}
	}
	return table
}

func parseSpanAttr(val string) int {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < 1 {
		return 1
	}
	return min(64, n)
}

func parseHTMLWidth(n *html.Node) float32 {
	raw := strings.TrimSpace(attr(n, "width"))
	if raw == "" {
		raw = parseCSSProps(attr(n, "style"))["width"]
	}
	raw = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "px")
	if raw == "" || strings.HasSuffix(raw, "%") {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 32)
	if err != nil || v <= 0 || v > 10000 {
		return 0
	}
	return float32(v)
}

func parseBlockAlign(n *html.Node) string {
	a := strings.ToLower(strings.TrimSpace(attr(n, "align")))
	if a == "" {
		a = strings.ToLower(strings.TrimSpace(parseCSSProps(attr(n, "style"))["text-align"]))
	}
	switch a {
	case "center", "right":
		return a
	case "left":
		return ""
	}
	return ""
}

func parseCellBackground(n *html.Node) string {
	if bg := attr(n, "data-bg"); validStyleName(bg, document.TextHighlights) {
		return bg
	}
	props := parseCSSProps(attr(n, "style"))
	raw := props["background-color"]
	if raw == "" {
		raw = props["background"]
	}
	if raw == "" {
		raw = attr(n, "bgcolor")
	}
	return matchHighlightCSS(raw)
}
