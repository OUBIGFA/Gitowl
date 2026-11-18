package editor

import (
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Palette hex values mirror internal/app/theme.go's light document theme so
// Markdown files opened in Typora, Obsidian, VS Code or browsers show the
// same colors ArcMD draws natively.
var (
	textColorHex = map[string]string{
		"gray":   "#8F959E",
		"red":    "#D83931",
		"orange": "#DE7802",
		"yellow": "#DC9B04",
		"green":  "#2EA121",
		"blue":   "#245BDB",
		"purple": "#6425D0",
	}

	highlightHex = map[string]string{
		"light-gray":   "#F2F3F5",
		"light-red":    "#FBBFBC",
		"light-orange": "#FFE4C4",
		"light-yellow": "#FFF67A",
		"light-green":  "#CCF3C8",
		"light-blue":   "#D8E4FF",
		"light-purple": "#E2D2FC",
		"medium-gray":  "#E5E6E8",
		"gray":         "#BBBFC4",
		"red":          "#F76964",
		"orange":       "#FFA53D",
		"yellow":       "#FFE928",
		"green":        "#62D256",
		"blue":         "#9EBEFE",
		"purple":       "#C3A3FA",
	}

	cellFillHex = map[string]string{
		"light-gray":   "#F5F6F7",
		"light-red":    "#FEF1F1",
		"light-orange": "#FFF5EB",
		"light-yellow": "#FEFFF0",
		"light-green":  "#F0FBEF",
		"light-blue":   "#F0F4FF",
		"light-purple": "#F6F1FE",
		"medium-gray":  "#F2F3F5",
		"gray":         "#EFF0F1",
		"red":          "#FDE2E2",
		"orange":       "#FEEAD2",
		"yellow":       "#FFFFCC",
		"green":        "#D9F5D6",
		"blue":         "#E1EAFF",
		"purple":       "#ECE2FE",
	}
)

func validStyleName(name string, allowed []string) bool {
	for _, item := range allowed {
		if item == name {
			return true
		}
	}
	return false
}

// spanTagAndAttrs formats a Style's color, highlight and hand-drawn annotation
// as either a standard <mark> element or a <span> with both inline CSS (for
// external Markdown viewers) and lossless data-* attributes (for ArcMD).
func spanTagAndAttrs(s document.Style) (tag, attrs string) {
	if s.Color == "" && s.Highlight == "light-yellow" && s.Annotation == "" {
		return "mark", ""
	}
	if s.Color == "" && s.Highlight == "" && s.Annotation == "" {
		return "", ""
	}
	var css []string
	var data []string
	if hex := textColorHex[s.Color]; hex != "" {
		css = append(css, "color:"+hex)
		data = append(data, `data-color="`+s.Color+`"`)
	}
	if hex := highlightHex[s.Highlight]; hex != "" {
		css = append(css, "background-color:"+hex)
		data = append(data, `data-highlight="`+s.Highlight+`"`)
	}
	if s.Annotation != "" {
		markHex := textColorHex[s.AnnotationColor]
		if markHex == "" {
			markHex = "#D83931"
		}
		switch s.Annotation {
		case "wavy":
			css = append(css, "text-decoration:underline wavy "+markHex)
		case "underline", "line":
			css = append(css, "text-decoration:underline "+markHex)
		case "dotted":
			css = append(css, "text-decoration:underline dotted "+markHex)
		case "double":
			css = append(css, "text-decoration:underline double "+markHex)
		case "strike", "cross":
			css = append(css, "text-decoration:line-through "+markHex)
		case "highlight":
			if s.Highlight == "" {
				bg := "#FFF67A"
				if s.AnnotationColor != "" {
					if h := highlightHex["light-"+s.AnnotationColor]; h != "" {
						bg = h
					}
				}
				css = append(css, "background-color:"+bg)
			}
		case "box", "circle", "bracket":
			css = append(css, "border-bottom:1.5px solid "+markHex)
		}
		data = append(data, `data-mark="`+s.Annotation+`"`)
		if s.AnnotationColor != "" {
			data = append(data, `data-mark-color="`+s.AnnotationColor+`"`)
		}
	}
	var parts []string
	if len(css) > 0 {
		parts = append(parts, `style="`+strings.Join(css, ";")+`"`)
	}
	parts = append(parts, data...)
	return "span", strings.Join(parts, " ")
}

// parseCSSProps splits an inline CSS style attribute into lowercase property pairs.
func parseCSSProps(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]string{}
	for _, decl := range strings.Split(raw, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

func parseHexRGB(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		switch len(hex) {
		case 3, 4:
			rv, e1 := strconv.ParseUint(strings.Repeat(string(hex[0]), 2), 16, 8)
			gv, e2 := strconv.ParseUint(strings.Repeat(string(hex[1]), 2), 16, 8)
			bv, e3 := strconv.ParseUint(strings.Repeat(string(hex[2]), 2), 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				return uint8(rv), uint8(gv), uint8(bv), true
			}
		case 6, 8:
			rv, e1 := strconv.ParseUint(hex[0:2], 16, 8)
			gv, e2 := strconv.ParseUint(hex[2:4], 16, 8)
			bv, e3 := strconv.ParseUint(hex[4:6], 16, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				return uint8(rv), uint8(gv), uint8(bv), true
			}
		}
		return 0, 0, 0, false
	}
	for _, prefix := range []string{"rgba(", "rgb("} {
		if strings.HasPrefix(s, prefix) && strings.HasSuffix(s, ")") {
			inner := s[len(prefix) : len(s)-1]
			parts := strings.Split(inner, ",")
			if len(parts) < 3 {
				return 0, 0, 0, false
			}
			if len(parts) == 4 {
				if a, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64); err == nil && a <= 0.05 {
					return 0, 0, 0, false
				}
			}
			rv, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			gv, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			bv, e3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			if e1 == nil && e2 == nil && e3 == nil {
				return uint8(max(0, min(255, rv))), uint8(max(0, min(255, gv))), uint8(max(0, min(255, bv))), true
			}
		}
	}
	return 0, 0, 0, false
}

