package editor

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// Hand-drawn annotations follow Opensource UI's AnnotatedText, under the
// MIT License, Copyright (c) 2026 Bidyut Kundu (third_party/opensourceui).
// Its SVG paths are kept as they are, stretched over the annotated text the
// way preserveAspectRatio="none" stretches them, and its turbulence filter
// becomes a smooth, seeded displacement of the flattened strokes, so a mark
// looks drawn by hand yet stays the same from frame to frame.

// annotationDraw is how long a new mark takes to draw itself.
const annotationDraw = 550 * time.Millisecond

// annotationLook is one variant: where its box sits around the text, in
// ems and fractions of the text's width as the original's Tailwind classes
// put it, and the strokes drawn in that box.
type annotationLook struct {
	name string
	// color is the variant's own, one of document.TextColors.
	color string
	// pad is the space the original's wrapper adds each side, in ems:
	// padAnnotations opens it in the text with letter spacing.
	pad float32
	// box places the decoration around the padded text: left and right
	// grow it by fractions of the width plus ems, top and bottom are ems
	// from the text's middle, up being negative.
	left, right, leftEm, rightEm float32
	top, bottom                  float32
	view                         [2]float32
	strokes                      []annotationStroke
	// under draws it under the text, as a highlight.
	under bool
	// dots draws the dotted underline instead of strokes.
	dots bool
	// bar draws a plain rounded bar 2 DIP high instead of strokes.
	bar bool
}

type annotationStroke struct {
	d     string
	width float32 // In the view box's units; 0 fills.
	alpha float32
	rough bool // The stronger of the original's two filters.
}

// Each look's box is what its decorationClassName gives: the text box is
// 1.2em high around its middle, so "bottom: -0.4em; height: 0.7em" is from
// 0.6-0.7+0.4 = 0.3em below the middle down to 1.0em.
var annotationLooks = []annotationLook{
	{name: "wavy", color: "purple", left: .02, right: .02, top: .3, bottom: 1,
		view: [2]float32{140, 14}, strokes: []annotationStroke{{d: "M2,6 Q5.5,3 9,6 T17,6 T25,6 T33,6 T41,6 T49,6 T57,6 T65,6 T73,6 T81,6 T89,6 T97,6 T105,6 T113,6 T121,6 T129,6 T137,6", width: 2.2, alpha: 1}}},
	{name: "circle", color: "blue", pad: .25, leftEm: .55, rightEm: .55, top: -1.2, bottom: 1.2,
		view: [2]float32{220, 64}, strokes: []annotationStroke{
			{d: "M40,40 C20,23 53,7 102,5 C153,3 207,11 211,29 C215,47 167,60 109,60 C59,60 15,53 19,35 C21,27 27,22 37,20", width: 3, alpha: 1, rough: true},
			{d: "M43,37 C28,25 58,9 105,7 C151,6 199,14 206,29 C212,45 167,57 110,58", width: 1.5, alpha: .55},
		}},
	{name: "highlight", color: "yellow", left: .04, right: .04, top: -.47, bottom: .68, under: true,
		view: [2]float32{170, 26}, strokes: []annotationStroke{{d: "M4,17 C2,11 5,7 12,6 C45,2 95,2 138,4 C152,5 164,7 166,13 C167,18 163,21 155,22 C112,24 60,24 16,22 C8,21.5 4,20 4,17 Z", alpha: 1}}},
	{name: "underline", color: "purple", left: .01, right: .01, top: .42, bottom: .92,
		view: [2]float32{140, 10}, strokes: []annotationStroke{{d: "M3,6 C40,3 100,3 137,5", width: 2.4, alpha: 1}}},
	{name: "line", color: "gray", left: .01, right: .01, top: .66, bottom: .78, bar: true},
	{name: "dotted", color: "gray", left: .01, right: .01, top: .4, bottom: .95, dots: true},
	{name: "double", color: "green", left: .01, right: .01, top: .4, bottom: 1.1,
		view: [2]float32{140, 16}, strokes: []annotationStroke{
			{d: "M3,5 C40,2 100,2 137,4", width: 2.2, alpha: 1},
			{d: "M5,12 C42,9 98,10 135,11", width: 1.8, alpha: .75},
		}},
	{name: "strike", color: "red", left: .01, right: .01, top: -.25, bottom: .25,
		view: [2]float32{140, 10}, strokes: []annotationStroke{{d: "M3,5 C40,7 100,3 137,5", width: 2.4, alpha: 1}}},
	{name: "cross", color: "red", left: .02, right: .02, top: -.7, bottom: .7,
		view: [2]float32{140, 40}, strokes: []annotationStroke{
			{d: "M4,32 C40,10 96,30 136,8", width: 2.4, alpha: 1, rough: true},
			{d: "M6,10 C44,30 92,12 134,30", width: 2, alpha: .7},
		}},
	{name: "arrow", color: "blue", left: .01, right: .05, top: .25, bottom: 1.05,
		view: [2]float32{150, 18}, strokes: []annotationStroke{
			{d: "M3,7 C45,3 105,4 140,8", width: 2.4, alpha: 1},
			{d: "M132,3 L142,8 L131,13", width: 2.4, alpha: 1},
		}},
	{name: "bracket", color: "gray", pad: .35, leftEm: .15, rightEm: .15, top: -.85, bottom: .85,
		view: [2]float32{160, 60}, strokes: []annotationStroke{
			{d: "M18,6 C9,7 6,12 6,30 C6,48 9,53 18,54", width: 2.6, alpha: 1, rough: true},
			{d: "M142,6 C151,7 154,12 154,30 C154,48 151,53 142,54", width: 2.6, alpha: 1, rough: true},
		}},
	{name: "box", color: "orange", pad: .4, leftEm: .4, rightEm: .4, top: -1, bottom: 1,
		view: [2]float32{200, 64}, strokes: []annotationStroke{{d: "M12,10 C60,6 140,6 188,10 C193,26 193,40 188,54 C140,58 60,58 12,54 C7,40 7,26 12,10 Z", width: 2.6, alpha: 1, rough: true}}},
}

