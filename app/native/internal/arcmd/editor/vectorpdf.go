package editor

import (
	"fmt"
	"image"
	stdcolor "image/color"
	"math"
	"strings"
	"unicode"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/mermaid"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/pdf"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"github.com/egoist/mygo/ui"
)

func pdfRGBA(c ui.Color) [4]uint8 {
	s := c.SRGB()
	return [4]uint8{s.R, s.G, s.B, s.A}
}

func docYToPDFPage(docY float32, tops []float32, paperH, paperMargin float32, pdfHeight float64) (pageIdx int, pdfY float64) {
	if len(tops) > 0 {
		for i := len(tops) - 1; i >= 0; i-- {
			if tops[i] <= docY+1 {
				pageIdx = i
				break
			}
		}
	}
	top := float32(0)
	if pageIdx < len(tops) {
		top = tops[pageIdx]
	}
	localY := min(max(0, docY-top+paperMargin), paperH)
	return pageIdx, float64(localY) * (pdfHeight / float64(paperH))
}

func headingAnchorSlug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			prevDash = false
		} else if unicode.IsSpace(r) || r == '-' || unicode.IsPunct(r) {
			if b.Len() > 0 && !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ExportOutlines converts the laid-out document headings into hierarchical PDF
// bookmark items mapped to 0-based page numbers and top-down PDF points.
func (e *Editor) ExportOutlines(tops []float32, paperH, paperMargin float32, pdfHeight float64) []pdf.OutlineItem {
	out := make([]pdf.OutlineItem, 0, len(e.outline))
	for i := 0; i < len(e.outline); i++ {
		it := *e.indexedOutline(i)
		title := strings.TrimSpace(it.text)
		if title == "" {
			continue
		}
		pg, py := docYToPDFPage(it.y, tops, paperH, paperMargin, pdfHeight)
		out = append(out, pdf.OutlineItem{
			Title: title,
			Page:  pg,
			Y:     py,
			Level: max(1, it.level),
		})
	}
	return out
}

// ExportVectorPage writes the document range [top, bottom) as scalable PDF
// paths, rectangles, Type 3 vector glyphs, clickable links, and embedded images.
func (e *Editor) ExportVectorPage(vp *pdf.VectorPage, o Options, top, bottom float32, pageIdx, totalPages int, tops []float32, paperW, paperH, paperMargin float32) {
	if vp == nil || o.Theme == nil {
		return
	}
	t := o.Theme
	e.printing = true
	e.page = pageOf(o.Page, t)
	e.Scroll.Y, e.viewport = top, bottom-top
	pg := e.page

	sx := vp.Width / float64(paperW)
	sy := vp.Height / float64(paperH)
	sc := min(sx, sy)
	px := func(x float32) float64 { return float64(x) * sx }
	py := func(docY float32) float64 { return float64(docY-top+paperMargin) * sy }
	pw := func(w float32) float64 { return float64(w) * sx }
	ph := func(h float32) float64 { return float64(h) * sy }

	vp.FillRect(0, 0, vp.Width, vp.Height, 0, pdfRGBA(t.Background))

	vp.PushClip(0, float64(paperMargin)*sy, vp.Width, float64(bottom-top)*sy, 0)
	e.exportVectorHeader(vp, o, top, bottom, sx, sy, sc, px, py, pw, ph)
	e.exportVectorDecorations(vp, t, pg, top, bottom, sx, sy, sc, px, py, pw, ph)
	e.exportVectorRows(vp, o, t, pg, top, bottom, tops, paperH, paperMargin, sx, sy, sc, px, py, pw, ph)
	vp.PopClip()

	footer := fmt.Sprintf("%d / %d", pageIdx+1, totalPages)
	footerSize := 10.0 * sy
	footerW := estimateVectorTextWidth(footer, footerSize)
	footerX := (vp.Width - footerW) / 2
	footerY := vp.Height - float64(paperMargin)*sy/2 + 3*sy
	vp.DrawText(footer, footerX, footerY, 0, footerSize, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.TextMuted))
}

func estimateVectorTextWidth(s string, size float64) float64 {
	l := ui.ShapeText(s, ui.Font{Family: "system-ui", Size: float32(size)}, 0)
	w, _ := l.Size()
	return float64(w)
}

func (e *Editor) exportVectorHeader(
	vp *pdf.VectorPage, o Options, top, bottom float32,
	sx, sy, sc float64,
	px, py, pw, ph func(float32) float64,
) {
	h := &e.header
	pg := e.page
	if h.coverRect.H > 0 && h.coverRect.Y+h.coverRect.H > top && h.coverRect.Y < bottom {
		r := h.coverRect
		if !strings.HasPrefix(e.Doc.Cover, "gradient:") && o.LoadRawImage != nil {
			if img := o.LoadRawImage(e.Doc.Cover); img != nil {
				b := img.Bounds()
				iw, ih := b.Dx(), b.Dy()
				if iw > 0 && ih > 0 {
					s := max(r.W/float32(iw), r.H/float32(ih))
					w, hh := float32(iw)*s, float32(ih)*s
					place := ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-hh)*float32(e.coverY()+1)/2, W: w, H: hh}
					vp.PushClip(px(r.X), py(r.Y), pw(r.W), ph(r.H), 0)
					vp.DrawImage(img, px(place.X), py(place.Y), pw(place.W), ph(place.H), 0)
					vp.PopClip()
					goto afterCover
				}
			}
		}
		colors, ok := pg.Covers[strings.TrimPrefix(e.Doc.Cover, "gradient:")]
		if !ok {
			colors = [2]ui.Color{pg.Hover, pg.Hover}
		}
		const bands = 24
		bandH := r.H / bands
		for i := 0; i < bands; i++ {
			u := (float32(i) + 0.5) / bands
			col := colors[0].Mix(colors[1], u)
			vp.FillRect(px(r.X), py(r.Y+float32(i)*bandH), pw(r.W), ph(bandH+0.5), 0, pdfRGBA(col))
		}
	}
afterCover:
	if h.iconRect.H > 0 && e.Doc.Icon != "" && h.iconRect.Y+h.iconRect.H > top && h.iconRect.Y < bottom {
		ir := h.iconRect
		sz := float32(iconSize * .82)
		span := ui.Span{Text: e.Doc.Icon, Size: sz}
		w, th := ui.ShapeRichText([]ui.Span{span}, ui.Font{Size: sz}, 0).Size()
		if containsColorEmoji(e.Doc.Icon) {
			vp.DrawText(e.Doc.Icon, px(ir.X+(ir.W-w)/2), py(ir.Y+(ir.H+th)/2-th*0.18), pw(w), float64(sz)*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(tTextFallback(e.Doc.Icon)))
			e.drawEmojiSprite(vp, e.Doc.Icon, sz, ui.Color{}, px(ir.X+(ir.W-w)/2), py(ir.Y+(ir.H-th)/2), sx, sy)
		} else {
			vp.DrawText(e.Doc.Icon, px(ir.X+(ir.W-w)/2), py(ir.Y+(ir.H+th)/2-th*0.18), pw(w), float64(sz)*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(pg.Marker))
		}
	}
	_ = sc
}

func tTextFallback(_ string) ui.Color { return ui.RGB(31, 35, 41) }

