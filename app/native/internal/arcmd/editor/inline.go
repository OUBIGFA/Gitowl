package editor

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"github.com/egoist/mygo/ui"
)

type inlineMathEdit struct {
	blockID    string
	start, end int
	source     string
}

// Inline syntax stays lossless in the model. Only layout replaces it with an
// object, with a reversible UTF-16 mapping shared by caret, selection and PDF.
type inlineToken struct {
	start, end   int
	source, note string
}

type inlineObject struct {
	start, end, index int
	box               *tex.Box
	label             *ui.TextLayout
	sourceLayout      *ui.TextLayout
	source            string
	note              string
	rect              ui.Rect
	scale             float32
	script            bool
	editing           bool
	placeholder       bool
	hasErr            bool
}

type textLayout struct {
	*ui.TextLayout
	objects []inlineObject
}

func inlineTokens(s string) []inlineToken {
	var out []inlineToken
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			_, n := utf8.DecodeRuneInString(s[min(i+1, len(s)):])
			i += 1 + n
			continue
		}
		if strings.HasPrefix(s[i:], "[^") {
			if n := strings.IndexByte(s[i+2:], ']'); n > 0 {
				z := i + 2 + n + 1
				label := s[i+2 : z-1]
				if !strings.ContainsAny(label, " \t\r\n[") {
					out = append(out, inlineToken{i, z, "", label})
					i = z
					continue
				}
			}
		}
		if s[i] != '$' || i+1 >= len(s) || s[i+1] == '$' || i > 0 && s[i-1] == '$' {
			i++
			continue
		}
		first, _ := utf8.DecodeRuneInString(s[i+1:])
		if unicode.IsSpace(first) {
			i++
			continue
		}
		z := i + 1
		for z < len(s) && s[z] != '\n' {
			if s[z] == '\\' {
				z += min(2, len(s)-z)
				continue
			}
			if s[z] == '$' {
				break
			}
			z++
		}
		if z >= len(s) || s[z] != '$' {
			i++
			continue
		}
		last, _ := utf8.DecodeLastRuneInString(s[i+1 : z])
		if z == i+1 || unicode.IsSpace(last) || z+1 < len(s) && (s[z+1] >= '0' && s[z+1] <= '9' || s[z+1] == '$') {
			i++
			continue
		}
		out = append(out, inlineToken{i, z + 1, s[i+1 : z], ""})
		i = z + 1
	}
	return out
}

func literalBlock(kind string) bool {
	return kind == "code" || kind == "raw" || kind == "math" || kind == "image"
}

func tokenAllowed(b *document.Block, a, z int) bool {
	if literalBlock(b.Kind) {
		return false
	}
	for _, r := range document.SliceRuns(b.Runs, a, z) {
		if r.Style.Code || r.Style.Link != "" || r.Style.NoMath {
			return false
		}
	}
	return true
}

func scanDollarSpan(s string, openByte int) (closeByte int, ok bool) {
	if openByte < 0 || openByte >= len(s) || s[openByte] != '$' {
		return 0, false
	}
	if openByte > 0 && (s[openByte-1] == '$' || s[openByte-1] == '\\') {
		return 0, false
	}
	z := openByte + 1
	for z < len(s) && s[z] != '\n' {
		if s[z] == '\\' {
			z += min(2, len(s)-z)
			continue
		}
		if s[z] == '$' {
			if z+1 < len(s) && s[z+1] == '$' {
				return 0, false
			}
			return z, true
		}
		z++
	}
	return 0, false
}