// AnnotationNames name the variants for people, in document.Annotations'
// order.
var AnnotationNames = map[string]string{
	"wavy": "波浪线", "circle": "圈出", "highlight": "荧光笔", "underline": "手绘下划线", "line": "直线",
	"dotted": "点状下划线", "double": "双下划线", "strike": "手绘删除线", "cross": "打叉", "arrow": "箭头",
	"bracket": "括号", "box": "方框",
}

func lookOf(name string) (annotationLook, bool) {
	for _, l := range annotationLooks {
		if l.name == name {
			return l, true
		}
	}
	return annotationLook{}, false
}

// annotationColor is the color a mark is drawn in.
func (e *Editor) annotationColor(variant, color string) ui.Color {
	if color == "" {
		if l, ok := lookOf(variant); ok {
			color = l.color
		}
	}
	return e.page.TextColors[color]
}

// Annotate marks the selection with a hand-drawn annotation, or what is
// typed next at a caret, and lets the new mark draw itself; "" takes it
// away. color is one of document.TextColors, or "" for the variant's own.
func (e *Editor) Annotate(variant, color string) {
	sel := e.Selection().Range
	e.Engine.SetAnnotation(variant, color)
	if variant != "" {
		e.lastAnnotation, e.lastAnnotationColor = variant, color
	}
	e.drawIn = annotationReveal{}
	if variant != "" && sel.Start != sel.End && !e.ReadOnly {
		e.drawIn = annotationReveal{start: sel.Start, end: sel.End, pending: true}
	}
	e.RequestFocus = true
}

// annotationReveal is the mark that is drawing itself.
type annotationReveal struct {
	start, end int
	pending    bool // Its clock starts with the next paint.
	at         time.Time
}