func (e *Editor) exportVectorDecorations(
	vp *pdf.VectorPage, t *ui.Theme, pg Page, top, bottom float32,
	sx, sy, sc float64,
	px, py, pw, ph func(float32) float64,
) {
	for i := 0; i < len(e.decorations); i++ {
		d := *e.indexedDecoration(i)
		if d.rect.Y+d.rect.H < top-2 || d.rect.Y > bottom+2 {
			continue
		}
		x, y, w, h := px(d.rect.X), py(d.rect.Y), pw(d.rect.W), ph(d.rect.H)
		switch d.kind {
		case "code", "raw":
			vp.FillRect(x, y, w, h, 8*sc, pdfRGBA(pg.CodeFill))
			vp.StrokeRect(x, y, w, h, 8*sc, 1*sc, pdfRGBA(pg.CodeBorder), false)
			if d.kind == "code" {
				label := languageLabel(d.label)
				if label == "纯文本" || label == "Plain Text" || label == "" {
					label = "Plain Text"
				}
				vp.DrawText(label, px(d.rect.X+16), py(d.rect.Y+22), 0, 14*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Text))
				e.exportVectorLineNumbers(vp, d, pg, top, bottom, sx, sy, px, py)
			} else if d.label != "" {
				vp.DrawText(d.label, px(d.rect.X+20), py(d.rect.Y+20), 0, 11*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(pg.Caption))
			}

		case "mermaid":
			if !d.mermaidHidden {
				cardH := max(60, d.rect.H-2*mermaidMargin)
				cardY := d.rect.Y + mermaidMargin
				vp.FillRect(px(d.rect.X), py(cardY), pw(d.rect.W), ph(cardH), 12*sc, pdfRGBA(pg.Popover))
				border := pg.MathBorder
				if idx, ok := e.rowIDs[d.id]; ok && e.indexedRow(idx).mermaidError != nil {
					border = t.Danger.Alpha(0.5)
				}
				vp.StrokeRect(px(d.rect.X), py(cardY), pw(d.rect.W), ph(cardH), 12*sc, 1*sc, pdfRGBA(border), false)
				wellY := d.rect.Y + d.mermaidWellOff
				wellH := (cardY + cardH - 10) - wellY
				if d.rect.W > 24 && wellH > 12 {
					vp.FillRect(px(d.rect.X+10), py(wellY), pw(d.rect.W-20), ph(wellH), 8*sc, pdfRGBA(pg.CodeFill))
					vp.StrokeRect(px(d.rect.X+10), py(wellY), pw(d.rect.W-20), ph(wellH), 8*sc, 1*sc, pdfRGBA(pg.CodeBorder), false)
					vp.DrawText("Mermaid", px(d.rect.X+22), py(wellY+22), 0, 14*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Text))
					e.exportVectorLineNumbers(vp, d, pg, top, bottom, sx, sy, px, py)
				}
			}

		case "callout":
			look := pg.Callouts[d.tone]
			vp.FillRect(x, y, w, h, 8*sc, pdfRGBA(look.Fill))
			vp.StrokeRect(x, y, w, h, 8*sc, 1*sc, pdfRGBA(look.Border), false)
			icon := calloutIconRect(d)
			if d.label != "" {
				if containsColorEmoji(d.label) {
					vp.DrawText(d.label, px(icon.X+3), py(icon.Y+18), 0, 18*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Text))
					e.drawEmojiSprite(vp, d.label, 18, t.Text, px(icon.X+3), py(icon.Y+2), sx, sy)
				} else {
					vp.DrawText(d.label, px(icon.X+3), py(icon.Y+18), 0, 18*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(look.Icon))
				}
			} else {
				drawVectorCalloutIcon(vp, d.tone, px(icon.X+3), py(icon.Y+3), 18*sc, look.Icon)
			}

		case "quote":
			vp.FillRect(x, y, 2*sx, h, 1*sc, pdfRGBA(pg.QuoteBar))

		case "rule":
			vp.Line(x, py(d.rect.Y+12.5), x+w, py(d.rect.Y+12.5), 1*sc, pdfRGBA(pg.Rule), false)

		case "cell":
			if d.header || pg.CellFills[d.tone].A > 0 {
				vp.PushClip(px(d.table.X), py(d.table.Y), pw(d.table.W), ph(d.table.H), 8*sc)
				if d.header {
					vp.FillRect(x, y, w, h, 0, pdfRGBA(pg.TableHeader))
				}
				if fill, ok := pg.CellFills[d.tone]; ok && fill.A > 0 {
					vp.FillRect(x, y, w, h, 0, pdfRGBA(fill))
				}
				vp.PopClip()
			}

		case "table":
			vp.StrokeRect(x, y, w, h, 8*sc, 1*sc, pdfRGBA(pg.TableLine), false)
		}
	}

	for i := 0; i < len(e.decorations); i++ {
		d := *e.indexedDecoration(i)
		if d.kind != "cell" || d.rect.Y+d.rect.H < top-2 || d.rect.Y > bottom+2 {
			continue
		}
		x, y, w, h := px(d.rect.X), py(d.rect.Y), pw(d.rect.W), ph(d.rect.H)
		if d.right {
			vp.Line(x+w, y, x+w, y+h, 1*sc, pdfRGBA(pg.TableLine), false)
		}
		if d.below {
			vp.Line(x, y+h, x+w, y+h, 1*sc, pdfRGBA(pg.TableLine), false)
		}
	}
}

func (e *Editor) exportVectorLineNumbers(
	vp *pdf.VectorPage, d decoration, pg Page, top, bottom float32,
	sx, sy float64,
	px, py func(float32) float64,
) {
	idx, ok := e.rowIDs[d.id]
	if !ok {
		return
	}
	r := e.ensureRow(idx)
	if r.layout == nil {
		return
	}
	text := r.leaf.Block.Text()
	pos := 0
	at := 0
	for i := 0; ; i++ {
		nl := strings.IndexByte(text[pos:], '\n')
		var line string
		if nl < 0 {
			line = text[pos:]
		} else {
			line = text[pos : pos+nl]
		}
		caret := r.layout.Caret(at)
		lineY := r.y + caret.Y
		if lineY > bottom+4 {
			break
		}
		if lineY+max(caret.H, 22) >= top-4 {
			num := fmt.Sprintf("%d", i+1)
			span := ui.Span{Text: num, Font: "monospace", Size: 14, Features: "tnum"}
			w, _ := ui.ShapeRichText([]ui.Span{span}, ui.Font{Family: "monospace", Size: 14}, 0).Size()
			vp.DrawText(num, px(d.rect.X+36-w), py(lineY+15), float64(w)*sx, 14*sy, pdf.TextStyle{Family: pdf.FontMono}, pdfRGBA(pg.Caption))
		}
		if nl < 0 {
			break
		}
		pos += nl + 1
		at += document.Len(line) + 1
	}
}

