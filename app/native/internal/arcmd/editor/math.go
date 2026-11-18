package editor

import (
	"fmt"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"github.com/egoist/mygo/ui"
)

// Display math renders as an unboxed formula at rest, and while editing opens
// a unified card with the live centered formula on top and a rounded monospace
// TeX input bar directly below.
const (
	mathSize     = 16 * 1.21 // KaTeX's size relative to the document body.
	mathMargin   = 18
	mathPaddingX = 12
	mathPaddingY = 8
	mathFenceH   = 20
	mathPreviewY = 16
	mathPreviewW = 520
)

// mathFont is the family formulas are set in: a book face, as TeX's.
const mathFont = "KaTeX_Main, Times New Roman, STIX Two Text, Noto Serif, serif"

// mathShape is a math block's formula, laid out for its TeX.
type mathShape struct {
	src string
	box tex.Box
	err error
}

// watchMath notes the math block the caret is in, which shows its TeX.
func (e *Editor) watchMath() {
	e.mathEditing = ""
	if !e.focused || e.printing || e.ReadOnly {
		return
	}
	if leaf := e.Engine.LeafAt(e.Caret); leaf.Block.Kind == "math" && e.Anchor >= leaf.Start && e.Anchor <= leaf.End {
		e.mathEditing = leaf.Block.ID
	}
}