// padAnnotations opens the space the original's wrapper pads a circle,
// bracket or box with: the character before the stretch and its last
// character each get that much letter spacing, which follows a character,
// so the stretch's text sits between two gaps its decoration fills.
func padAnnotations(spans []ui.Span, runs []document.Run, size float32) []ui.Span {
	needsPad := false
	for i := range runs {
		if v := runs[i].Style.Annotation; v != "" {
			if look, ok := lookOf(v); ok && look.pad != 0 {
				needsPad = true
				break
			}
		}
	}
	if !needsPad {
		return spans
	}
	type gap struct {
		at  int // The character ending at at gets the spacing.
		pad float32
	}
	var gaps []gap
	var sb strings.Builder
	for _, r := range runs {
		sb.WriteString(r.Text)
	}
	text := sb.String()
	for _, run := range annotationRuns(runs) {
		look, ok := lookOf(run.variant)
		if !ok || look.pad == 0 {
			continue
		}
		if run.start > 0 {
			gaps = append(gaps, gap{run.start, look.pad * size})
		}
		gaps = append(gaps, gap{run.end, look.pad * size})
	}
	// From the end, so earlier offsets hold.
	for i := len(gaps) - 1; i >= 0; i-- {
		g := gaps[i]
		at := ui.UTF16ByteOffset(text, g.at)
		r, n := utf8.DecodeLastRuneInString(text[:at])
		if n == 0 || r == '\n' || r == '\u2028' {
			continue
		}
		from := g.at - document.Len(string(r))
		total := document.Len(text)
		mid := sliceSpans(spans, from, g.at)
		for k := range mid {
			mid[k].LetterSpacing += g.pad
		}
		spans = append(append(sliceSpans(spans, 0, from), mid...), sliceSpans(spans, g.at, total)...)
	}
	return spans
}

// annotationRun is a stretch of a row under one mark.
type annotationRun struct {
	start, end     int
	variant, color string
}

// annotationRuns are a row's marked stretches: runs that differ in other
// styles but share a mark are one stretch, so a circle goes round all of it.
func annotationRuns(runs []document.Run) []annotationRun {
	var out []annotationRun
	at := 0
	for _, r := range runs {
		n := document.Len(r.Text)
		if v := r.Style.Annotation; v != "" && n > 0 {
			if k := len(out) - 1; k >= 0 && out[k].end == at && out[k].variant == v && out[k].color == r.Style.AnnotationColor {
				out[k].end = at + n
			} else {
				out = append(out, annotationRun{start: at, end: at + n, variant: v, color: r.Style.AnnotationColor})
			}
		}
		at += n
	}
	return out
}

// paintAnnotations draws a row's marks: under its text when under is
// true, over it otherwise.
func (e *Editor) paintAnnotations(p *ui.Painter, bounds ui.Rect, r row, under bool) {
	if r.leaf.Block == nil || r.leaf.Block.Kind == "code" || r.leaf.Block.Kind == "math" || r.leaf.Block.Kind == "raw" {
		return
	}
	runs := annotationRuns(r.leaf.Block.Runs)
	if len(runs) == 0 {
		return
	}
	size := r.size
	if size <= 0 {
		size = 16
	}
	for _, run := range runs {
		look, ok := lookOf(run.variant)
		if !ok || look.under != under {
			continue
		}
		progress := e.revealProgress(p, r.leaf.Start+run.start, r.leaf.Start+run.end)
		color := e.annotationColor(run.variant, run.color)
		rects := r.layout.SelectionRects(ui.TextInputRange{Start: run.start, End: run.end})
		if k := len(rects) - 1; k >= 0 && look.pad > 0 {
			// The last character's spacing is the gap after the text, not
			// the text: the box pads both sides itself.
			rects[k].W -= look.pad * size
		}
		for i, rect := range rects {
			if rect.W < 1 {
				continue
			}
			rect.X += bounds.X + r.x
			rect.Y += bounds.Y + r.y
			seed := uint32(r.leaf.Start+run.start)*2654435761 + uint32(i)*40503
			paintAnnotation(p, look, rect, size, color, progress, seed)
		}
	}
}

// openReveal draws the marks in view one after another when a document
// opens, if the preference asks for it.
type openReveal struct {
	checked, pending bool
	at               time.Time
	// order numbers the marks in view, in the page's order, each frame.
	order map[int]int
}

// openStagger is the delay between one mark and the next; openStaggerMax
// keeps a page of many marks from taking long.
const (
	openStagger    = 120 * time.Millisecond
	openStaggerMax = 1500 * time.Millisecond
)

func hasAnnotations(blocks []*document.Block) bool {
	for _, b := range blocks {
		if b == nil {
			continue
		}
		for _, r := range b.Runs {
			if r.Style.Annotation != "" {
				return true
			}
		}
		if hasAnnotations(b.Children) {
			return true
		}
	}
	return false
}