func drawVectorCalloutIcon(vp *pdf.VectorPage, tone string, x, y, size float64, c ui.Color) {
	sc := size / 24.0
	col := pdfRGBA(c)
	lw := 1.8 * sc
	pt := func(vx, vy float64) (float64, float64) { return x + vx*sc, y + vy*sc }
	line := func(x1, y1, x2, y2 float64) {
		ax, ay := pt(x1, y1)
		bx, by := pt(x2, y2)
		vp.Line(ax, ay, bx, by, lw, col, false)
	}
	dot := func(vx, vy float64) {
		cx, cy := pt(vx, vy)
		vp.DrawCircle(cx, cy, 1.0*sc, col, col, 0)
	}
	switch tone {
	case "green":
		cx, cy := pt(12, 9)
		vp.DrawCircle(cx, cy, 5.5*sc, [4]uint8{}, col, lw)
		line(9, 18, 15, 18)
		line(10, 21.5, 14, 21.5)
	case "purple":
		var segs []pdf.PathSegment
		for i, p := range [][2]float64{{4, 3}, {20, 3}, {21, 16}, {7, 16}, {3, 21}} {
			tx, ty := pt(p[0], p[1])
			op := 1
			if i == 0 {
				op = 0
			}
			segs = append(segs, pdf.PathSegment{Op: op, X0: tx, Y0: ty})
		}
		segs = append(segs, pdf.PathSegment{Op: 4})
		vp.DrawPath(segs, [4]uint8{}, col, lw, false)
		line(12, 7, 12, 10)
		dot(12, 13)
	case "yellow":
		var segs []pdf.PathSegment
		for i, p := range [][2]float64{{12, 3}, {21.5, 20}, {2.5, 20}} {
			tx, ty := pt(p[0], p[1])
			op := 1
			if i == 0 {
				op = 0
			}
			segs = append(segs, pdf.PathSegment{Op: op, X0: tx, Y0: ty})
		}
		segs = append(segs, pdf.PathSegment{Op: 4})
		vp.DrawPath(segs, [4]uint8{}, col, lw, false)
		line(12, 9, 12, 13.5)
		dot(12, 16.8)
	case "red":
		var segs []pdf.PathSegment
		for i, p := range [][2]float64{{8.2, 2.5}, {15.8, 2.5}, {21.5, 8.2}, {21.5, 15.8}, {15.8, 21.5}, {8.2, 21.5}, {2.5, 15.8}, {2.5, 8.2}} {
			tx, ty := pt(p[0], p[1])
			op := 1
			if i == 0 {
				op = 0
			}
			segs = append(segs, pdf.PathSegment{Op: op, X0: tx, Y0: ty})
		}
		segs = append(segs, pdf.PathSegment{Op: 4})
		vp.DrawPath(segs, [4]uint8{}, col, lw, false)
		line(12, 8, 12, 12.5)
		dot(12, 16)
	default:
		cx, cy := pt(12, 12)
		vp.DrawCircle(cx, cy, 9.5*sc, [4]uint8{}, col, lw)
		line(12, 16, 12, 12)
		dot(12, 8)
	}
}

func (e *Editor) exportVectorRows(
	vp *pdf.VectorPage, o Options, t *ui.Theme, pg Page,
	top, bottom float32, tops []float32, paperH, paperMargin float32,
	sx, sy, sc float64,
	px, py, pw, ph func(float32) float64,
) {
	for i := 0; i < len(e.rows); i++ {
		r := *e.indexedRow(i)
		if r.rect.Y+r.rect.H <= top || r.rect.Y >= bottom {
			continue
		}
		if r.picture != nil {
			e.exportVectorImage(vp, o, t, pg, r, sx, sy, sc, px, py, pw, ph)
			if !e.captionVisible(r.leaf.Block) {
				continue
			}
		}
		e.exportVectorMarker(vp, t, pg, r, sx, sy, sc, px, py)

		if r.leaf.Block.Kind == "math" {
			if r.mathError != nil {
				ew, _ := r.mathError.Size()
				msg := i18nErrorText(r.mathError, r.mathErr)
				vp.DrawText(msg, px(r.rect.X+(r.rect.W-ew)/2), py(r.rect.Y+r.mathTop+14), pw(ew), 13*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Danger))
			} else if r.math != nil {
				e.exportVectorFormula(vp, *r.math, r.rect.X+(r.rect.W-r.math.W*r.mathScale)/2, r.rect.Y+r.mathTop, r.mathScale, sx, sy, top, paperMargin, t)
			}
			if r.mathHidden {
				continue
			}
		}

		if isMermaidBlock(r.leaf.Block) {
			if r.mermaidError != nil {
				ew, _ := r.mermaidError.Size()
				msg := r.mermaidErr
				if msg == "" {
					msg = "Mermaid syntax error"
				}
				vp.DrawText(msg, px(r.rect.X+(r.rect.W-ew)/2), py(r.rect.Y+r.mermaidTop+14), pw(ew), 13*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Danger))
			} else if r.mermaid != nil {
				e.exportVectorMermaid(vp, *r.mermaid, r.rect.X+(r.rect.W-r.mermaid.W*r.mermaidScale)/2, r.rect.Y+r.mermaidTop, r.mermaidScale, sx, sy, sc, top, paperMargin, t, pg)
			}
			if r.mermaidHidden {
				continue
			}
		}

		if r.layout == nil {
			continue
		}
		e.exportVectorAnnotations(vp, r, true, sx, sy, sc, top, paperMargin)
		e.exportVectorRowText(vp, t, pg, r, top, bottom, tops, paperH, paperMargin, sx, sy, sc, px, py, pw, ph)
		e.exportVectorAnnotations(vp, r, false, sx, sy, sc, top, paperMargin)
	}
}

func i18nErrorText(_ *ui.TextLayout, msg string) string { return msg }

func (e *Editor) exportVectorImage(
	vp *pdf.VectorPage, o Options, t *ui.Theme, pg Page, r row,
	sx, sy, sc float64,
	px, py, pw, ph func(float32) float64,
) {
	f := r.picture
	fb := f.bounds
	var raw image.Image
	if o.LoadRawImage != nil {
		raw = o.LoadRawImage(r.leaf.Block.URL)
	}
	if raw == nil && f.resource.Bitmap != nil {
		iw, ih := f.resource.Bitmap.Size()
		if iw > 0 && ih > 0 {
			raw = ui.Render(func(c *ui.Context) {
				ui.Box(c).Fill().Draw(func(p *ui.Painter, rect ui.Rect) {
					p.Image(f.resource.Bitmap, rect, ui.Contain)
				})
			}, max(1, int(math.Ceil(float64(fb.W)))), max(1, int(math.Ceil(float64(fb.H)))), 2)
		}
	}
	if raw != nil {
		vp.DrawImage(raw, px(fb.X), py(fb.Y), pw(fb.W), ph(fb.H), 8*sc)
		return
	}
	vp.StrokeRect(px(fb.X), py(fb.Y), pw(fb.W), ph(fb.H), 8*sc, 1*sc, pdfRGBA(pg.Caption), true)
	name := f.name
	if name == "" {
		name = imageName(r.leaf.Block.URL)
	}
	vp.DrawText(name, px(fb.X+36), py(fb.Y+22), 0, 14*sy, pdf.TextStyle{Family: pdf.FontSans, Bold: true}, pdfRGBA(t.Text))
	if f.resource.Problem != 0 {
		vp.DrawText(f.resource.Problem.text(), px(fb.X+14), py(fb.Y+f.reasonY+12), 0, 12*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(pg.Caption))
	}
}