func (e *Editor) watchInline() {
	e.collectNotes()
	prev := e.inlineMath
	e.inlineEditing = ""
	e.inlineMath = inlineMathEdit{}
	if !e.focused || e.ReadOnly || e.printing {
		return
	}
	lo, hi := min(e.Anchor, e.Caret), max(e.Anchor, e.Caret)
	l := e.LeafAt(lo)
	if e.LeafAt(hi).Block.ID != l.Block.ID || literalBlock(l.Block.Kind) {
		return
	}
	s := l.Block.Text()
	atLo, atHi := lo-l.Start, hi-l.Start
	activate := func(a, z int, src string) {
		e.inlineMath = inlineMathEdit{blockID: l.Block.ID, start: a, end: z, source: src}
		e.inlineEditing = fmt.Sprintf("%s:%d:%d:%s", l.Block.ID, a, z, src)
	}
	for _, t := range inlineTokens(s) {
		a, z := document.Len(s[:t.start]), document.Len(s[:t.end])
		if t.note == "" && atLo > a && atHi < z && tokenAllowed(l.Block, a, z) {
			activate(a, z, t.source)
			return
		}
	}
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			if p.open != "$" || p.close != "$" || p.start < l.Start || p.end+1 > l.End {
				continue
			}
			a, z := p.start-l.Start, p.end+1-l.Start
			if atLo <= a || atHi >= z || !tokenAllowed(l.Block, a, z) {
				continue
			}
			ba, bz := document.ByteOffset(s, a), document.ByteOffset(s, z)
			if ba < len(s) && bz <= len(s) && bz-ba >= 2 && s[ba] == '$' && s[bz-1] == '$' && !strings.ContainsAny(s[ba+1:bz-1], "$\n") {
				activate(a, z, s[ba+1:bz-1])
				return
			}
		}
	}
	if prev.blockID == l.Block.ID && prev.start >= 0 && prev.start < document.Len(s) {
		ba := document.ByteOffset(s, prev.start)
		if bz, ok := scanDollarSpan(s, ba); ok {
			a, z := prev.start, document.Len(s[:bz+1])
			if z-a >= 2 && atLo > a && atHi < z && tokenAllowed(l.Block, a, z) {
				activate(a, z, s[ba+1:bz])
				return
			}
		}
	}
}

func sliceSpans(spans []ui.Span, a, z int) []ui.Span {
	var out []ui.Span
	at := 0
	for _, s := range spans {
		n := document.Len(s.Text)
		lo, hi := max(0, a-at), min(n, z-at)
		if hi > lo {
			s.Text = s.Text[document.ByteOffset(s.Text, lo):document.ByteOffset(s.Text, hi)]
			out = append(out, s)
		}
		at += n
	}
	return out
}

func (e *Editor) buildEditingInlineMath(b *document.Block, text string, font ui.Font, shape ui.TextShape) (inlineObject, bool) {
	if e.printing || e.inlineEditing == "" || e.inlineMath.blockID != b.ID {
		return inlineObject{}, false
	}
	a, z := e.inlineMath.start, e.inlineMath.end
	if a < 0 || z > document.Len(text) || z-a < 2 {
		return inlineObject{}, false
	}
	raw := text[document.ByteOffset(text, a):document.ByteOffset(text, z)]
	src := strings.TrimSpace(e.inlineMath.source)
	obj := inlineObject{
		start:        a,
		end:          z,
		source:       e.inlineMath.source,
		sourceLayout: ui.ShapeText(raw, ui.Font{Family: "monospace", Size: 13}, 0),
		scale:        1,
		editing:      true,
	}
	validKey := fmt.Sprintf("%s:%d", b.ID, a)
	if src == "" {
		box := tex.Box{W: max(18, font.Size*1.15), Ascent: font.Size * 0.78, Descent: font.Size * 0.22}
		obj.box = &box
		obj.placeholder = true
		return obj, true
	}
	box, err := tex.LayoutInline(src, font.Size*1.1, e.measureMath)
	if err == nil {
		obj.box = &box
		obj.scale = min(float32(1), max(1, shape.Width-30)/max(1, box.W))
		e.inlineValidID = validKey
		e.inlineValidBox = box
		return obj, true
	}
	obj.hasErr = true
	if e.inlineValidID == validKey && e.inlineValidBox.W > 0 {
		prev := e.inlineValidBox
		obj.box = &prev
		obj.scale = min(float32(1), max(1, shape.Width-30)/max(1, prev.W))
		return obj, true
	}
	fallback := tex.Box{W: max(22, font.Size*1.3), Ascent: font.Size * 0.78, Descent: font.Size * 0.22}
	obj.box = &fallback
	obj.placeholder = true
	return obj, true
}