// revealProgress is how far the mark over a..z has drawn itself, from 0 to
// 1, asking for frames until it has: a new mark, or one drawing itself as
// its document opens.
func (e *Editor) revealProgress(p *ui.Painter, a, z int) float32 {
	if e.printing || e.reduceMotion {
		e.openReveal.pending, e.openReveal.at = false, time.Time{}
		return 1
	}
	progress := float32(1)
	if o := &e.openReveal; o.pending || !o.at.IsZero() {
		if o.pending {
			o.at, o.pending = p.Now(), false
		}
		if o.order == nil {
			o.order = map[int]int{}
		}
		i, ok := o.order[a]
		if !ok {
			i = len(o.order)
			o.order[a] = i
		}
		since := p.Now().Sub(o.at)
		if since >= openStaggerMax+annotationDraw {
			o.at = time.Time{}
		} else {
			p.AnimationFrame()
			progress = easeOut(float32(since-min(openStaggerMax, time.Duration(i)*openStagger)) / float32(annotationDraw))
		}
	}
	d := &e.drawIn
	if d.end <= d.start || z <= d.start || a >= d.end {
		return progress
	}
	if d.pending {
		d.at, d.pending = p.Now(), false
	}
	t := float32(p.Now().Sub(d.at)) / float32(annotationDraw)
	if t >= 1 {
		*d = annotationReveal{}
		return 1
	}
	p.AnimationFrame()
	return min(progress, easeOut(t))
}

// easeOut is a cubic ease-out of t, clamped to 0…1.
func easeOut(t float32) float32 {
	t = max(0, min(1, t))
	return 1 - (1-t)*(1-t)*(1-t)
}

// annotationBox is where a look's decoration goes around one line of text.
func annotationBox(look annotationLook, line ui.Rect, size float32) ui.Rect {
	mid := line.Y + line.H/2
	w := line.W + 2*look.pad*size
	x := line.X - look.pad*size
	left := look.left*w + look.leftEm*size
	right := look.right*w + look.rightEm*size
	return ui.Rect{X: x - left, Y: mid + look.top*size, W: w + left + right, H: (look.bottom - look.top) * size}
}

// paintAnnotation draws one look over one line of text, progress of the
// way through drawing itself.
func paintAnnotation(p *ui.Painter, look annotationLook, line ui.Rect, size float32, color ui.Color, progress float32, seed uint32) {
	box := annotationBox(look, line, size)
	if progress <= 0 {
		return
	}
	switch {
	case look.bar:
		p.Fill(ui.Rect{X: box.X, Y: box.Y + box.H/2 - 1, W: box.W * progress, H: 2}, color, 1)
		return
	case look.dots:
		// A dot of radius 1.5 every half em, in the middle of each tile.
		step := size / 2
		for x := box.X + step/2; x <= box.X+box.W*progress; x += step {
			var path ui.Path
			path.Circle(x, box.Y+box.H/2, 1.5)
			p.FillPath(&path, color)
		}
		return
	}
	sx, sy := box.W/look.view[0], box.H/look.view[1]
	for _, s := range look.strokes {
		scale, freq := float32(1.5), float32(.035)
		if s.rough {
			scale, freq = 2.6, .045
		}
		subpaths := flattenSVG(s.d)
		var path ui.Path
		for _, sub := range subpaths {
			for i, pt := range sub {
				x, y := box.X+pt[0]*sx, box.Y+pt[1]*sy
				dx, dy := roughOffset(x, y, freq, seed)
				x, y = x+dx*scale, y+dy*scale
				if i == 0 {
					path.MoveTo(x, y)
				} else {
					path.LineTo(x, y)
				}
			}
		}
		c := color.Alpha(float32(color.A) / 255 * s.alpha)
		if s.width == 0 {
			// The highlight sweeps in from the left rather than tracing
			// its outline.
			c = color.Alpha(float32(color.A) / 255 * .45)
			if progress >= 1 {
				p.FillPath(&path, c)
			} else {
				p.Clip(ui.Rect{X: box.X - 4, Y: box.Y - 4, W: (box.W + 8) * progress, H: box.H + 8}, 0, func() { p.FillPath(&path, c) })
			}
			continue
		}
		path.Cap(ui.RoundCap).Join(ui.RoundJoin)
		if progress < 1 {
			path.Trim(0, progress)
		}
		// preserveAspectRatio="none" stretches strokes too: they follow
		// the box's height, which tracks the font, not the text's length.
		width := min(3.4, max(1.2, s.width*sy*1.1))
		p.StrokePath(&path, width, c)
	}
}