func (e *Editor) exportVectorMarker(
	vp *pdf.VectorPage, t *ui.Theme, pg Page, r row,
	sx, sy, sc float64,
	px, py func(float32) float64,
) {
	kind := r.leaf.Block.Kind
	if kind != "bullet" && kind != "numbered" && kind != "task" {
		return
	}
	caret := r.layout.Caret(0)
	cy := r.y + caret.Y + max(caret.H, 26)/2
	col := pdfRGBA(pg.Marker)
	switch kind {
	case "bullet":
		switch r.leaf.Block.Indent % 3 {
		case 0:
			vp.DrawCircle(px(r.x-17.25), py(cy), 2.75*sc, col, [4]uint8{}, 0)
		case 1:
			vp.DrawCircle(px(r.x-17.25), py(cy), 2.75*sc, [4]uint8{}, col, 1*sc)
		default:
			vp.FillRect(px(r.x-19.75), py(cy-2.5), 5*sx, 5*sy, 0, col)
		}
	case "numbered":
		label := listNumber(max(1, r.number), r.leaf.Block.Indent) + "."
		span := ui.Span{Text: label, Size: 16, Features: "tnum"}
		w, _ := ui.ShapeRichText([]ui.Span{span}, ui.Font{Size: 16}, 0).Size()
		vp.DrawText(label, px(r.x-6-w), py(cy+5.5), float64(w)*sx, 16*sy, pdf.TextStyle{Family: pdf.FontSans}, col)
	case "task":
		bx, by := r.x-24, cy-8
		if r.leaf.Block.Checked {
			vp.FillRect(px(bx), py(by), 16*sx, 16*sy, 4*sc, pdfRGBA(pg.Marker.Alpha(0.10)))
			vp.StrokeRect(px(bx), py(by), 16*sx, 16*sy, 4*sc, 1.2*sc, col, false)
			segs := []pdf.PathSegment{
				{Op: 0, X0: px(bx + 4.2), Y0: py(by + 8.3)},
				{Op: 1, X0: px(bx + 6.9), Y0: py(by + 11.1)},
				{Op: 1, X0: px(bx + 11.9), Y0: py(by + 5.1)},
			}
			vp.DrawPath(segs, [4]uint8{}, col, 1.6*sc, false)
		} else {
			vp.FillRect(px(bx), py(by), 16*sx, 16*sy, 4*sc, pdfRGBA(t.Background))
			vp.StrokeRect(px(bx), py(by), 16*sx, 16*sy, 4*sc, 1*sc, pdfRGBA(pg.TaskBorder), false)
		}
	}
}

func (e *Editor) exportVectorFormula(
	vp *pdf.VectorPage, box tex.Box, docX, docY, scale float32,
	sx, sy float64, top, paperMargin float32, t *ui.Theme,
) {
	sc := min(sx, sy) * float64(scale)
	px := func(x float32) float64 { return float64(docX+x*scale) * sx }
	py := func(y float32) float64 { return float64(docY+(box.Ascent+y)*scale-top+paperMargin) * sy }
	for _, op := range box.Ops {
		switch op.Kind {
		case "text":
			col := t.Text
			if op.Error {
				col = t.Danger
			}
			st := pdf.TextStyle{Family: pdf.FontMath, Bold: op.Bold, Italic: op.Italic}
			if op.Italic {
				st.Family = pdf.FontMathItalic
			}
			vp.DrawText(op.Text, px(op.X), py(op.Y), 0, float64(op.Size*scale)*sy, st, pdfRGBA(col))
		case "rule":
			vp.FillRect(px(op.X), py(op.Y), float64(op.W*scale)*sx, float64(op.H*scale)*sy, 0, pdfRGBA(t.Text))
		case "path":
			if len(op.Points) < 2 {
				continue
			}
			segs := make([]pdf.PathSegment, len(op.Points))
			for i, pt := range op.Points {
				code := 1
				if i == 0 {
					code = 0
				}
				segs[i] = pdf.PathSegment{Op: code, X0: px(pt[0]), Y0: py(pt[1])}
			}
			vp.DrawPath(segs, [4]uint8{}, pdfRGBA(t.Text), max(0.5, float64(op.Width)*sc), false)
		}
	}
}

func (e *Editor) exportVectorMermaid(
	vp *pdf.VectorPage, diag mermaid.Diagram, docX, docY, scale float32,
	sx, sy, sc float64, top, paperMargin float32, t *ui.Theme, pg Page,
) {
	blue := pg.Callouts["blue"]
	yellow := pg.Callouts["yellow"]
	px := func(x float32) float64 { return float64(docX+x*scale) * sx }
	py := func(y float32) float64 { return float64(docY+y*scale-top+paperMargin) * sy }
	lineScale := sc * float64(scale)

	for _, s := range diag.Shapes {
		if s.Role == mermaid.RoleSubgraph || s.Role == mermaid.RoleFragment {
			exportMermaidShape(vp, s, px, py, sx*float64(scale), sy*float64(scale), lineScale, t, pg, blue, yellow)
		}
	}
	for _, pOp := range diag.Paths {
		exportMermaidPath(vp, pOp, docX, docY, scale, sx, sy, lineScale, top, paperMargin, t, pg)
	}
	for _, s := range diag.Shapes {
		if s.Role != mermaid.RoleSubgraph && s.Role != mermaid.RoleFragment {
			exportMermaidShape(vp, s, px, py, sx*float64(scale), sy*float64(scale), lineScale, t, pg, blue, yellow)
		}
	}

	for _, lb := range diag.Labels {
		fg := t.Text
		if lb.Role == mermaid.RoleSubgraph || lb.Role == mermaid.RoleFragment {
			fg = pg.Caption
		}
		if lb.Pill {
			vp.FillRect(px(lb.X-4), py(lb.Y-1), float64((lb.W+8)*scale)*sx, float64((lb.H+2)*scale)*sy, 4*lineScale, pdfRGBA(t.Background.Alpha(0.92)))
		}
		tl := e.mermaidTextLayout(lb.Text, lb.Size, lb.Bold, lb.Align, max(8, lb.W))
		_, th := tl.Size()
		startY := lb.Y + (lb.H-th)/2
		lines := tl.Lines()
		for _, ln := range lines {
			sub := lb.Text[ui.UTF16ByteOffset(lb.Text, ln.Start):ui.UTF16ByteOffset(lb.Text, ln.End)]
			if strings.TrimSpace(sub) == "" {
				continue
			}
			lineX := lb.X + ln.Rect.X
			vp.DrawText(sub, px(lineX), py(startY+ln.Baseline), float64(ln.Rect.W*scale)*sx, float64(lb.Size*scale)*sy, pdf.TextStyle{Family: pdf.FontSans, Bold: lb.Bold}, pdfRGBA(fg))
		}
	}
}

func mermaidColorsFor(role mermaid.Role, kind mermaid.ShapeKind, t *ui.Theme, pg Page, blue, yellow Callout) (fill, border ui.Color, strokeW float32) {
	strokeW = 1.25
	switch role {
	case mermaid.RoleSubgraph, mermaid.RoleFragment:
		if kind == mermaid.ShapePentagon {
			return pg.CodeHover.Alpha(0.28), pg.CodeBorder, 1
		}
		return pg.CodeFill.Alpha(0.50), pg.CodeBorder, 1
	case mermaid.RoleActor:
		return blue.Fill, blue.Border, strokeW
	case mermaid.RoleNote:
		return yellow.Fill, yellow.Border, strokeW
	case mermaid.RoleActivation:
		return t.Accent.Alpha(0.18), t.Accent.Alpha(0.68), 1
	default:
		return pg.CodeFill, t.Accent.Alpha(0.45), strokeW
	}
}