var namedTextColors = map[string]string{
	"gray": "gray", "grey": "gray", "slategray": "gray", "darkgray": "gray",
	"red": "red", "crimson": "red", "firebrick": "red", "darkred": "red",
	"orange": "orange", "darkorange": "orange", "coral": "orange",
	"yellow": "yellow", "gold": "yellow", "goldenrod": "yellow",
	"green": "green", "forestgreen": "green", "seagreen": "green", "teal": "green",
	"blue": "blue", "royalblue": "blue", "dodgerblue": "blue", "navy": "blue",
	"purple": "purple", "violet": "purple", "indigo": "purple", "magenta": "purple",
}

var namedHighlights = map[string]string{
	"yellow": "light-yellow", "lightyellow": "light-yellow", "gold": "yellow",
	"red": "light-red", "pink": "light-red", "mistyrose": "light-red",
	"orange": "light-orange", "peachpuff": "light-orange", "moccasin": "light-orange",
	"green": "light-green", "lightgreen": "light-green", "honeydew": "light-green",
	"blue": "light-blue", "lightblue": "light-blue", "aliceblue": "light-blue",
	"purple": "light-purple", "lavender": "light-purple", "plum": "light-purple",
	"gray": "light-gray", "grey": "light-gray", "lightgray": "light-gray", "lightgrey": "light-gray",
}

func matchTextColorCSS(val string) string {
	v := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(val, "!important")))
	if name := namedTextColors[v]; name != "" {
		return name
	}
	for name, hex := range textColorHex {
		if strings.EqualFold(v, hex) {
			return name
		}
	}
	r, g, b, ok := parseHexRGB(v)
	if !ok {
		return ""
	}
	// Ignore near-black and near-white default body ink from pasted rich text.
	hi, lo := max(r, max(g, b)), min(r, min(g, b))
	if hi < 75 && hi-lo < 24 || lo > 235 {
		return ""
	}
	if hi-lo < 20 {
		if hi >= 90 && hi <= 200 {
			return "gray"
		}
		return ""
	}
	return nearestPaletteColor(r, g, b, textColorHex, document.TextColors)
}

func matchHighlightCSS(val string) string {
	v := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(val, "!important")))
	if v == "" || v == "transparent" || v == "none" || v == "inherit" || v == "initial" {
		return ""
	}
	if name := namedHighlights[v]; name != "" {
		return name
	}
	for _, table := range []map[string]string{highlightHex, cellFillHex} {
		for name, hex := range table {
			if strings.EqualFold(v, hex) {
				return name
			}
		}
	}
	r, g, b, ok := parseHexRGB(v)
	if !ok {
		return ""
	}
	// Ignore white page backgrounds.
	if r > 248 && g > 248 && b > 248 {
		return ""
	}
	return nearestPaletteColor(r, g, b, highlightHex, document.TextHighlights)
}