// roughOffset is the original filter's displacement at x, y: two octaves
// of smooth value noise for each axis, from -0.5 to 0.5.
func roughOffset(x, y, freq float32, seed uint32) (float32, float32) {
	return fractal(x*freq, y*freq, seed) - .5, fractal(x*freq, y*freq, seed^0x9e3779b9) - .5
}

func fractal(x, y float32, seed uint32) float32 {
	return (valueNoise(x, y, seed)*2 + valueNoise(x*2, y*2, seed+1)) / 3
}

func valueNoise(x, y float32, seed uint32) float32 {
	x0, y0 := float32(math.Floor(float64(x))), float32(math.Floor(float64(y)))
	fx, fy := x-x0, y-y0
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	ix, iy := int32(x0), int32(y0)
	a, b := hashNoise(ix, iy, seed), hashNoise(ix+1, iy, seed)
	c, d := hashNoise(ix, iy+1, seed), hashNoise(ix+1, iy+1, seed)
	top := a + (b-a)*fx
	return top + (c+(d-c)*fx-top)*fy
}

func hashNoise(x, y int32, seed uint32) float32 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + seed*2246822519
	h = (h ^ h>>13) * 1274126177
	h ^= h >> 16
	return float32(h&0xffffff) / float32(0xffffff)
}