func exportMermaidShape(
	vp *pdf.VectorPage, s mermaid.ShapeOp,
	px, py func(float32) float64,
	sx, sy, sc float64,
	t *ui.Theme, pg Page, blue, yellow Callout,
) {
	fill, border, sw := mermaidColorsFor(s.Role, s.Kind, t, pg, blue, yellow)
	fc, bc := pdfRGBA(fill), pdfRGBA(border)
	lw := float64(sw) * sc
	x, y, w, h := px(s.X), py(s.Y), float64(s.W)*sx, float64(s.H)*sy

	switch s.Kind {
	case mermaid.ShapeRect:
		vp.FillRect(x, y, w, h, float64(s.Radius)*sc, fc)
		vp.StrokeRect(x, y, w, h, float64(s.Radius)*sc, lw, bc, s.Dashed)
	case mermaid.ShapeRound:
		rad := float64(max(s.Radius, 6)) * sc
		vp.FillRect(x, y, w, h, rad, fc)
		vp.StrokeRect(x, y, w, h, rad, lw, bc, s.Dashed)
	case mermaid.ShapeStadium:
		rad := min(w, h) / 2
		vp.FillRect(x, y, w, h, rad, fc)
		vp.StrokeRect(x, y, w, h, rad, lw, bc, s.Dashed)
	case mermaid.ShapeCircle:
		rad := min(w, h) / 2
		vp.DrawCircle(x+w/2, y+h/2, rad, fc, bc, lw)
	case mermaid.ShapeSubroutine:
		vp.FillRect(x, y, w, h, 4*sc, fc)
		vp.StrokeRect(x, y, w, h, 4*sc, lw, bc, s.Dashed)
		inset := min(8*sx, w*0.14)
		vp.Line(x+inset, y, x+inset, y+h, lw, bc, false)
		vp.Line(x+w-inset, y, x+w-inset, y+h, lw, bc, false)
	case mermaid.ShapeCylinder:
		ry := min(7*sy, h*0.18)
		kx := w * 0.28
		segs := []pdf.PathSegment{
			{Op: 0, X0: x, Y0: y + ry},
			{Op: 1, X0: x, Y0: y + h - ry},
			{Op: 3, X0: x, Y0: y + h - ry + ry*0.9, X1: x + kx, Y1: y + h, X2: x + w/2, Y2: y + h},
			{Op: 3, X0: x + w - kx, Y0: y + h, X1: x + w, Y1: y + h - ry + ry*0.9, X2: x + w, Y2: y + h - ry},
			{Op: 1, X0: x + w, Y0: y + ry},
			{Op: 3, X0: x + w, Y0: y + ry - ry*0.9, X1: x + w - kx, Y1: y, X2: x + w/2, Y2: y},
			{Op: 3, X0: x + kx, Y0: y, X1: x, Y1: y + ry - ry*0.9, X2: x, Y2: y + ry},
			{Op: 4},
		}
		vp.DrawPath(segs, fc, bc, lw, false)
		rim := []pdf.PathSegment{
			{Op: 0, X0: x, Y0: y + ry},
			{Op: 3, X0: x, Y0: y + ry + ry*0.9, X1: x + kx, Y1: y + 2*ry, X2: x + w/2, Y2: y + 2*ry},
			{Op: 3, X0: x + w - kx, Y0: y + 2*ry, X1: x + w, Y1: y + ry + ry*0.9, X2: x + w, Y2: y + ry},
		}
		vp.DrawPath(rim, [4]uint8{}, bc, lw, false)
	case mermaid.ShapeDiamond:
		cx, cy := x+w/2, y+h/2
		segs := []pdf.PathSegment{
			{Op: 0, X0: cx, Y0: y},
			{Op: 1, X0: x + w, Y0: cy},
			{Op: 1, X0: cx, Y0: y + h},
			{Op: 1, X0: x, Y0: cy},
			{Op: 4},
		}
		vp.DrawPath(segs, fc, bc, lw, s.Dashed)
	case mermaid.ShapeHexagon:
		inset := min(14*sx, w*0.18)
		cy := y + h/2
		segs := []pdf.PathSegment{
			{Op: 0, X0: x + inset, Y0: y},
			{Op: 1, X0: x + w - inset, Y0: y},
			{Op: 1, X0: x + w, Y0: cy},
			{Op: 1, X0: x + w - inset, Y0: y + h},
			{Op: 1, X0: x + inset, Y0: y + h},
			{Op: 1, X0: x, Y0: cy},
			{Op: 4},
		}
		vp.DrawPath(segs, fc, bc, lw, s.Dashed)
	case mermaid.ShapeParallelogram:
		skew := min(14*sx, w*0.18)
		segs := []pdf.PathSegment{
			{Op: 0, X0: x + skew, Y0: y},
			{Op: 1, X0: x + w, Y0: y},
			{Op: 1, X0: x + w - skew, Y0: y + h},
			{Op: 1, X0: x, Y0: y + h},
			{Op: 4},
		}
		vp.DrawPath(segs, fc, bc, lw, s.Dashed)
	case mermaid.ShapePentagon:
		cut := min(7*sc, min(w, h)*0.35)
		segs := []pdf.PathSegment{
			{Op: 0, X0: x, Y0: y},
			{Op: 1, X0: x + w, Y0: y},
			{Op: 1, X0: x + w, Y0: y + h - cut},
			{Op: 1, X0: x + w - cut, Y0: y + h},
			{Op: 1, X0: x, Y0: y + h},
			{Op: 4},
		}
		vp.DrawPath(segs, fc, bc, lw, s.Dashed)
	case mermaid.ShapeActor:
		cx := x + w/2
		headR := min(w, h) * 0.22
		headY := y + headR + 1*sy
		vp.DrawCircle(cx, headY, headR, fc, bc, lw)
		neckY := headY + headR
		hipY := y + h*0.66
		armY := y + h*0.44
		vp.Line(cx, neckY, cx, hipY, lw, bc, false)
		vp.Line(cx-headR*1.5, armY, cx+headR*1.5, armY, lw, bc, false)
		vp.Line(cx, hipY, cx-headR*1.2, y+h-1*sy, lw, bc, false)
		vp.Line(cx, hipY, cx+headR*1.2, y+h-1*sy, lw, bc, false)
	}
}