func (e *Editor) shapeInline(b *document.Block, spans []ui.Span, font ui.Font, shape ui.TextShape) *textLayout {
	l := &textLayout{}
	text := b.Text()
	editObj, hasEdit := e.buildEditingInlineMath(b, text, font, shape)
	if hasEdit {
		l.objects = append(l.objects, editObj)
	}
	for _, token := range inlineTokens(text) {
		a, z := document.Len(text[:token.start]), document.Len(text[:token.end])
		if !tokenAllowed(b, a, z) {
			continue
		}
		if hasEdit && a < editObj.end && z > editObj.start {
			continue
		}
		obj := inlineObject{start: a, end: z, note: token.note, scale: 1}
		if token.note != "" {
			note, ok := e.notes[token.note]
			if !ok {
				continue
			}
			obj.label = ui.ShapeText(fmt.Sprint(note.number), ui.Font{Family: font.Family, Size: font.Size * .72}, 0)
		} else {
			box, err := tex.LayoutInline(token.source, font.Size*1.1, e.measureMath)
			if err != nil {
				continue
			}
			obj.box = &box
			obj.scale = min(float32(1), max(1, shape.Width-4)/max(1, box.W))
		}
		l.objects = append(l.objects, obj)
	}
	// MyGo 0.7.4 measures BaselineShift but its glyph painter ignores g.Y.
	// Draw each script grapheme as an object, retaining native caret boundaries,
	// marks and wrapping instead of painting small glyphs on the wrong baseline.
	offset := 0
	shifts := map[int]float32{}
	for _, span := range spans {
		n := document.Len(span.Text)
		if span.BaselineShift != 0 {
			shift := span.BaselineShift
			span.BaselineShift = 0
			whole := ui.ShapeRichText([]ui.Span{span}, font, 0)
			for a := 0; a < n; {
				z := whole.NextBoundary(a)
				if z <= a {
					break
				}
				overlap := false
				for _, o := range l.objects {
					if offset+a < o.end && offset+z > o.start {
						overlap = true
						break
					}
				}
				if !overlap {
					part := span
					part.Text = span.Text[document.ByteOffset(span.Text, a):document.ByteOffset(span.Text, z)]
					label := ui.ShapeRichText([]ui.Span{part}, font, 0)
					l.objects = append(l.objects, inlineObject{start: offset + a, end: offset + z, label: label, script: true, scale: 1})
					shifts[offset+a] = shift
				}
				a = z
			}
		}
		offset += n
	}
	sort.Slice(l.objects, func(i, j int) bool { return l.objects[i].start < l.objects[j].start })
	var visible []ui.Span
	pos, delta := 0, 0
	for i := range l.objects {
		o := &l.objects[i]
		o.index = o.start - delta
		span := ui.Span{Text: "\ufffc"}
		if o.box != nil {
			extra := float32(4)
			if o.editing {
				extra = 10
			}
			span.ObjectWidth = o.box.W*o.scale + extra
			span.ObjectHeight = o.box.Ascent * o.scale
			span.ObjectDescent = o.box.Descent * o.scale
		} else {
			w, h := o.label.Size()
			span.ObjectWidth = w + 4
			span.ObjectHeight = h + font.Size*.25
			if o.script {
				base := o.label.Lines()[0].Baseline
				shift := shifts[o.start]
				span.ObjectWidth = w
				span.ObjectHeight = base + shift
				span.ObjectDescent = max(0, h-base-shift)
			}
		}
		visible = append(visible, sliceSpans(spans, pos, o.start)...)
		visible = append(visible, span)
		pos = o.end
		delta += o.end - o.start - 1
	}
	visible = append(visible, sliceSpans(spans, pos, document.Len(text))...)
	if len(visible) == 0 {
		visible = spans
	}
	l.TextLayout = ui.ShapeRichTextWith(visible, font, shape)
	for _, o := range l.TextLayout.Objects() {
		for i := range l.objects {
			if o.Index == l.objects[i].index {
				l.objects[i].rect = o.Rect
				break
			}
		}
	}
	return l
}

func (l *textLayout) visual(at int, end bool) int {
	delta := 0
	for _, o := range l.objects {
		if at <= o.start {
			break
		}
		if at < o.end {
			if end {
				return o.index + 1
			}
			return o.index
		}
		delta += o.end - o.start - 1
	}
	return at - delta
}

func (l *textLayout) model(at int) int {
	delta := 0
	for _, o := range l.objects {
		if at <= o.index {
			break
		}
		delta += o.end - o.start - 1
	}
	return at + delta
}