// flattenSVG turns the absolute M, L, C, Q, T and Z commands the looks use
// into polylines, one per subpath.
func flattenSVG(d string) [][][2]float32 {
	fields := strings.FieldsFunc(d, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	var tokens []string
	for _, f := range fields {
		// A command letter may run into its first number: "M2" or "T17".
		for len(f) > 0 && unicode.IsLetter(rune(f[0])) {
			tokens = append(tokens, f[:1])
			f = f[1:]
		}
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	var out [][][2]float32
	var cur [][2]float32
	var at, start, ctrl [2]float32
	var cmd string
	num := func(i *int) float32 {
		v, _ := strconv.ParseFloat(tokens[*i], 32)
		*i++
		return float32(v)
	}
	const steps = 16
	for i := 0; i < len(tokens); {
		if unicode.IsLetter(rune(tokens[i][0])) {
			cmd = tokens[i]
			i++
			if cmd == "Z" || cmd == "z" {
				cur = append(cur, start)
				at = start
				continue
			}
		}
		if i >= len(tokens) {
			break
		}
		switch cmd {
		case "M":
			if len(cur) > 1 {
				out = append(out, cur)
			}
			at = [2]float32{num(&i), num(&i)}
			start, ctrl, cur = at, at, [][2]float32{at}
			cmd = "L"
		case "L":
			at = [2]float32{num(&i), num(&i)}
			ctrl = at
			cur = append(cur, at)
		case "C":
			if i+5 >= len(tokens) {
				i = len(tokens)
				break
			}
			c1 := [2]float32{num(&i), num(&i)}
			c2 := [2]float32{num(&i), num(&i)}
			end := [2]float32{num(&i), num(&i)}
			for s := 1; s <= steps; s++ {
				t := float32(s) / steps
				u := 1 - t
				cur = append(cur, [2]float32{
					u*u*u*at[0] + 3*u*u*t*c1[0] + 3*u*t*t*c2[0] + t*t*t*end[0],
					u*u*u*at[1] + 3*u*u*t*c1[1] + 3*u*t*t*c2[1] + t*t*t*end[1],
				})
			}
			at, ctrl = end, c2
		case "Q", "T":
			var c [2]float32
			if cmd == "Q" {
				c = [2]float32{num(&i), num(&i)}
			} else {
				// T reflects the last control point about the current one.
				c = [2]float32{2*at[0] - ctrl[0], 2*at[1] - ctrl[1]}
			}
			if i+1 >= len(tokens) {
				i = len(tokens)
				break
			}
			end := [2]float32{num(&i), num(&i)}
			for s := 1; s <= steps/2; s++ {
				t := float32(s) / (steps / 2)
				u := 1 - t
				cur = append(cur, [2]float32{u*u*at[0] + 2*u*t*c[0] + t*t*end[0], u*u*at[1] + 2*u*t*c[1] + t*t*end[1]})
			}
			at, ctrl = end, c
		default:
			i++
		}
	}
	if len(cur) > 1 {
		out = append(out, cur)
	}
	return out
}

// iconAnnotate is Lucide's pencil-line.
var iconAnnotate = lucide(`<path d="M13 21h8"/><path d="m15 5 4 4"/><path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>`)

// annotationPanelWidth and annotationPanelHeight are the mark picker's
// size: four rows of tiles, the colors and the remover.
const (
	annotationPanelWidth  = 300
	annotationPanelHeight = 336
)

// annotateSplit is the selection bar's annotation button: the pen
// applies the last mark used, the arrow opens the mark picker.
func (e *Editor) annotateSplit(c *ui.Context) {
	pg := e.page
	ui.Row(c.Key("float:annotate")).Height(24).Radius(4).Children(func() {
		name := i18n.T(AnnotationNames[e.lastAnnotation])
		apply := ui.ButtonBase(c.Key("float:annotate-apply")).Size(24, 24).Radius(4).Center().Label(i18n.T("手绘标注")).
			Tooltip(i18n.T("手绘标注：") + name).KeepFocus().Cursor(ui.CursorPointer)
		if apply.Hovered() {
			apply.Background(pg.Hover)
		}
		// The button shows the mark it draws, on an "A" as the old color
		// button showed its color, rather than a pen whose color alone tells.
		ink := e.annotationColor(e.lastAnnotation, e.lastAnnotationColor)
		look, _ := lookOf(e.lastAnnotation)
		text := c.Theme().Text
		apply.Draw(func(p *ui.Painter, r ui.Rect) { paintAnnotationGlyph(p, look, r, "A", 14, ink, text) })
		if apply.Clicked() {
			e.Annotate(e.lastAnnotation, e.lastAnnotationColor)
		}
		open := ui.ButtonBase(c.Key("float:annotate-open")).Size(20, 24).Radius(4).Center().Label(i18n.T("选择标注")).Tooltip(i18n.T("选择手绘标注")).KeepFocus().Cursor(ui.CursorPointer)
		if open.Hovered() || e.annotatePanel {
			open.Background(pg.Hover)
		}
		open.Children(func() { ui.Icon(c, iconChevron).FontSize(12).TextColor(pg.Placeholder) })
		if open.Clicked() {
			e.OpenAnnotations()
		}
	})
}

// OpenAnnotations opens the mark picker of the selection bar.
func (e *Editor) OpenAnnotations() {
	e.annotatePanel = !e.annotatePanel
	e.annotatePanelOpened = true
}

// annotationPanel picks a hand-drawn mark and its color: each mark is a
// tile that shows itself on a sample, then the colors, then a remover.
func (e *Editor) annotationPanel(c *ui.Context, x, y float32) {
	pg := e.page
	t := c.Theme()
	style := e.Engine.CurrentStyle()
	windowW, _ := c.Size()
	x = max(8, min(x, windowW-annotationPanelWidth-8))
	title := func(text string) {
		ui.Text(c, text).FontSize(12).TextColor(t.Text).Margin(7, 0, 6, 0)
	}
	color := style.AnnotationColor
	if style.Annotation == "" {
		color = e.lastAnnotationColor
	}
	panel := ui.Column(c.Key("annotation-panel")).Absolute().Left(x).Top(y).Width(annotationPanelWidth).Padding(4, 10, 12, 10)
	e.popover(c, panel).Children(func() {
		title(i18n.T("手绘标注"))
		ui.Column(c).Gap(6).Children(func() {
			for row := 0; row < len(annotationLooks); row += 3 {
				ui.Row(c.Key(row)).Gap(6).Children(func() {
					for _, look := range annotationLooks[row:min(row+3, len(annotationLooks))] {
						name := i18n.T(AnnotationNames[look.name])
						tile := ui.ButtonBase(c.Key("an:" + look.name)).Height(46).Grow(1).Basis(0).Radius(6).Label(name).
							Tooltip(name).KeepFocus().Cursor(ui.CursorPointer)
						switch {
						case style.Annotation == look.name:
							tile.Background(pg.TurnActive).Border(1.5, pg.CodeHover)
						case tile.Hovered():
							tile.Background(pg.Hover)
						}
						ink := e.annotationColor(look.name, color)
						text := t.Text
						tile.Draw(func(p *ui.Painter, r ui.Rect) { paintAnnotationSample(p, look, r, ink, text) })
						if tile.Clicked() {
							variant := look.name
							if style.Annotation == variant {
								variant = ""
							}
							e.Annotate(variant, color)
						}
					}
				})
			}
		})
		title(i18n.T("颜色"))
		ui.Row(c).Justify(ui.SpaceBetween).Children(func() {
			for _, name := range append([]string{""}, document.TextColors...) {
				label := i18n.T("标注自带颜色")
				if name != "" {
					label = i18n.T("标注颜色：") + ColorName(name, false)
				}
				s := ui.ButtonBase(c.Key("ac:"+name)).Size(26, 26).Radius(13).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
				if color == name {
					s.Border(2, pg.CodeHover)
				} else if s.Hovered() {
					s.Background(pg.Hover)
				}
				s.Children(func() { colorDot(c, pg, name) })
				if s.Clicked() {
					e.lastAnnotationColor = name
					if style.Annotation != "" {
						e.Annotate(style.Annotation, name)
					}
				}
			}
		})
		remove := ui.ButtonBase(c.Key("annotation-remove")).Height(24).Margin(12, 0, 0, 0).Radius(6).Border(1, t.Border).Center().
			Label(i18n.T("移除标注")).KeepFocus().Cursor(ui.CursorPointer)
		if remove.Hovered() {
			remove.Background(pg.Hover)
		}
		remove.Children(func() { ui.Text(c, i18n.T("移除标注")).FontSize(12).TextColor(t.Text) })
		if remove.Clicked() {
			e.Annotate("", "")
		}
	})
	if panel.PressedOutside() && !e.annotatePanelOpened {
		e.annotatePanel = false
	}
	e.annotatePanelOpened = false
}

// paintAnnotationSample draws a look on its name in the middle of r.
func paintAnnotationSample(p *ui.Painter, look annotationLook, r ui.Rect, ink, text ui.Color) {
	paintAnnotationGlyph(p, look, r, i18n.T(AnnotationNames[look.name]), 13, ink, text)
}

// paintAnnotationGlyph draws a look on a word in the middle of r.
func paintAnnotationGlyph(p *ui.Painter, look annotationLook, r ui.Rect, word string, size float32, ink, text ui.Color) {
	span := ui.Span{Text: word, Size: size, Color: text}
	w, h := p.MeasureText(0, span)
	line := ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
	if look.under {
		paintAnnotation(p, look, line, size, ink, 1, 7)
	}
	p.RichText(line.X, line.Y, 0, span)
	if !look.under {
		paintAnnotation(p, look, line, size, ink, 1, 7)
	}
}

// markPopover is the small bar a click on marked text opens beside it, to
// change or take away the mark without selecting it first. The click
// still places the caret, so the text stays as easy to edit as any.
type markPopover struct {
	open       bool
	start, end int // The marked stretch, in the document.
	revision   uint64
}

// annotationAt is the marked stretch at a document position.
func (e *Editor) annotationAt(at int) (annotationRun, bool) {
	leaf := e.Engine.LeafAt(at)
	if leaf.Block == nil {
		return annotationRun{}, false
	}
	local := at - leaf.Start
	for _, run := range annotationRuns(leaf.Block.Runs) {
		if run.start <= local && local < run.end || local == run.end && local > run.start {
			run.start += leaf.Start
			run.end += leaf.Start
			return run, true
		}
	}
	return annotationRun{}, false
}

// openMarkPopover opens the mark bar when a plain click lands in marked text.
func (e *Editor) openMarkPopover(at int) {
	e.markPop = markPopover{}
	if e.ReadOnly {
		return
	}
	if run, ok := e.annotationAt(at); ok {
		e.markPop = markPopover{open: true, start: run.start, end: run.end, revision: e.Revision()}
	}
}

// OpenMarkBar opens the mark bar for the marked text at a position, as a
// click on it does.
func (e *Editor) OpenMarkBar(at int) { e.openMarkPopover(at) }

// restyleMark changes the mark of the popover's stretch, keeping the caret.
func (e *Editor) restyleMark(variant, color string) {
	p := &e.markPop
	caret := e.Caret
	e.SetSelection(p.start, p.end)
	e.Annotate(variant, color)
	e.SetSelection(caret, caret)
	p.revision = e.Revision()
	if variant == "" {
		p.open = false
	}
}

// markBar draws the mark popover over its stretch while the caret stays in
// it and the text is unchanged.
func (e *Editor) markBar(c *ui.Context, surface ui.Element) {
	p := &e.markPop
	sel := e.Selection().Range
	if !p.open || !e.focused || e.ReadOnly || sel.Start != sel.End || sel.Start < p.start || sel.Start > p.end ||
		e.Revision() != p.revision || e.slashOpen || e.menuOpen || e.linkOpen {
		p.open = false
		return
	}
	// A drag from the click that selects text closes it above, as the
	// selection is no longer a caret.
	run, ok := e.annotationAt(p.start)
	if !ok || run.start != p.start || run.end != p.end {
		p.open = false
		return
	}
	first, _, ok := e.BoundsForRange(ui.TextInputRange{Start: p.start, End: p.start})
	if !ok {
		return
	}
	last, _, _ := e.BoundsForRange(ui.TextInputRange{Start: p.end, End: p.end})
	bounds := surface.Bounds()
	const w, h = float32(markBarWidth), float32(78)
	windowW, _ := c.Size()
	x := max(8, min(bounds.X+first.X-12, windowW-w-12))
	// Over the stretch's first line, clear of a circle or box round it.
	y := bounds.Y + first.Y - h - 14
	if y < bounds.Y+e.Scroll.Y+4 {
		y = bounds.Y + last.Y + last.H + 14
	}
	pg := e.page
	t := c.Theme()
	ui.Overlay(c, func() {
		bar := ui.Column(c.Key("mark-bar")).Absolute().Left(x).Top(y).Width(w).Padding(6, 8, 6, 8).Gap(4)
		e.popover(c, bar).Children(func() {
			ui.Row(c).Gap(2).Children(func() {
				for _, look := range annotationLooks {
					name := i18n.T(AnnotationNames[look.name])
					b := ui.ButtonBase(c.Key("mk:"+look.name)).Size(26, 26).Radius(4).Label(i18n.T("改为") + name).Tooltip(name).KeepFocus().Cursor(ui.CursorPointer)
					switch {
					case run.variant == look.name:
						b.Background(pg.TurnActive)
					case b.Hovered():
						b.Background(pg.Hover)
					}
					ink := e.annotationColor(look.name, run.color)
					text := t.Text
					b.Draw(func(p *ui.Painter, r ui.Rect) { paintAnnotationGlyph(p, look, r, "A", 12, ink, text) })
					if b.Clicked() && run.variant != look.name {
						e.restyleMark(look.name, run.color)
					}
				}
			})
			ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
				for _, name := range append([]string{""}, document.TextColors...) {
					label := i18n.T("标注自带颜色")
					if name != "" {
						label = i18n.T("标注颜色：") + ColorName(name, false)
					}
					s := ui.ButtonBase(c.Key("mc:"+name)).Size(26, 26).Radius(13).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
					if run.color == name {
						s.Border(2, pg.CodeHover)
					} else if s.Hovered() {
						s.Background(pg.Hover)
					}
					s.Children(func() { colorDot(c, pg, name) })
					if s.Clicked() && run.color != name {
						e.lastAnnotationColor = name
						e.restyleMark(run.variant, name)
					}
				}
				ui.Box(c).Grow(1)
				remove := ui.ButtonBase(c.Key("mark-remove")).Height(26).PaddingX(10).Radius(6).Center().Label(i18n.T("移除标注")).KeepFocus().Cursor(ui.CursorPointer)
				if remove.Hovered() {
					remove.Background(pg.DangerCell)
				}
				remove.Children(func() { ui.Text(c, i18n.T("移除标注")).FontSize(12).TextColor(t.Text) })
				if remove.Clicked() {
					e.restyleMark("", "")
				}
			})
		})
	})
}

// markBarWidth fits a row of the twelve marks.
const markBarWidth = 12*26 + 11*2 + 16

// colorDot is a mark color's swatch: its color, or for a mark's own color
// an outlined dot.
func colorDot(c *ui.Context, pg Page, name string) {
	t := c.Theme()
	dot := ui.Box(c).Size(16, 16).Radius(8)
	if name == "" {
		dot.Border(1.5, t.TextMuted).Center().Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(t.TextMuted)
		})
	} else {
		dot.Background(pg.TextColors[name])
	}
}