func exportMermaidPath(
	vp *pdf.VectorPage, op mermaid.PathOp, docX, docY, scale float32,
	sx, sy, sc float64, top, paperMargin float32, t *ui.Theme, pg Page,
) {
	if len(op.Points) < 2 {
		return
	}
	pts := make([][2]float32, len(op.Points))
	for i, pt := range op.Points {
		pts[i] = [2]float32{docX + pt[0]*scale, docY + pt[1]*scale}
	}
	col := pg.Caption
	width := float32(1.35)
	if op.Style == mermaid.EdgeThick {
		width = 2.3
		col = t.Text.Alpha(0.72)
	} else if op.Role == mermaid.RoleFragment {
		col = pg.CodeBorder
		width = 1
	}

	drawPts := append([][2]float32(nil), pts...)
	if op.StartMarker == mermaid.MarkerArrow {
		if ux, uy, d := segDirLen(pts[1], pts[0]); d >= 8*scale {
			drawPts[0] = [2]float32{pts[0][0] - ux*5*scale, pts[0][1] - uy*5*scale}
		}
	}
	if op.EndMarker == mermaid.MarkerArrow {
		n := len(drawPts)
		if ux, uy, d := segDirLen(drawPts[n-2], drawPts[n-1]); d >= 8*scale {
			drawPts[n-1] = [2]float32{pts[n-1][0] - ux*5*scale, pts[n-1][1] - uy*5*scale}
		}
	}
	px := func(v float32) float64 { return float64(v) * sx }
	py := func(v float32) float64 { return float64(v-top+paperMargin) * sy }

	var segs []pdf.PathSegment
	segs = append(segs, pdf.PathSegment{Op: 0, X0: px(drawPts[0][0]), Y0: py(drawPts[0][1])})
	if op.Smooth && len(drawPts) >= 3 {
		for i := 1; i < len(drawPts)-1; i++ {
			prev, cur, next := drawPts[i-1], drawPts[i], drawPts[i+1]
			ux1, uy1, d1 := segDirLen(prev, cur)
			ux2, uy2, d2 := segDirLen(cur, next)
			if d1 < 2 || d2 < 2 {
				segs = append(segs, pdf.PathSegment{Op: 1, X0: px(cur[0]), Y0: py(cur[1])})
				continue
			}
			rad := min(float32(8)*scale, min(d1, d2)*0.35)
			inX, inY := cur[0]-ux1*rad, cur[1]-uy1*rad
			outX, outY := cur[0]+ux2*rad, cur[1]+uy2*rad
			segs = append(segs, pdf.PathSegment{Op: 1, X0: px(inX), Y0: py(inY)})
			segs = append(segs, pdf.PathSegment{Op: 2, X0: px(cur[0]), Y0: py(cur[1]), X1: px(outX), Y1: py(outY)})
		}
		last := drawPts[len(drawPts)-1]
		segs = append(segs, pdf.PathSegment{Op: 1, X0: px(last[0]), Y0: py(last[1])})
	} else {
		for _, pt := range drawPts[1:] {
			segs = append(segs, pdf.PathSegment{Op: 1, X0: px(pt[0]), Y0: py(pt[1])})
		}
	}
	vp.DrawPath(segs, [4]uint8{}, pdfRGBA(col), float64(width)*sc, op.Style == mermaid.EdgeDashed)

	if op.StartMarker != mermaid.MarkerNone {
		if ux, uy, ok := segUnit(pts[1], pts[0]); ok {
			exportMermaidMarker(vp, op.StartMarker, pts[0][0], pts[0][1], ux, uy, width, scale, sx, sy, sc, top, paperMargin, col, t.Background)
		}
	}
	if op.EndMarker != mermaid.MarkerNone {
		n := len(pts)
		if ux, uy, ok := segUnit(pts[n-2], pts[n-1]); ok {
			exportMermaidMarker(vp, op.EndMarker, pts[n-1][0], pts[n-1][1], ux, uy, width, scale, sx, sy, sc, top, paperMargin, col, t.Background)
		}
	}
}

func exportMermaidMarker(
	vp *pdf.VectorPage, m mermaid.MarkerKind, tipX, tipY, ux, uy, strokeW, scale float32,
	sx, sy, sc float64, top, paperMargin float32, col, bg ui.Color,
) {
	px := func(v float32) float64 { return float64(v) * sx }
	py := func(v float32) float64 { return float64(v-top+paperMargin) * sy }
	nx, ny := -uy, ux
	switch m {
	case mermaid.MarkerArrow:
		sz, hw := float32(9)*scale, float32(4.2)*scale
		segs := []pdf.PathSegment{
			{Op: 0, X0: px(tipX), Y0: py(tipY)},
			{Op: 1, X0: px(tipX - ux*sz + nx*hw), Y0: py(tipY - uy*sz + ny*hw)},
			{Op: 1, X0: px(tipX - ux*sz - nx*hw), Y0: py(tipY - uy*sz - ny*hw)},
			{Op: 4},
		}
		vp.DrawPath(segs, pdfRGBA(col), [4]uint8{}, 0, false)
	case mermaid.MarkerOpenArrow:
		sz, hw := float32(8.5)*scale, float32(4.5)*scale
		segs := []pdf.PathSegment{
			{Op: 0, X0: px(tipX - ux*sz + nx*hw), Y0: py(tipY - uy*sz + ny*hw)},
			{Op: 1, X0: px(tipX), Y0: py(tipY)},
			{Op: 1, X0: px(tipX - ux*sz - nx*hw), Y0: py(tipY - uy*sz - ny*hw)},
		}
		vp.DrawPath(segs, [4]uint8{}, pdfRGBA(col), float64(max(1.3, strokeW))*sc, false)
	case mermaid.MarkerCircle:
		r := float32(3.8) * scale
		cx, cy := tipX-ux*r, tipY-uy*r
		vp.DrawCircle(px(cx), py(cy), float64(r)*min(sx, sy)*1.0, pdfRGBA(bg), pdfRGBA(col), float64(max(1.3, strokeW))*sc)
	case mermaid.MarkerCross:
		r := float32(3.8) * scale
		cx, cy := tipX-ux*5*scale, tipY-uy*5*scale
		dx1, dy1 := (ux+nx)*r*0.75, (uy+ny)*r*0.75
		dx2, dy2 := (ux-nx)*r*0.75, (uy-ny)*r*0.75
		lw := float64(max(1.4, strokeW)) * sc
		vp.Line(px(cx-dx1), py(cy-dy1), px(cx+dx1), py(cy+dy1), lw, pdfRGBA(col), false)
		vp.Line(px(cx-dx2), py(cy-dy2), px(cx+dx2), py(cy+dy2), lw, pdfRGBA(col), false)
	}
}

type spanSeg struct {
	start, end int
	span       ui.Span
}