func nearestPaletteColor(r, g, b uint8, palette map[string]string, order []string) string {
	best, bestDist := "", math.MaxFloat64
	for _, name := range order {
		pr, pg, pb, ok := parseHexRGB(palette[name])
		if !ok {
			continue
		}
		dr, dg, db := float64(r)-float64(pr), float64(g)-float64(pg), float64(b)-float64(pb)
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist, best = d, name
		}
	}
	return best
}

// applyNodeRichStyle enriches style from an HTML element's tag, data-*
// attributes and inline CSS.
func applyNodeRichStyle(n *xhtml.Node, style document.Style) document.Style {
	if n.DataAtom == atom.Mark {
		style.Highlight = "light-yellow"
	}
	if c := attr(n, "data-color"); validStyleName(c, document.TextColors) {
		style.Color = c
	}
	if h := attr(n, "data-highlight"); validStyleName(h, document.TextHighlights) {
		style.Highlight = h
	}
	if m := attr(n, "data-mark"); validStyleName(m, document.Annotations) {
		style.Annotation = m
		if mc := attr(n, "data-mark-color"); validStyleName(mc, document.TextColors) {
			style.AnnotationColor = mc
		} else {
			style.AnnotationColor = ""
		}
	}
	if attr(n, "data-nomath") == "1" {
		style.NoMath = true
	}
	props := parseCSSProps(attr(n, "style"))
	if style.Color == "" {
		if c := matchTextColorCSS(props["color"]); c != "" {
			style.Color = c
		}
	}
	if style.Highlight == "" {
		bg := props["background-color"]
		if bg == "" {
			bg = props["background"]
		}
		if h := matchHighlightCSS(bg); h != "" && style.Annotation != "highlight" {
			style.Highlight = h
		}
	}
	return style
}