// exitDisplayMath jumps out of a display math block onto the block below (or
// inserts a new paragraph below if at the end of the parent list).
func (e *Editor) exitDisplayMath() bool {
	if e.ReadOnly || e.Anchor != e.Caret {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind != "math" || leaf.Parent == nil {
		return false
	}
	if leaf.Index+1 < len(*leaf.Parent) {
		next := (*leaf.Parent)[leaf.Index+1]
		for _, l := range e.Doc.Leaves() {
			if l.Block.ID == next.ID || l.Owner == next.ID {
				e.SetSelection(l.Start, l.Start)
				e.reveal = true
				return true
			}
		}
	}
	e.Engine.Edit(func() {
		next := document.NewBlock("paragraph", "")
		next.Indent = leaf.Block.Indent
		*leaf.Parent = append((*leaf.Parent)[:leaf.Index+1], append([]*document.Block{next}, (*leaf.Parent)[leaf.Index+1:]...)...)
		e.Engine.FocusBlock(next.ID)
	})
	e.sync()
	e.reveal = true
	return true
}

// glyphs shapes a piece of a formula's text, kept for drawing.
func (e *Editor) glyphs(text string, size float32, italic, bold bool) *ui.TextLayout {
	// A registration failure still leaves the system serif fallbacks usable.
	_ = loadMathFonts()
	key := fmt.Sprintf("%s|%g|%v|%v", text, size, italic, bold)
	if l, ok := e.mathGlyphs[key]; ok {
		return l
	}
	if e.mathGlyphs == nil || len(e.mathGlyphs) > 4096 {
		e.mathGlyphs = map[string]*ui.TextLayout{}
	}
	family := mathFont
	if italic {
		family = "KaTeX_Math, " + family
	}
	weight := 400
	if bold {
		weight = 700
	}
	l := ui.ShapeRichText([]ui.Span{{Text: text, Size: size, Italic: italic, Weight: weight}}, ui.Font{Family: family, Size: size, Italic: italic, Weight: weight}, 0)
	e.mathGlyphs[key] = l
	return l
}

func (e *Editor) measureMath(text string, size float32, italic, bold bool) (w, ascent, descent float32) {
	l := e.glyphs(text, size, italic, bold)
	w, h := l.Size()
	if lines := l.Lines(); len(lines) > 0 {
		return w, lines[0].Ascent, lines[0].Descent
	}
	return w, h * .8, h * .2
}

// formula lays out the formula of a math block, kept while its TeX is
// unchanged.
func (e *Editor) formula(id, src string) mathShape {
	if s, ok := e.maths[id]; ok && s.src == src {
		return s
	}
	if e.maths == nil {
		e.maths = map[string]mathShape{}
	}
	box, err := tex.Layout(src, mathSize, e.measureMath)
	s := mathShape{src: src, box: box, err: err}
	e.maths[id] = s
	return s
}

// mathRow reserves room for the unified card (live formula preview on top and
// monospace TeX bar below) while editing, or just the clean formula at rest.
func (e *Editor) mathRow(r *row, textH float32) float32 {
	b := r.leaf.Block
	src := strings.TrimSpace(b.Text())
	r.mathScale = 1
	r.mathHidden = e.mathEditing != b.ID
	r.mathTop = mathMargin
	r.y = r.rect.Y + mathMargin

	previewH := float32(26)
	if src != "" {
		shape := e.formula(b.ID, src)
		if shape.err != nil {
			r.mathErr = i18n.T("公式有误：") + strings.TrimPrefix(shape.err.Error(), "tex: ")
			r.mathError = ui.ShapeText(r.mathErr, ui.Font{Family: "system-ui", Size: 12}, max(24, r.rect.W-2*mathPaddingX))
			_, errorH := r.mathError.Size()
			previewH = max(20, errorH)
			if r.mathHidden {
				r.mathHidden = false // Malformed TeX always remains editable and visible.
			}
		} else {
			box := shape.box
			r.math = &box
			width := r.rect.W
			if !r.mathHidden {
				width = max(24, min(width, mathPreviewW)-2*mathPaddingX)
			}
			if box.W > width {
				r.mathScale = max(1, width) / box.W
			}
			previewH = max(26, (box.Ascent+box.Descent)*r.mathScale)
		}
	}

	if r.mathHidden {
		r.mathPanel = 0
		return 2*mathMargin + previewH
	}

	r.mathTop = mathMargin + mathPreviewY
	wellTop := r.mathTop + previewH + 14
	r.y = r.rect.Y + wellTop + mathFenceH + mathPaddingY
	wellH := mathFenceH + mathPaddingY + textH + mathPaddingY
	r.mathPanel = (wellTop - mathMargin) + wellH + 10
	return mathMargin + r.mathPanel + mathMargin
}

// paintMath draws a math block's formula and unified editing card, reporting
// whether it stands for the block's TeX (which then is not drawn).
func (e *Editor) paintMath(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if r.leaf.Block.Kind != "math" {
		return false
	}
	dx, dy := e.visualOffset(r)
	x, y := bounds.X+r.rect.X+dx, bounds.Y+r.rect.Y+dy

	if !r.mathHidden && r.mathPanel > 0 {
		card := ui.Rect{X: x, Y: y + mathMargin, W: r.rect.W, H: r.mathPanel}
		p.Shadow(card, 12, 0, 4, 16, 0, e.page.MathShadow)
		p.Fill(card, e.page.Popover, 12)
		border := e.page.MathBorder
		if r.mathError != nil {
			border = t.Danger.Alpha(0.5)
		}
		p.Stroke(card, border, 12, 1)

		wellY := bounds.Y + r.y + dy - mathFenceH - mathPaddingY
		wellH := (card.Y + card.H - 10) - wellY
		if r.rect.W > 24 && wellH > 12 {
			well := ui.Rect{X: x + 10, Y: wellY, W: r.rect.W - 20, H: wellH}
			p.Fill(well, e.page.CodeFill, 8)
			fence := ui.ShapeText("$$", ui.Font{Family: "monospace", Size: 11.5, Weight: 600}, 0)
			_, fh := fence.Size()
			p.TextLayout(fence, well.X+10, well.Y+(mathFenceH-fh)/2+2, e.page.Caption)
			if !e.printing && well.W >= 150 {
				hint := ui.ShapeText("↵↵ · ⇥", ui.Font{Family: "system-ui", Size: 11, Weight: 500}, 0)
				hw, hh := hint.Size()
				pill := ui.Rect{X: well.X + well.W - hw - 18, Y: well.Y + 4, W: hw + 10, H: max(15, hh+2)}
				p.Fill(pill, t.Accent.Alpha(0.10), 4)
				p.TextLayout(hint, pill.X+5, pill.Y+(pill.H-hh)/2, t.Accent.Alpha(0.85))
			}
		}
	}

	if r.mathError != nil {
		ew, _ := r.mathError.Size()
		p.TextLayout(r.mathError, x+(r.rect.W-ew)/2, y+r.mathTop, t.Danger)
		return false
	}
	if r.math == nil {
		if !e.printing {
			hint := ui.ShapeText(i18n.T("输入 LaTeX 公式"), ui.Font{Family: "system-ui", Size: 14}, 0)
			hw, hh := hint.Size()
			p.TextLayout(hint, x+(r.rect.W-hw)/2, y+r.mathTop+(26-hh)/2, e.page.Placeholder)
		}
		return r.mathHidden
	}
	box, scale := r.math, r.mathScale
	w := box.W * scale
	fx := x + (r.rect.W-w)/2
	fy := y + r.mathTop
	p.Transform(ui.Scaling(scale, scale), fx, fy, func() {
		e.paintFormula(p, *box, fx, fy+box.Ascent, t)
	})
	return r.mathHidden
}

func (e *Editor) paintFormula(p *ui.Painter, box tex.Box, x, y float32, t *ui.Theme) {
	for _, op := range box.Ops {
		switch op.Kind {
		case "text":
			l := e.glyphs(op.Text, op.Size, op.Italic, op.Bold)
			baseline := float32(0)
			if lines := l.Lines(); len(lines) > 0 {
				baseline = lines[0].Baseline
			}
			color := t.Text
			if op.Error {
				color = t.Danger
			}
			p.TextLayout(l, x+op.X, y+op.Y-baseline, color)
		case "rule":
			p.Fill(ui.Rect{X: x + op.X, Y: y + op.Y, W: op.W, H: op.H}, t.Text, 0)
		case "path":
			pts := make([][2]float32, len(op.Points))
			for i, pt := range op.Points {
				pts[i] = [2]float32{x + pt[0], y + pt[1]}
			}
			p.StrokePolyline(pts, op.Width, t.Text)
		}
	}
}