func (l *textLayout) Caret(at int) ui.Rect { return l.TextLayout.Caret(l.visual(at, false)) }

func (l *textLayout) IndexAt(p ui.Point) int {
	for _, o := range l.objects {
		if p.X >= o.rect.X && p.X <= o.rect.X+o.rect.W && p.Y >= o.rect.Y && p.Y <= o.rect.Y+o.rect.H {
			if o.script {
				if p.X < o.rect.X+o.rect.W/2 {
					return o.start
				}
				return o.end
			}
			return o.start + 1
		}
	}
	return l.model(l.TextLayout.IndexAt(p))
}

func (l *textLayout) SelectionRects(r ui.TextInputRange) []ui.Rect {
	return l.TextLayout.SelectionRects(ui.TextInputRange{Start: l.visual(r.Start, false), End: l.visual(r.End, true)})
}

func (l *textLayout) NextBoundary(at int) int {
	for _, o := range l.objects {
		if o.note == "" && !o.script {
			if at == o.start && o.end-o.start >= 2 {
				return o.start + 1
			}
			if o.editing && o.sourceLayout != nil && at >= o.start && at < o.end {
				return o.start + o.sourceLayout.NextBoundary(at-o.start)
			}
		}
	}
	return l.model(l.TextLayout.NextBoundary(l.visual(at, false)))
}

func (l *textLayout) PreviousBoundary(at int) int {
	for _, o := range l.objects {
		if o.note == "" && !o.script {
			if at == o.end && o.end-o.start >= 2 {
				return o.end - 1
			}
			if o.editing && o.sourceLayout != nil && at > o.start && at <= o.end {
				return o.start + o.sourceLayout.PreviousBoundary(at-o.start)
			}
		}
	}
	return l.model(l.TextLayout.PreviousBoundary(l.visual(at, true)))
}

func (l *textLayout) Lines() []ui.TextLine {
	lines := l.TextLayout.Lines()
	for i := range lines {
		lines[i].Start = l.model(lines[i].Start)
		lines[i].End = l.model(lines[i].End)
	}
	return lines
}

func (e *Editor) paintInline(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) {
	dx, dy := e.visualOffset(r)
	for _, o := range r.layout.objects {
		ox, oy := bounds.X+r.x+dx+o.rect.X, bounds.Y+r.y+dy+o.rect.Y
		x, y := ox+2, oy
		if o.box != nil {
			if o.editing {
				frame := ui.Rect{X: ox, Y: oy - 2, W: max(20, o.rect.W-2), H: o.rect.H + 4}
				border := t.Accent.Alpha(0.42)
				fill := t.Accent.Alpha(0.065)
				if o.hasErr {
					border = t.Danger.Alpha(0.55)
				}
				p.Fill(frame, fill, 5)
				p.Stroke(frame, border, 5, 1)
				if o.placeholder && len(o.box.Ops) == 0 {
					fx := ui.ShapeText("fx", ui.Font{Family: mathFont, Size: 12, Italic: true}, 0)
					fw, fh := fx.Size()
					p.TextLayout(fx, ox+4+max(0, o.box.W*o.scale-fw)/2, oy+(o.rect.H-fh)/2, t.Accent.Alpha(0.7))
				} else {
					fx := ox + 4
					p.Transform(ui.Scaling(o.scale, o.scale), fx, y, func() { e.paintFormula(p, *o.box, fx, y+o.box.Ascent, t) })
				}
				e.paintTabOutBadge(p, frame, t)
			} else {
				p.Transform(ui.Scaling(o.scale, o.scale), x, y, func() { e.paintFormula(p, *o.box, x, y+o.box.Ascent, t) })
			}
		} else {
			if o.script {
				p.TextLayout(o.label, x-2, y, t.Text)
			} else {
				p.TextLayout(o.label, x, y, t.Accent)
			}
		}
	}
	if r.leaf.Block.Kind == "footnote" {
		n := e.notes[r.leaf.Block.NoteLabel]
		label := ui.ShapeText(fmt.Sprintf("%d ↩", n.number), ui.Font{Size: 12}, 0)
		p.TextLayout(label, bounds.X+r.rect.X+dx, bounds.Y+r.y+dy, t.Accent)
	}
}