func (e *Editor) exportVectorRowText(
	vp *pdf.VectorPage, t *ui.Theme, pg Page, r row,
	top, bottom float32, tops []float32, paperH, paperMargin float32,
	sx, sy, sc float64,
	px, py, pw, ph func(float32) float64,
) {
	m := e.blockMetrics(r.leaf.Block, r.rect.W, r.tableHeader)
	font := m.font
	spans := e.blockSpans(r.leaf.Block, font, r.tableHeader, t)
	blockText := r.leaf.Block.Text()

	segs := make([]spanSeg, 0, len(spans))
	uOff := 0
	for _, sp := range spans {
		n := document.Len(sp.Text)
		if n > 0 {
			segs = append(segs, spanSeg{start: uOff, end: uOff + n, span: sp})
		}
		uOff += n
	}

	baseColor := t.Text
	if r.leaf.Block.Kind == "image" {
		baseColor = pg.Caption
	}

	_, clipBox, _, _, scrollX, noWrap := e.codeScrollFor(r)
	if noWrap {
		vp.PushClip(px(clipBox.X), py(clipBox.Y), pw(clipBox.W), ph(clipBox.H), 0)
	}

	objRanges := make([][2]int, 0, len(r.layout.objects))
	for _, o := range r.layout.objects {
		if o.end > o.start {
			objRanges = append(objRanges, [2]int{o.start, o.end})
		}
	}

	for _, line := range r.layout.Lines() {
		if r.y+line.Rect.Y+line.Rect.H < top-2 || r.y+line.Rect.Y > bottom+2 {
			continue
		}
		// Pass 1: inline span backgrounds (code pills, highlights, tags).
		for _, sg := range segs {
			if sg.span.Background.A == 0 {
				continue
			}
			for _, sub := range subtractRanges(max(line.Start, sg.start), min(line.End, sg.end), objRanges) {
				for _, rc := range r.layout.SelectionRects(ui.TextInputRange{Start: sub[0], End: sub[1]}) {
					rx, ry, rw, rh := rc.X, rc.Y, rc.W, rc.H
					rad := float64(sg.span.Radius) * sc
					if sg.span.Radius > 0 {
						rx -= 1.5
						rw += 3
					}
					vp.FillRect(px(r.x-scrollX+rx), py(r.y+ry), pw(rw), ph(rh), rad, pdfRGBA(sg.span.Background))
				}
			}
		}

		// Pass 2: styled text runs, emoji sprites, underlines, strikethroughs.
		for _, sg := range segs {
			style := pdfTextStyleFor(font, sg.span)
			sz := sg.span.Size
			if sz <= 0 {
				sz = font.Size
			}
			col := sg.span.Color
			if col.A == 0 {
				col = baseColor
			}
			baseline := r.y + line.Baseline - sg.span.BaselineShift
			for _, sub := range subtractRanges(max(line.Start, sg.start), min(line.End, sg.end), objRanges) {
				for _, chunk := range splitEmojiSubranges(blockText, sub[0], sub[1]) {
					rects := r.layout.SelectionRects(ui.TextInputRange{Start: chunk.start, End: chunk.end})
					if len(rects) == 0 {
						continue
					}
					bStart := ui.UTF16ByteOffset(blockText, chunk.start)
					bEnd := ui.UTF16ByteOffset(blockText, chunk.end)
					substr := blockText[bStart:bEnd]
					if substr == "" {
						continue
					}
					rc := rects[0]
					if len(rects) > 1 {
						minX, maxX := rc.X, rc.X+rc.W
						for _, alt := range rects[1:] {
							minX = min(minX, alt.X)
							maxX = max(maxX, alt.X+alt.W)
						}
						rc.X, rc.W = minX, maxX-minX
					}
					vp.DrawText(substr, px(r.x-scrollX+rc.X), py(baseline), pw(rc.W), float64(sz)*sy, style, pdfRGBA(col))
					if chunk.emoji {
						e.drawEmojiSprite(vp, substr, sz, col, px(r.x-scrollX+rc.X), py(r.y+rc.Y), sx, sy)
					}
					if sg.span.Underline || sg.span.Strikethrough {
						dc := sg.span.DecorationColor
						if dc.A == 0 {
							dc = col
						}
						lw := max(0.75, float64(sz)*0.065*sy)
						x0, x1 := px(r.x-scrollX+rc.X), px(r.x-scrollX+rc.X+rc.W)
						if sg.span.Underline {
							uy := py(baseline + sz*0.14)
							vp.Line(x0, uy, x1, uy, lw, pdfRGBA(dc), false)
						}
						if sg.span.Strikethrough {
							syLine := py(baseline - sz*0.30)
							vp.Line(x0, syLine, x1, syLine, lw, pdfRGBA(dc), false)
						}
					}
				}
			}
		}
	}

	// Render inline objects (inline math, footnote refs, sup/sub scripts).
	for _, o := range r.layout.objects {
		if r.y+o.rect.Y+o.rect.H < top-2 || r.y+o.rect.Y > bottom+2 {
			continue
		}
		if o.box != nil {
			e.exportVectorFormula(vp, *o.box, r.x+o.rect.X+2, r.y+o.rect.Y, o.scale, sx, sy, top, paperMargin, t)
			continue
		}
		if o.label != nil {
			col := t.Accent
			if o.script {
				col = t.Text
			}
			labelText := ""
			if o.note != "" {
				if n, ok := e.notes[o.note]; ok {
					labelText = fmt.Sprint(n.number)
				}
			} else if o.end > o.start {
				bStart := ui.UTF16ByteOffset(blockText, o.start)
				bEnd := ui.UTF16ByteOffset(blockText, o.end)
				labelText = blockText[bStart:bEnd]
			}
			if labelText == "" {
				continue
			}
			base := float32(font.Size * 0.72)
			if lines := o.label.Lines(); len(lines) > 0 {
				base = lines[0].Baseline
			}
			ox := r.x - scrollX + o.rect.X
			if !o.script {
				ox += 2
			}
			vp.DrawText(labelText, px(ox), py(r.y+o.rect.Y+base), pw(o.rect.W), float64(font.Size*0.72)*sy, pdfTextStyleFor(font, ui.Span{}), pdfRGBA(col))
		}
	}

	if r.leaf.Block.Kind == "footnote" {
		n := e.notes[r.leaf.Block.NoteLabel]
		label := fmt.Sprintf("%d ↩", n.number)
		vp.DrawText(label, px(r.rect.X), py(r.y+12), 0, 12*sy, pdf.TextStyle{Family: pdf.FontSans}, pdfRGBA(t.Accent))
	}

	if noWrap {
		vp.PopClip()
	}

	// Clickable hyperlink / anchor annotations.
	off := 0
	for _, run := range r.leaf.Block.Runs {
		n := document.Len(run.Text)
		if run.Style.Link != "" && n > 0 {
			for _, rc := range r.layout.SelectionRects(ui.TextInputRange{Start: off, End: off + n}) {
				if r.y+rc.Y+rc.H <= top || r.y+rc.Y >= bottom {
					continue
				}
				lx, ly, lw, lh := px(r.x-scrollX+rc.X), py(r.y+rc.Y), pw(rc.W), ph(rc.H)
				if strings.HasPrefix(run.Style.Link, "#") {
					if targetY, ok := e.resolveOutlineAnchor(strings.TrimPrefix(run.Style.Link, "#")); ok {
						tp, ty := docYToPDFPage(targetY, tops, paperH, paperMargin, vp.Height)
						vp.AddLinkDest(lx, ly, lw, lh, tp, ty)
					}
				} else {
					vp.AddLinkURI(lx, ly, lw, lh, run.Style.Link)
				}
			}
		}
		off += n
	}
}

func (e *Editor) resolveOutlineAnchor(target string) (float32, bool) {
	if target == "" {
		return 0, false
	}
	slug := headingAnchorSlug(target)
	for i := 0; i < len(e.outline); i++ {
		it := *e.indexedOutline(i)
		if it.id == target || strings.EqualFold(strings.TrimSpace(it.text), strings.TrimSpace(target)) || (slug != "" && headingAnchorSlug(it.text) == slug) {
			return it.y, true
		}
	}
	return 0, false
}