// canUseMarkdownDelim checks CommonMark §6.2 left/right-flanking delimiter rules
// so emphasis markers touching HTML tags or intraword text never fail to parse;
// when false, the exporter falls back to semantic HTML (<strong>, <em>, <del>).
func canUseMarkdownDelim(inner string, before, after rune, delim string) bool {
	if inner == "" || strings.Contains(inner, "\n\n") {
		return false
	}
	if strings.HasSuffix(inner, `\`) || strings.HasSuffix(inner, strings.Repeat(delim[:1], 1)) && strings.HasSuffix(inner, `\`+delim[:1]) {
		return false
	}
	if delim == "~~" && (strings.HasPrefix(inner, "~") || strings.HasSuffix(inner, "~")) {
		return false
	}
	var first, last rune
	for _, r := range inner {
		first = r
		break
	}
	for _, r := range inner {
		last = r
	}
	isWS := func(r rune) bool { return r == 0 || unicode.IsSpace(r) }
	isPunct := func(r rune) bool { return r != 0 && (unicode.IsPunct(r) || unicode.IsSymbol(r)) }
	leftFlanking := !isWS(first) && (!isPunct(first) || isWS(before) || isPunct(before))
	rightFlanking := !isWS(last) && (!isPunct(last) || isWS(after) || isPunct(after))
	return leftFlanking && rightFlanking
}

// exportHTMLRuns renders runs as semantic HTML (for HTML blocks in Markdown and
// clipboard HTML export), merging shared outer marks across adjacent runs.
func exportHTMLRuns(runs []document.Run, preserveNoMath bool) string {
	runs = document.Compact(runs)
	return renderHTMLRunSlice(runs, preserveNoMath)
}

func renderHTMLRunSlice(runs []document.Run, preserveNoMath bool) string {
	var out strings.Builder
	for i := 0; i < len(runs); {
		r := runs[i]
		kind, val := outerHTMLMark(r.Style)
		if kind == "" {
			s := html.EscapeString(r.Text)
			s = strings.NewReplacer("\u2028", "<br>", "\n", "<br>").Replace(s)
			if r.Style.Code {
				s = "<code>" + s + "</code>"
			}
			if preserveNoMath && r.Style.NoMath && strings.Contains(r.Text, "$") {
				s = `<span data-nomath="1">` + s + "</span>"
			}
			out.WriteString(s)
			i++
			continue
		}
		j := i + 1
		for j < len(runs) {
			k2, v2 := hasHTMLMark(runs[j].Style, kind)
			if !k2 || v2 != val {
				break
			}
			j++
		}
		sub := make([]document.Run, j-i)
		for k := i; k < j; k++ {
			sub[k-i] = clearHTMLMark(runs[k], kind)
		}
		inner := renderHTMLRunSlice(sub, preserveNoMath)
		switch kind {
		case "link":
			if safeHTMLURL(val) {
				out.WriteString(`<a href="` + html.EscapeString(val) + `">` + inner + "</a>")
			} else {
				out.WriteString(inner)
			}
		case "bold":
			out.WriteString("<strong>" + inner + "</strong>")
		case "italic":
			out.WriteString("<em>" + inner + "</em>")
		case "strike":
			out.WriteString("<del>" + inner + "</del>")
		case "underline":
			out.WriteString("<u>" + inner + "</u>")
		case "script":
			out.WriteString("<" + val + ">" + inner + "</" + val + ">")
		case "span":
			tag, attrs := spanTagAndAttrs(r.Style)
			if attrs != "" {
				out.WriteString("<" + tag + " " + attrs + ">" + inner + "</" + tag + ">")
			} else {
				out.WriteString("<" + tag + ">" + inner + "</" + tag + ">")
			}
		}
		i = j
	}
	return out.String()
}

func outerHTMLMark(s document.Style) (kind, val string) {
	switch {
	case s.Link != "":
		return "link", s.Link
	case s.Bold:
		return "bold", "1"
	case s.Italic:
		return "italic", "1"
	case s.Strike:
		return "strike", "1"
	case s.Underline:
		return "underline", "1"
	case s.Script != "":
		return "script", s.Script
	case s.Color != "" || s.Highlight != "" || s.Annotation != "":
		return "span", fmt.Sprintf("%s|%s|%s|%s", s.Color, s.Highlight, s.Annotation, s.AnnotationColor)
	}
	return "", ""
}

func hasHTMLMark(s document.Style, kind string) (bool, string) {
	switch kind {
	case "link":
		return s.Link != "", s.Link
	case "bold":
		return s.Bold, "1"
	case "italic":
		return s.Italic, "1"
	case "strike":
		return s.Strike, "1"
	case "underline":
		return s.Underline, "1"
	case "script":
		return s.Script != "", s.Script
	case "span":
		on := s.Color != "" || s.Highlight != "" || s.Annotation != ""
		return on, fmt.Sprintf("%s|%s|%s|%s", s.Color, s.Highlight, s.Annotation, s.AnnotationColor)
	}
	return false, ""
}

func clearHTMLMark(r document.Run, kind string) document.Run {
	switch kind {
	case "link":
		r.Style.Link = ""
	case "bold":
		r.Style.Bold = false
	case "italic":
		r.Style.Italic = false
	case "strike":
		r.Style.Strike = false
	case "underline":
		r.Style.Underline = false
	case "script":
		r.Style.Script = ""
	case "span":
		r.Style.Color, r.Style.Highlight = "", ""
		r.Style.Annotation, r.Style.AnnotationColor = "", ""
	}
	return r
}

// parseInlineHTMLNodes converts children of an inline/block HTML container
// (such as <p align="center">, <summary>, <td>) into document.Runs, keeping
// <br> as newline and preserving colors, highlights, annotations and marks.
func parseInlineHTMLNodes(parent *xhtml.Node, base document.Style) []document.Run {
	var runs []document.Run
	var walk func(*xhtml.Node, document.Style)
	walk = func(n *xhtml.Node, style document.Style) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case xhtml.TextNode:
				if c.Data != "" {
					runs = append(runs, document.Run{Text: c.Data, Style: style})
				}
			case xhtml.ElementNode:
				next := applyNodeRichStyle(c, styled(c, style))
				next = applyElementTagStyle(c, next)
				switch c.DataAtom {
				case atom.Br:
					runs = append(runs, document.Run{Text: "\n", Style: style})
				case atom.Script, atom.Style:
				default:
					walk(c, next)
				}
			}
		}
	}
	walk(parent, base)
	return document.Compact(runs)
}

func applyElementTagStyle(n *xhtml.Node, style document.Style) document.Style {
	switch n.DataAtom {
	case atom.B, atom.Strong:
		style.Bold = true
	case atom.I, atom.Em, atom.Cite:
		style.Italic = true
	case atom.U, atom.Ins:
		style.Underline = true
	case atom.S, atom.Del, atom.Strike:
		style.Strike = true
	case atom.Sup:
		style.Script = "sup"
	case atom.Sub:
		style.Script = "sub"
	case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
		style.Code = true
	case atom.A:
		if href := attr(n, "href"); href != "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(href)), "javascript:") {
			style.Link = href
		}
	}
	return style
}

func parseInlineHTMLFragment(fragment string) []document.Run {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Span, Data: "span"}
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return document.Compact([]document.Run{{Text: html.UnescapeString(stripTags(fragment))}})
	}
	wrap := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: atom.Span, Data: "span"}
	for _, n := range nodes {
		wrap.AppendChild(n)
	}
	runs := parseInlineHTMLNodes(wrap, document.Style{})
	if len(runs) == 0 {
		return []document.Run{{Text: ""}}
	}
	return runs
}

type htmlStyleFrame struct {
	tag   string
	style document.Style
}

// applyInlineHTMLToken handles an inline HTML tag inside Markdown text,
// maintaining a tag stack so nested <span>, <mark>, <strong>, <u>, <sup>, <a>
// open and close cleanly. It reports whether raw was consumed as markup (with
// optional replacement text such as "\n" for <br>).
func applyInlineHTMLToken(raw string, stack *[]htmlStyleFrame, style *document.Style) (handled bool, emit string) {
	z := xhtml.NewTokenizer(strings.NewReader(raw))
	tt := z.Next()
	switch tt {
	case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
		tok := z.Token()
		name := strings.ToLower(tok.Data)
		if tok.DataAtom == atom.Br {
			return true, "\n"
		}
		node := &xhtml.Node{Type: xhtml.ElementNode, DataAtom: tok.DataAtom, Data: name, Attr: tok.Attr}
		if tok.DataAtom == atom.Span && (attr(node, "data-emoji") != "" || attr(node, "data-align") != "") {
			return false, ""
		}
		switch tok.DataAtom {
		case atom.Span, atom.Mark, atom.Font,
			atom.B, atom.Strong, atom.I, atom.Em, atom.Cite,
			atom.U, atom.Ins, atom.S, atom.Del, atom.Strike,
			atom.Sup, atom.Sub, atom.Code, atom.Kbd, atom.Samp, atom.Tt, atom.A:
			if tt == xhtml.StartTagToken {
				*stack = append(*stack, htmlStyleFrame{tag: name, style: *style})
				next := applyNodeRichStyle(node, styled(node, *style))
				next = applyElementTagStyle(node, next)
				*style = next
			}
			return true, ""
		}
	case xhtml.EndTagToken:
		tok := z.Token()
		name := strings.ToLower(tok.Data)
		switch tok.DataAtom {
		case atom.Span, atom.Mark, atom.Font,
			atom.B, atom.Strong, atom.I, atom.Em, atom.Cite,
			atom.U, atom.Ins, atom.S, atom.Del, atom.Strike,
			atom.Sup, atom.Sub, atom.Code, atom.Kbd, atom.Samp, atom.Tt, atom.A:
			for idx := len(*stack) - 1; idx >= 0; idx-- {
				if (*stack)[idx].tag == name {
					*style = (*stack)[idx].style
					*stack = (*stack)[:idx]
					return true, ""
				}
			}
			// Unmatched closing tag still clears its own mark rather than leaking markup.
			switch tok.DataAtom {
			case atom.Sup, atom.Sub:
				style.Script = ""
			case atom.U, atom.Ins:
				style.Underline = false
			case atom.B, atom.Strong:
				style.Bold = false
			case atom.I, atom.Em, atom.Cite:
				style.Italic = false
			case atom.S, atom.Del, atom.Strike:
				style.Strike = false
			case atom.Mark:
				style.Highlight = ""
			case atom.Span, atom.Font:
				style.Color, style.Highlight = "", ""
				style.Annotation, style.AnnotationColor = "", ""
			case atom.A:
				style.Link = ""
			case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
				style.Code = false
			}
			return true, ""
		}
	}
	return false, ""
}