// paintInlineMathPopover renders the floating monospace TeX bar beneath an
// active inline math formula and returns the document-space caret rect inside
// that bar when active.
func (e *Editor) paintInlineMathPopover(p *ui.Painter, bounds ui.Rect, t *ui.Theme) (ui.Rect, bool) {
	e.inlinePopRect = ui.Rect{}
	e.inlinePopLayout = nil
	if e.printing || e.ReadOnly || !e.focused || e.inlineMath.blockID == "" {
		return ui.Rect{}, false
	}
	idx, ok := e.rowIDs[e.inlineMath.blockID]
	if !ok {
		return ui.Rect{}, false
	}
	r := *e.ensureRow(idx)
	if r.layout == nil {
		return ui.Rect{}, false
	}
	for _, o := range r.layout.objects {
		if !o.editing {
			continue
		}
		dx, dy := e.visualOffset(r)
		anchorX := bounds.X + r.x + dx + o.rect.X
		anchorY := bounds.Y + r.y + dy + o.rect.Y + o.rect.H + 6
		delim := ui.ShapeText("$", ui.Font{Family: "monospace", Size: 12.5}, 0)
		dw, dh := delim.Size()
		srcLayout := ui.ShapeText(o.source, ui.Font{Family: "monospace", Size: 13}, 0)
		sw, sh := srcLayout.Size()
		sh = max(sh, 18)
		hint := ui.ShapeText(i18n.T("输入公式"), ui.Font{Family: "system-ui", Size: 12}, 0)
		hw, hh := hint.Size()
		innerW := sw
		if o.source == "" {
			innerW = max(innerW, hw+4)
		}
		popW := max(116, min(max(116, bounds.W-28), dw*2+innerW+30))
		popH := max(30, sh+12)
		popX := min(max(bounds.X+14, anchorX), max(bounds.X+14, bounds.X+bounds.W-14-popW))
		pop := ui.Rect{X: popX, Y: anchorY, W: popW, H: popH}

		p.Shadow(pop, 8, 0, 4, 14, 0, e.page.MathShadow)
		p.Fill(pop, e.page.Popover, 8)
		border := e.page.MathBorder
		if o.hasErr && strings.TrimSpace(o.source) != "" {
			border = t.Danger.Alpha(0.55)
		}
		p.Stroke(pop, border, 8, 1)

		textX := pop.X + 10 + dw + 6
		textY := pop.Y + (pop.H-sh)/2
		p.TextLayout(delim, pop.X+10, pop.Y+(pop.H-dh)/2, e.page.Caption)
		p.TextLayout(delim, pop.X+pop.W-10-dw, pop.Y+(pop.H-dh)/2, e.page.Caption)

		srcStart := r.leaf.Start + o.start + 1
		srcLen := document.Len(o.source)
		e.inlinePopRect = ui.Rect{X: pop.X - bounds.X, Y: pop.Y - bounds.Y, W: pop.W, H: pop.H}
		e.inlinePopTextX = textX - bounds.X
		e.inlinePopTextY = textY - bounds.Y
		e.inlinePopLayout = srcLayout
		e.inlinePopStart = srcStart
		e.inlinePopLen = srcLen

		if e.Anchor != e.Caret {
			selLo := max(0, min(srcLen, min(e.Anchor, e.Caret)-srcStart))
			selHi := max(0, min(srcLen, max(e.Anchor, e.Caret)-srcStart))
			for _, sr := range srcLayout.SelectionRects(ui.TextInputRange{Start: selLo, End: selHi}) {
				p.Fill(ui.Rect{X: textX + sr.X, Y: textY + sr.Y, W: sr.W, H: sr.H}, t.Selection, 2)
			}
		}
		if o.source == "" {
			p.TextLayout(hint, textX+2, pop.Y+(pop.H-hh)/2, e.page.Placeholder)
		} else {
			p.TextLayout(srcLayout, textX, textY, t.Text)
		}
		localCaret := max(0, min(srcLen, e.Caret-srcStart))
		cr := srcLayout.Caret(localCaret)
		caretDoc := ui.Rect{
			X: (textX - bounds.X) + cr.X,
			Y: (textY - bounds.Y) + cr.Y,
			W: 2,
			H: max(16, sh),
		}
		return caretDoc, true
	}
	return ui.Rect{}, false
}