func pdfTextStyleFor(base ui.Font, sp ui.Span) pdf.TextStyle {
	family := base.Family
	if sp.Font != "" {
		family = sp.Font
	}
	pdfFam := pdf.FontSans
	low := strings.ToLower(family)
	switch {
	case strings.Contains(low, "mono"):
		pdfFam = pdf.FontMono
	case strings.Contains(low, "serif") || strings.Contains(low, "georgia") || strings.Contains(low, "song"):
		pdfFam = pdf.FontSerif
	}
	return pdf.TextStyle{
		Family: pdfFam,
		Bold:   sp.Weight >= 600 || base.Weight >= 600,
		Italic: sp.Italic || base.Italic,
	}
}

func subtractRanges(a, b int, holes [][2]int) [][2]int {
	if a >= b {
		return nil
	}
	cur := [][2]int{{a, b}}
	for _, h := range holes {
		var next [][2]int
		for _, seg := range cur {
			if h[1] <= seg[0] || h[0] >= seg[1] {
				next = append(next, seg)
				continue
			}
			if h[0] > seg[0] {
				next = append(next, [2]int{seg[0], h[0]})
			}
			if h[1] < seg[1] {
				next = append(next, [2]int{h[1], seg[1]})
			}
		}
		cur = next
	}
	return cur
}

type textChunk struct {
	start, end int
	emoji      bool
}

func isColorEmojiRune(r rune) bool {
	return r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) || r == 0xFE0F || r == 0x200D
}

func containsColorEmoji(s string) bool {
	return strings.IndexFunc(s, isColorEmojiRune) >= 0
}

func splitEmojiSubranges(blockText string, uStart, uEnd int) []textChunk {
	bStart := ui.UTF16ByteOffset(blockText, uStart)
	bEnd := ui.UTF16ByteOffset(blockText, uEnd)
	sub := blockText[bStart:bEnd]
	if sub == "" {
		return nil
	}
	if !containsColorEmoji(sub) {
		return []textChunk{{start: uStart, end: uEnd}}
	}
	var out []textChunk
	pos := uStart
	curStart := uStart
	curEmoji := false
	first := true
	for _, r := range sub {
		n := document.Len(string(r))
		em := isColorEmojiRune(r)
		if first {
			curEmoji = em
			first = false
		} else if em != curEmoji {
			// Keep variation selector / ZWJ attached to emoji clusters.
			if !(curEmoji && (r == 0xFE0F || r == 0x200D)) {
				out = append(out, textChunk{start: curStart, end: pos, emoji: curEmoji})
				curStart = pos
				curEmoji = em
			}
		}
		pos += n
	}
	if pos > curStart {
		out = append(out, textChunk{start: curStart, end: pos, emoji: curEmoji})
	}
	return out
}

func (e *Editor) drawEmojiSprite(vp *pdf.VectorPage, emoji string, size float32, color ui.Color, x, y, sx, sy float64) {
	span := ui.Span{Text: emoji, Size: size, Color: color}
	l := ui.ShapeRichText([]ui.Span{span}, ui.Font{Size: size}, 0)
	w, h := l.Size()
	if w <= 0 || h <= 0 {
		return
	}
	cw := max(2, int(math.Ceil(float64(w)))+2)
	ch := max(2, int(math.Ceil(float64(h)))+2)
	renderMatte := func(bg ui.Color) *image.RGBA {
		return ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Background(bg).Draw(func(p *ui.Painter, rect ui.Rect) {
				p.TextLayout(l, rect.X, rect.Y, color)
			})
		}, cw, ch, 2)
	}
	whiteImg := renderMatte(ui.RGB(255, 255, 255))
	blackImg := renderMatte(ui.RGB(0, 0, 0))
	bounds := blackImg.Bounds()
	out := image.NewNRGBA(bounds)
	for yy := bounds.Min.Y; yy < bounds.Max.Y; yy++ {
		for xx := bounds.Min.X; xx < bounds.Max.X; xx++ {
			wc := whiteImg.RGBAAt(xx, yy)
			bc := blackImg.RGBAAt(xx, yy)
			diff := (int(wc.R) - int(bc.R) + int(wc.G) - int(bc.G) + int(wc.B) - int(bc.B) + 1) / 3
			a := max(0, min(255, 255-diff))
			if a == 0 {
				continue
			}
			r := uint8(min(255, (int(bc.R)*255+a/2)/a))
			g := uint8(min(255, (int(bc.G)*255+a/2)/a))
			b := uint8(min(255, (int(bc.B)*255+a/2)/a))
			out.SetNRGBA(xx, yy, stdcolor.NRGBA{R: r, G: g, B: b, A: uint8(a)})
		}
	}
	vp.DrawImage(out, x, y, float64(cw)*sx, float64(ch)*sy, 0)
}

func (e *Editor) exportVectorAnnotations(
	vp *pdf.VectorPage, r row, under bool,
	sx, sy, sc float64, top, paperMargin float32,
) {
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
	px := func(v float32) float64 { return float64(v) * sx }
	py := func(v float32) float64 { return float64(v-top+paperMargin) * sy }
	for _, run := range runs {
		look, ok := lookOf(run.variant)
		if !ok || look.under != under {
			continue
		}
		color := e.annotationColor(run.variant, run.color)
		rects := r.layout.SelectionRects(ui.TextInputRange{Start: run.start, End: run.end})
		if k := len(rects) - 1; k >= 0 && look.pad > 0 {
			rects[k].W -= look.pad * size
		}
		for i, rect := range rects {
			if rect.W < 1 {
				continue
			}
			rect.X += r.x
			rect.Y += r.y
			seed := uint32(r.leaf.Start+run.start)*2654435761 + uint32(i)*40503
			box := annotationBox(look, rect, size)
			switch {
			case look.bar:
				vp.FillRect(px(box.X), py(box.Y+box.H/2-1), float64(box.W)*sx, 2*sy, 1*sc, pdfRGBA(color))
			case look.dots:
				step := size / 2
				for dx := box.X + step/2; dx <= box.X+box.W; dx += step {
					vp.DrawCircle(px(dx), py(box.Y+box.H/2), 1.5*sc, pdfRGBA(color), [4]uint8{}, 0)
				}
			default:
				bx, by := box.W/look.view[0], box.H/look.view[1]
				for _, s := range look.strokes {
					disp, freq := float32(1.5), float32(.035)
					if s.rough {
						disp, freq = 2.6, .045
					}
					subpaths := flattenSVG(s.d)
					var segs []pdf.PathSegment
					for _, sub := range subpaths {
						for k, pt := range sub {
							ax, ay := box.X+pt[0]*bx, box.Y+pt[1]*by
							odx, ody := roughOffset(ax, ay, freq, seed)
							ax += odx * disp
							ay += ody * disp
							opCode := 1
							if k == 0 {
								opCode = 0
							}
							segs = append(segs, pdf.PathSegment{Op: opCode, X0: px(ax), Y0: py(ay)})
						}
					}
					c := color.Alpha(float32(color.A) / 255 * s.alpha)
					if s.width == 0 {
						c = color.Alpha(float32(color.A) / 255 * .45)
						vp.DrawPath(segs, pdfRGBA(c), [4]uint8{}, 0, false)
					} else {
						lw := float64(min(3.4, max(1.2, s.width*by*1.1))) * sc
						vp.DrawPath(segs, [4]uint8{}, pdfRGBA(c), lw, false)
					}
				}
			}
		}
	}
}