func (e *Editor) clickInlineMathPopover(x, y float32) bool {
	pop := e.inlinePopRect
	if e.inlinePopLayout == nil || pop.W <= 0 || x < pop.X || x > pop.X+pop.W || y < pop.Y || y > pop.Y+pop.H {
		return false
	}
	idx := e.inlinePopLayout.IndexAt(ui.Point{X: x - e.inlinePopTextX, Y: y - e.inlinePopTextY})
	pos := e.inlinePopStart + max(0, min(e.inlinePopLen, idx))
	e.SetSelection(pos, pos)
	e.reveal = false
	return true
}

func (e *Editor) exitInlineMath() bool {
	if e.inlineMath.blockID == "" {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != e.inlineMath.blockID {
		return false
	}
	openPos := l.Start + e.inlineMath.start
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			if e.pairs[i].open == "$" && e.pairs[i].start == openPos {
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				break
			}
		}
	}
	after := min(l.End, l.Start+e.inlineMath.end)
	e.SetSelection(after, after)
	e.smartBack = smartRollback{}
	e.reveal = true
	return true
}

func (e *Editor) escapeInlineMath() bool {
	if e.inlineMath.blockID == "" {
		return false
	}
	l := e.LeafAt(e.Caret)
	if l.Block.ID != e.inlineMath.blockID {
		return false
	}
	openPos := l.Start + e.inlineMath.start
	closeStart := l.Start + e.inlineMath.end - 1
	if e.pairRevision == e.Revision() {
		for i := len(e.pairs) - 1; i >= 0; i-- {
			p := e.pairs[i]
			if p.open == "$" && p.start == openPos && closeStart >= l.Start && closeStart+1 <= l.End && e.slice(closeStart, closeStart+1) == "$" {
				caret := e.Caret
				e.pairs = append(e.pairs[:i], e.pairs[i+1:]...)
				r := ui.TextInputRange{Start: closeStart, End: closeStart + 1}
				e.shiftPairs(r, 0)
				e.Buffer.ReplaceText(&r, "")
				e.SetSelection(min(caret, e.Len()), min(caret, e.Len()))
				return true
			}
		}
	}
	return e.exitInlineMath()
}

type noteTarget struct {
	number, start int
	id            string
}

// Only blocks with effective references need to participate in renumbering.
// A text edit cannot change definition labels or the order of blocks.
type noteReferences struct {
	id     string
	start  int
	labels []string
}

func referencedNotes(b *document.Block, text string) []string {
	if literalBlock(b.Kind) || !strings.Contains(text, "[^") {
		return nil
	}
	var labels []string
	for _, token := range inlineTokens(text) {
		if token.note != "" && tokenAllowed(b, document.Len(text[:token.start]), document.Len(text[:token.end])) {
			labels = append(labels, token.note)
		}
	}
	return labels
}

func (e *Editor) collectNotes() {
	if e.notesDoc == e.Doc && e.notesRevision == e.Revision() {
		return
	}
	if change, ok := e.TextChangeSince(e.notesRevision); ok && e.notesDoc == e.Doc {
		e.notesRevision = e.Revision()
		if len(e.notes) == 0 {
			return // text-only edits cannot create a footnote definition
		}
		delta := change.AfterEnd - change.BeforeEnd
		for label, note := range e.notes {
			if note.start > change.Start {
				note.start += delta
				e.notes[label] = note
			}
		}
		at := sort.Search(len(e.noteRefs), func(i int) bool { return e.noteRefs[i].start >= change.Start })
		labels := referencedNotes(e.Engine.LeafAt(change.Start).Block, change.After)
		var previous []string
		present := at < len(e.noteRefs) && e.noteRefs[at].id == change.ID
		if present {
			previous = e.noteRefs[at].labels
		}
		for i := at; i < len(e.noteRefs); i++ {
			if e.noteRefs[i].start > change.Start {
				e.noteRefs[i].start += delta
			}
		}
		if !slices.Equal(previous, labels) {
			switch {
			case present && len(labels) == 0:
				e.noteRefs = slices.Delete(e.noteRefs, at, at+1)
			case present:
				e.noteRefs[at].labels = labels
			default:
				e.noteRefs = slices.Insert(e.noteRefs, at, noteReferences{change.ID, change.Start, labels})
			}
			e.numberNotes()
		}
		return
	}
	e.notesDoc, e.notesRevision = e.Doc, e.Revision()
	e.noteRefs, e.noteOrder = nil, nil
	e.notesKey = ""
	e.notes = map[string]noteTarget{}
	if !e.Doc.HasKind("footnote") {
		// Only references to a definition are numbered, so a page without
		// one has nothing to number: skip the pass over all its text,
		// which every keystroke on a long page would otherwise pay.
		return
	}
	leaves := e.Doc.Leaves()
	for _, l := range leaves {
		if l.Block.Kind == "footnote" {
			if _, exists := e.notes[l.Block.NoteLabel]; !exists {
				e.notes[l.Block.NoteLabel] = noteTarget{start: l.Start, id: l.Block.ID}
				e.noteOrder = append(e.noteOrder, l.Block.NoteLabel)
			}
		}
	}
	for _, l := range leaves {
		if labels := referencedNotes(l.Block, l.Block.Text()); len(labels) > 0 {
			e.noteRefs = append(e.noteRefs, noteReferences{l.Block.ID, l.Start, labels})
		}
	}
	e.numberNotes()
}

func (e *Editor) numberNotes() {
	for label, note := range e.notes {
		note.number = 0
		e.notes[label] = note
	}
	n := 0
	assign := func(label string) {
		if note, ok := e.notes[label]; ok && note.number == 0 {
			n++
			note.number = n
			e.notes[label] = note
		}
	}
	for _, refs := range e.noteRefs {
		for _, label := range refs.labels {
			assign(label)
		}
	}
	for _, label := range e.noteOrder {
		assign(label)
	}
	e.notesKey = e.buildNoteKey()
}

func (e *Editor) noteKey() string { return e.notesKey }

func (e *Editor) buildNoteKey() string {
	keys := make([]string, 0, len(e.notes))
	for label, n := range e.notes {
		keys = append(keys, fmt.Sprintf("%s:%d", label, n.number))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}

// FollowFootnote also works at a keyboard caret; returning restores the exact
// originating reference, including when several references share a definition.
func (e *Editor) FollowFootnote() bool {
	l := e.LeafAt(e.Caret)
	if l.Block.Kind == "footnote" {
		return e.ReturnFootnote()
	}
	s := l.Block.Text()
	for _, t := range inlineTokens(s) {
		a, z := l.Start+document.Len(s[:t.start]), l.Start+document.Len(s[:t.end])
		if t.note != "" && e.Caret >= a && e.Caret <= z {
			if n, ok := e.notes[t.note]; ok {
				e.noteReturn, e.noteHasReturn = e.Caret, true
				e.noteOriginID, e.noteOriginOffset = l.Block.ID, e.Caret-l.Start
				e.FocusBlock(n.id)
				e.flashBlock(e.LeafAt(e.Caret).Owner)
				e.reveal, e.RequestFocus = true, true
				return true
			}
		}
	}
	return false
}

func (e *Editor) ReturnFootnote() bool {
	if !e.noteHasReturn {
		label := e.LeafAt(e.Caret).Block.NoteLabel
		if label == "" {
			return false
		}
		for _, leaf := range e.Doc.Leaves() {
			s := leaf.Block.Text()
			for _, token := range inlineTokens(s) {
				if token.note == label && tokenAllowed(leaf.Block, document.Len(s[:token.start]), document.Len(s[:token.end])) {
					e.noteReturn = leaf.Start + document.Len(s[:token.start])
					e.noteHasReturn = true
					e.noteOriginID = ""
					break
				}
			}
			if e.noteHasReturn {
				break
			}
		}
		if !e.noteHasReturn {
			return false
		}
	}
	for _, leaf := range e.Doc.Leaves() {
		if leaf.Block.ID == e.noteOriginID {
			e.noteReturn = min(leaf.End, leaf.Start+e.noteOriginOffset)
			break
		}
	}
	e.SetSelection(e.noteReturn, e.noteReturn)
	e.flashBlock(e.LeafAt(e.Caret).Owner)
	e.noteHasReturn = false
	e.reveal, e.RequestFocus = true, true
	return true
}
