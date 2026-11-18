package editor

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/mermaid"
	"github.com/egoist/mygo/ui"
)

// Native Mermaid diagram blocks render as clean centered vector diagrams at
// rest, and expand into a unified editing card (live preview on top, language
// bar + monospace Mermaid source well below) while focused, empty, errored, or
// matching an active find query.
const (
	mermaidMargin   = float32(18)
	mermaidPadX     = float32(14)
	mermaidPadY     = float32(10)
	mermaidHeaderH  = float32(34)
	mermaidPreviewY = float32(14)
	mermaidMaxH     = float32(520)

	defaultMermaidFlowchart = "flowchart LR\n    A[开始] --> B{条件判断}\n    B -- 通过 --> C[执行]\n    B -. 拒绝 .-> D[结束]"
	defaultMermaidSequence  = "sequenceDiagram\n    participant U as 客户端\n    participant S as 服务端\n    U->>+S: 请求数据\n    S-->>-U: 返回结果"
)

type mermaidShape struct {
	src  string
	diag mermaid.Diagram
	err  error
}

// watchMermaid tracks the Mermaid block currently under the caret so it opens
// its live-preview editor card while focused.
func (e *Editor) watchMermaid() {
	e.mermaidEditing = ""
	if !e.focused || e.printing || e.ReadOnly {
		return
	}
	if leaf := e.Engine.LeafAt(e.Caret); isMermaidBlock(leaf.Block) && e.Anchor >= leaf.Start && e.Anchor <= leaf.End {
		e.mermaidEditing = leaf.Block.ID
	}
}

// exitMermaid exits an open Mermaid block on Tab into the block below, or adds
// a fresh paragraph at the end of the parent list.
func (e *Editor) exitMermaid() bool {
	if e.ReadOnly || e.Anchor != e.Caret {
		return false
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if !isMermaidBlock(leaf.Block) || leaf.Parent == nil {
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

// InsertMermaidFlowchart inserts a starter Mermaid flowchart at the caret.
func (e *Editor) InsertMermaidFlowchart() { e.insertMermaidBlock(defaultMermaidFlowchart) }

// InsertMermaidSequence inserts a starter Mermaid sequence diagram at the caret.
func (e *Editor) InsertMermaidSequence() { e.insertMermaidBlock(defaultMermaidSequence) }

func (e *Editor) insertMermaidBlock(src string) {
	if e.ReadOnly {
		return
	}
	b := document.NewBlock("code", src)
	b.Language = "mermaid"
	leaf := e.Engine.LeafAt(e.Caret)
	if leaf.Block.Kind == "paragraph" && leaf.Block.Text() == "" {
		e.Engine.Edit(func() {
			leaf.Block.Kind = "code"
			leaf.Block.Language = "mermaid"
			leaf.Block.Runs = []document.Run{{Text: src}}
			e.Engine.FocusBlock(leaf.Block.ID)
		})
	} else {
		e.Engine.Insert(b)
	}
	e.sync()
	e.RequestFocus = true
	e.reveal = true
}

func (e *Editor) mermaidTextLayout(text string, size float32, bold bool, align int, maxW float32) *ui.TextLayout {
	weight := 400
	if bold {
		weight = 600
	}
	key := fmt.Sprintf("%s|%g|%d|%d|%g", text, size, weight, align, maxW)
	if l, ok := e.mermaidLayouts[key]; ok {
		return l
	}
	if e.mermaidLayouts == nil || len(e.mermaidLayouts) > 4096 {
		e.mermaidLayouts = map[string]*ui.TextLayout{}
	}
	var shapeAlign ui.Align
	switch {
	case align < 0:
		shapeAlign = ui.Start
	case align > 0:
		shapeAlign = ui.End
	default:
		shapeAlign = ui.Center
	}
	l := ui.ShapeRichTextWith(
		[]ui.Span{{Text: text, Size: size, Weight: weight}},
		ui.Font{Family: "system-ui", Size: size, Weight: weight},
		ui.TextShape{Width: maxW, Align: shapeAlign},
	)
	e.mermaidLayouts[key] = l
	return l
}

func (e *Editor) measureMermaid(text string, size float32, bold bool) (w, h float32) {
	if text == "" {
		return 0, size * 1.3
	}
	l := e.mermaidTextLayout(text, size, bold, 0, 0)
	return l.Size()
}

func (e *Editor) diagram(id, src string) mermaidShape {
	if s, ok := e.mermaids[id]; ok && s.src == src {
		return s
	}
	if e.mermaids == nil {
		e.mermaids = map[string]mermaidShape{}
	}
	diag, err := mermaid.ParseAndLayout(src, e.measureMermaid)
	s := mermaidShape{src: src, diag: diag, err: err}
	e.mermaids[id] = s
	return s
}

func (e *Editor) hasActiveMatchIn(leaf document.Leaf) bool {
	if !e.find.open || e.find.query == "" || len(e.find.matches) == 0 {
		return false
	}
	for _, m := range e.find.matches {
		if m.End >= leaf.Start && m.Start <= leaf.End {
			return true
		}
	}
	return false
}

// mermaidRow computes card height, diagram scale, and source baseline r.y for a
// Mermaid code block. At rest (hidden=true) only the centered diagram is shown.
func (e *Editor) mermaidRow(r *row, textH, gap float32) (total float32, wellOff float32) {
	b := r.leaf.Block
	src := strings.TrimSpace(b.Text())
	r.mermaidScale = 1
	r.mermaidHidden = e.mermaidEditing != b.ID && src != "" && !e.hasActiveMatchIn(r.leaf)
	if e.printing {
		r.mermaidHidden = src != ""
	}
	r.mermaidTop = mermaidMargin
	r.mermaidErr = ""
	r.mermaidError = nil
	r.mermaid = nil

	previewH := float32(36)
	if src != "" {
		shape := e.diagram(b.ID, src)
		if shape.err != nil {
			r.mermaidErr = i18n.T("图表语法有误：") + shape.err.Error()
			r.mermaidError = ui.ShapeText(r.mermaidErr, ui.Font{Family: "system-ui", Size: 12}, max(24, r.rect.W-2*mermaidPadX))
			_, errorH := r.mermaidError.Size()
			previewH = max(22, errorH)
			if !e.printing {
				r.mermaidHidden = false
			}
		} else {
			diag := shape.diag
			r.mermaid = &diag
			availW := max(24, r.rect.W)
			if !r.mermaidHidden {
				availW = max(24, r.rect.W-2*mermaidPadX)
			}
			scale := float32(1)
			if diag.W > availW {
				scale = availW / diag.W
			}
			if diag.H*scale > mermaidMaxH {
				scale = min(scale, mermaidMaxH/diag.H)
			}
			r.mermaidScale = max(0.1, scale)
			previewH = max(36, diag.H*r.mermaidScale)
		}
	}

	if r.mermaidHidden {
		r.mermaidPanel = 0
		r.y = r.rect.Y + mermaidMargin
		return 2*mermaidMargin + previewH + gap, 0
	}

	r.mermaidTop = mermaidMargin + mermaidPreviewY
	wellTop := r.mermaidTop + previewH + 14
	r.y = r.rect.Y + wellTop + mermaidHeaderH + mermaidPadY
	wellH := mermaidHeaderH + mermaidPadY + textH + mermaidPadY + 6
	r.mermaidPanel = (wellTop - mermaidMargin) + wellH + 10
	return mermaidMargin + r.mermaidPanel + mermaidMargin + gap, wellTop
}

func mermaidLanguageButton(d decoration) ui.Rect {
	if d.mermaidHidden {
		return ui.Rect{}
	}
	return ui.Rect{X: d.rect.X + 18, Y: d.rect.Y + d.mermaidWellOff + 6, W: 104, H: 22}
}

func mermaidCopyButton(d decoration) ui.Rect {
	if d.mermaidHidden {
		return ui.Rect{X: d.rect.X + d.rect.W - 70, Y: d.rect.Y + 6, W: 64, H: 22}
	}
	return ui.Rect{X: d.rect.X + d.rect.W - 82, Y: d.rect.Y + d.mermaidWellOff + 6, W: 64, H: 22}
}

func mermaidEditButton(d decoration) ui.Rect {
	if !d.mermaidHidden {
		return ui.Rect{}
	}
	return ui.Rect{X: d.rect.X + d.rect.W - 138, Y: d.rect.Y + 6, W: 62, H: 22}
}

func (e *Editor) focusBlockStart(id string) bool {
	if e.ReadOnly {
		return false
	}
	if leaf, ok := e.Engine.LeafByID(id); ok {
		e.SetSelection(leaf.Start, leaf.Start)
		e.RequestFocus = true
		e.reveal = true
		return true
	}
	return false
}

// paintMermaidDecoration draws the unified card background, CodeFill source
// well, language bar, and gutter line numbers before selection and text run.
func (e *Editor) paintMermaidDecoration(p *ui.Painter, bounds ui.Rect, d decoration, ddx, ddy float32, t *ui.Theme) {
	if d.mermaidHidden {
		return
	}
	x := bounds.X + d.rect.X + ddx
	y := bounds.Y + d.rect.Y + ddy
	cardH := max(60, d.rect.H-2*mermaidMargin)
	card := ui.Rect{X: x, Y: y + mermaidMargin, W: d.rect.W, H: cardH}
	p.Shadow(card, 12, 0, 4, 16, 0, e.page.MathShadow)
	p.Fill(card, e.page.Popover, 12)
	border := e.page.MathBorder
	if idx, ok := e.rowIDs[d.id]; ok && e.indexedRow(idx).mermaidError != nil {
		border = t.Danger.Alpha(0.5)
	}
	p.Stroke(card, border, 12, 1)

	wellY := y + d.mermaidWellOff
	wellH := (card.Y + card.H - 10) - wellY
	if d.rect.W > 24 && wellH > 12 {
		well := ui.Rect{X: x + 10, Y: wellY, W: d.rect.W - 20, H: wellH}
		p.Fill(well, e.page.CodeFill, 8)
		p.Stroke(well, e.page.CodeBorder, 8, 1)

		lb := mermaidLanguageButton(d)
		lb.X += bounds.X + ddx
		lb.Y += bounds.Y + ddy
		e.paintLanguage(p, lb, d, t)

		if !e.printing && well.W >= 240 {
			hint := ui.ShapeText("↵↵ · ⇥", ui.Font{Family: "system-ui", Size: 11, Weight: 500}, 0)
			hw, hh := hint.Size()
			pill := ui.Rect{X: well.X + well.W - hw - 92, Y: well.Y + 6, W: hw + 10, H: max(18, hh+4)}
			p.Fill(pill, t.Accent.Alpha(0.10), 4)
			p.TextLayout(hint, pill.X+5, pill.Y+(pill.H-hh)/2, t.Accent.Alpha(0.85))
		}
		e.paintLineNumbers(p, ui.Rect{X: bounds.X + ddx, Y: bounds.Y + ddy, W: bounds.W, H: bounds.H}, d)
	}
}

func (e *Editor) paintMermaidHover(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	if e.printing {
		return
	}
	ddy := e.ownerVisualDY(d.ownerID)
	offset := func(r ui.Rect) ui.Rect {
		r.X += bounds.X
		r.Y += bounds.Y + ddy
		return r
	}
	copied := d.id == e.copiedID && p.Now().Sub(e.copiedAt) < 1500*time.Millisecond
	if copied {
		p.After(1500*time.Millisecond - p.Now().Sub(e.copiedAt))
	}
	hoveredCard := e.hovered(d.rect, 0)
	if !copied && !hoveredCard {
		return
	}

	if d.mermaidHidden && hoveredCard && !e.ReadOnly {
		erDoc := mermaidEditButton(d)
		if erDoc.W > 0 {
			er := offset(erDoc)
			if e.hovered(erDoc, 0) {
				p.Fill(er, e.page.Hover, 4)
			}
			fg := e.page.Caption
			p.Icon(iconPencil, ui.Rect{X: er.X + 4, Y: er.Y + 4, W: 14, H: 14}, fg)
			span := ui.Span{Text: i18n.T("编辑"), Size: 13, Color: fg}
			_, th := p.MeasureText(0, span)
			p.RichText(er.X+22, er.Y+(er.H-th)/2, 0, span)
		}
	}

	crDoc := mermaidCopyButton(d)
	if d.rect.W >= 180 && (copied || hoveredCard) {
		cr := offset(crDoc)
		if e.hovered(crDoc, 0) {
			p.Fill(cr, e.page.Hover, 4)
		}
		icon, label := iconCopy, i18n.T("复制")
		fg := e.page.Caption
		scale := float32(1)
		if copied {
			icon, label = iconCheck, i18n.T("已复制")
			fg = t.Accent
			if elapsed := p.Now().Sub(e.copiedAt); elapsed >= 0 && elapsed < 160*time.Millisecond && !e.reduceMotion {
				u := float64(elapsed) / float64(160*time.Millisecond)
				scale = 1 + 0.08*float32(math.Sin(math.Pi*u))
				p.AnimationFrame()
			}
		}
		drawBtn := func() {
			p.Icon(icon, ui.Rect{X: cr.X + 4, Y: cr.Y + 4, W: 14, H: 14}, fg)
			span := ui.Span{Text: label, Size: 13, Color: fg}
			_, th := p.MeasureText(0, span)
			p.RichText(cr.X+22, cr.Y+(cr.H-th)/2, 0, span)
		}
		if scale != 1 {
			p.Transform(ui.Scaling(scale, scale), cr.X+cr.W/2, cr.Y+cr.H/2, drawBtn)
		} else {
			drawBtn()
		}
	}
}

// paintMermaid draws the live or collapsed Mermaid diagram preview and returns
// whether it hides the raw Mermaid source text beneath it.
func (e *Editor) paintMermaid(p *ui.Painter, bounds ui.Rect, r row, t *ui.Theme) bool {
	if !isMermaidBlock(r.leaf.Block) {
		return false
	}
	dx, dy := e.visualOffset(r)
	x, y := bounds.X+r.rect.X+dx, bounds.Y+r.rect.Y+dy

	if r.mermaidError != nil {
		ew, _ := r.mermaidError.Size()
		p.TextLayout(r.mermaidError, x+(r.rect.W-ew)/2, y+r.mermaidTop, t.Danger)
		return false
	}
	if r.mermaid == nil {
		if !e.printing {
			hint := ui.ShapeText(i18n.T("输入 Mermaid 流程图或时序图"), ui.Font{Family: "system-ui", Size: 14}, 0)
			hw, hh := hint.Size()
			p.TextLayout(hint, x+(r.rect.W-hw)/2, y+r.mermaidTop+(36-hh)/2, e.page.Placeholder)
		}
		return r.mermaidHidden
	}

	diag, scale := *r.mermaid, r.mermaidScale
	w := diag.W * scale
	fx := x + (r.rect.W-w)/2
	fy := y + r.mermaidTop
	if scale != 1 {
		p.Transform(ui.Scaling(scale, scale), fx, fy, func() {
			e.paintMermaidDiagram(p, diag, fx, fy, t)
		})
	} else {
		e.paintMermaidDiagram(p, diag, fx, fy, t)
	}
	return r.mermaidHidden
}

func (e *Editor) paintMermaidDiagram(p *ui.Painter, diag mermaid.Diagram, ox, oy float32, t *ui.Theme) {
	pg := e.page
	blue := pg.Callouts["blue"]
	yellow := pg.Callouts["yellow"]

	// 1. Background containers (subgraphs & fragments).
	for _, s := range diag.Shapes {
		if s.Role == mermaid.RoleSubgraph || s.Role == mermaid.RoleFragment {
			e.paintMermaidShape(p, s, ox, oy, t, pg, blue, yellow)
		}
	}

	// 2. Edges & sequence paths.
	for _, pathOp := range diag.Paths {
		e.paintMermaidPath(p, pathOp, ox, oy, t, pg)
	}

	// 3. Foreground shapes (nodes, actors, activations, notes).
	for _, s := range diag.Shapes {
		if s.Role != mermaid.RoleSubgraph && s.Role != mermaid.RoleFragment {
			e.paintMermaidShape(p, s, ox, oy, t, pg, blue, yellow)
		}
	}

	// 4. Labels (with subtle paper pill behind edge labels so text reads clearly).
	for _, lb := range diag.Labels {
		lx := ox + lb.X
		ly := oy + lb.Y
		fg := t.Text
		if lb.Role == mermaid.RoleSubgraph || lb.Role == mermaid.RoleFragment {
			fg = pg.Caption
		}
		if lb.Pill {
			pill := ui.Rect{X: lx - 4, Y: ly - 1, W: lb.W + 8, H: lb.H + 2}
			p.Fill(pill, t.Background.Alpha(0.92), 4)
		}
		tl := e.mermaidTextLayout(lb.Text, lb.Size, lb.Bold, lb.Align, max(8, lb.W))
		tw, th := tl.Size()
		drawX := lx
		if lb.Align == 0 {
			drawX = lx + (lb.W-tw)/2
		} else if lb.Align > 0 {
			drawX = lx + lb.W - tw
		}
		drawY := ly + (lb.H-th)/2
		p.TextLayout(tl, drawX, drawY, fg)
	}
}

func (e *Editor) paintMermaidShape(p *ui.Painter, s mermaid.ShapeOp, ox, oy float32, t *ui.Theme, pg Page, blue, yellow Callout) {
	x, y, w, h := ox+s.X, oy+s.Y, s.W, s.H
	r := ui.Rect{X: x, Y: y, W: w, H: h}

	var fill, border ui.Color
	strokeW := float32(1.25)
	switch s.Role {
	case mermaid.RoleSubgraph, mermaid.RoleFragment:
		if s.Kind == mermaid.ShapePentagon {
			fill = pg.CodeHover.Alpha(0.28)
			border = pg.CodeBorder
		} else {
			fill = pg.CodeFill.Alpha(0.50)
			border = pg.CodeBorder
		}
		strokeW = 1
	case mermaid.RoleActor:
		fill = blue.Fill
		border = blue.Border
	case mermaid.RoleNote:
		fill = yellow.Fill
		border = yellow.Border
	case mermaid.RoleActivation:
		fill = t.Accent.Alpha(0.18)
		border = t.Accent.Alpha(0.68)
		strokeW = 1
	default:
		fill = pg.CodeFill
		border = t.Accent.Alpha(0.45)
	}

	strokeOutline := func(path *ui.Path) {
		if s.Dashed {
			path.Dash(0, 5, 4)
		}
		p.StrokePath(path, strokeW, border)
	}

	switch s.Kind {
	case mermaid.ShapeRect:
		p.Fill(r, fill, s.Radius)
		if s.Dashed {
			p.StrokeDashed(r, border, s.Radius, strokeW)
		} else {
			p.Stroke(r, border, s.Radius, strokeW)
		}

	case mermaid.ShapeRound:
		rad := max(s.Radius, 6)
		p.Fill(r, fill, rad)
		if s.Dashed {
			p.StrokeDashed(r, border, rad, strokeW)
		} else {
			p.Stroke(r, border, rad, strokeW)
		}

	case mermaid.ShapeStadium:
		rad := min(w, h) / 2
		p.Fill(r, fill, rad)
		p.Stroke(r, border, rad, strokeW)

	case mermaid.ShapeCircle:
		rad := min(w, h) / 2
		var path ui.Path
		path.Circle(x+w/2, y+h/2, rad)
		p.FillPath(&path, fill)
		p.StrokePath(&path, strokeW, border)

	case mermaid.ShapeSubroutine:
		p.Fill(r, fill, 4)
		p.Stroke(r, border, 4, strokeW)
		inset := min(8, w*0.14)
		p.Line(x+inset, y, x+inset, y+h, strokeW, border)
		p.Line(x+w-inset, y, x+w-inset, y+h, strokeW, border)

	case mermaid.ShapeCylinder:
		ry := min(7, h*0.18)
		kx := w * 0.28
		var body ui.Path
		body.MoveTo(x, y+ry)
		body.LineTo(x, y+h-ry)
		body.CubeTo(x, y+h-ry+ry*0.9, x+kx, y+h, x+w/2, y+h)
		body.CubeTo(x+w-kx, y+h, x+w, y+h-ry+ry*0.9, x+w, y+h-ry)
		body.LineTo(x+w, y+ry)
		body.CubeTo(x+w, y+ry-ry*0.9, x+w-kx, y, x+w/2, y)
		body.CubeTo(x+kx, y, x, y+ry-ry*0.9, x, y+ry)
		body.Close()
		p.FillPath(&body, fill)
		p.StrokePath(&body, strokeW, border)

		var frontRim ui.Path
		frontRim.MoveTo(x, y+ry)
		frontRim.CubeTo(x, y+ry+ry*0.9, x+kx, y+2*ry, x+w/2, y+2*ry)
		frontRim.CubeTo(x+w-kx, y+2*ry, x+w, y+ry+ry*0.9, x+w, y+ry)
		p.StrokePath(&frontRim, strokeW, border)

	case mermaid.ShapeDiamond:
		cx, cy := x+w/2, y+h/2
		var path ui.Path
		path.MoveTo(cx, y)
		path.LineTo(x+w, cy)
		path.LineTo(cx, y+h)
		path.LineTo(x, cy)
		path.Close()
		p.FillPath(&path, fill)
		p.StrokePath(&path, strokeW, border)

	case mermaid.ShapeHexagon:
		inset := min(14, w*0.18)
		cy := y + h/2
		var path ui.Path
		path.MoveTo(x+inset, y)
		path.LineTo(x+w-inset, y)
		path.LineTo(x+w, cy)
		path.LineTo(x+w-inset, y+h)
		path.LineTo(x+inset, y+h)
		path.LineTo(x, cy)
		path.Close()
		p.FillPath(&path, fill)
		p.StrokePath(&path, strokeW, border)

	case mermaid.ShapeParallelogram:
		skew := min(14, w*0.18)
		var path ui.Path
		path.MoveTo(x+skew, y)
		path.LineTo(x+w, y)
		path.LineTo(x+w-skew, y+h)
		path.LineTo(x, y+h)
		path.Close()
		p.FillPath(&path, fill)
		p.StrokePath(&path, strokeW, border)

	case mermaid.ShapePentagon:
		cut := min(7, min(w, h)*0.35)
		var path ui.Path
		path.MoveTo(x, y)
		path.LineTo(x+w, y)
		path.LineTo(x+w, y+h-cut)
		path.LineTo(x+w-cut, y+h)
		path.LineTo(x, y+h)
		path.Close()
		p.FillPath(&path, fill)
		strokeOutline(&path)

	case mermaid.ShapeActor:
		cx := x + w/2
		headR := min(w, h) * 0.22
		headY := y + headR + 1
		var head ui.Path
		head.Circle(cx, headY, headR)
		p.FillPath(&head, fill)
		p.StrokePath(&head, strokeW, border)

		neckY := headY + headR
		hipY := y + h*0.66
		armY := y + h*0.44
		p.Line(cx, neckY, cx, hipY, strokeW, border)
		p.Line(cx-headR*1.5, armY, cx+headR*1.5, armY, strokeW, border)
		p.Line(cx, hipY, cx-headR*1.2, y+h-1, strokeW, border)
		p.Line(cx, hipY, cx+headR*1.2, y+h-1, strokeW, border)
	}
}

func (e *Editor) paintMermaidPath(p *ui.Painter, op mermaid.PathOp, ox, oy float32, t *ui.Theme, pg Page) {
	if len(op.Points) < 2 {
		return
	}
	pts := make([][2]float32, len(op.Points))
	for i, pt := range op.Points {
		pts[i] = [2]float32{ox + pt[0], oy + pt[1]}
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

	// Shorten shaft slightly when terminating at a filled arrowhead so the blunt
	// line cap does not poke past the tip.
	drawPts := append([][2]float32(nil), pts...)
	if op.StartMarker == mermaid.MarkerArrow {
		if ux, uy, d := segDirLen(pts[1], pts[0]); d >= 8 {
			drawPts[0] = [2]float32{pts[0][0] - ux*5, pts[0][1] - uy*5}
		}
	}
	if op.EndMarker == mermaid.MarkerArrow {
		n := len(drawPts)
		if ux, uy, d := segDirLen(drawPts[n-2], drawPts[n-1]); d >= 8 {
			drawPts[n-1] = [2]float32{pts[n-1][0] - ux*5, pts[n-1][1] - uy*5}
		}
	}

	var path ui.Path
	path.Cap(ui.RoundCap).Join(ui.RoundJoin)
	path.MoveTo(drawPts[0][0], drawPts[0][1])
	if op.Smooth && len(drawPts) >= 3 {
		for i := 1; i < len(drawPts)-1; i++ {
			prev, cur, next := drawPts[i-1], drawPts[i], drawPts[i+1]
			ux1, uy1, d1 := segDirLen(prev, cur)
			ux2, uy2, d2 := segDirLen(cur, next)
			if d1 < 2 || d2 < 2 {
				path.LineTo(cur[0], cur[1])
				continue
			}
			rad := min(float32(8), min(d1, d2)*0.35)
			inX, inY := cur[0]-ux1*rad, cur[1]-uy1*rad
			outX, outY := cur[0]+ux2*rad, cur[1]+uy2*rad
			path.LineTo(inX, inY)
			path.QuadTo(cur[0], cur[1], outX, outY)
		}
		last := drawPts[len(drawPts)-1]
		path.LineTo(last[0], last[1])
	} else {
		for _, pt := range drawPts[1:] {
			path.LineTo(pt[0], pt[1])
		}
	}
	if op.Style == mermaid.EdgeDashed {
		path.Dash(0, 5, 4)
	}
	p.StrokePath(&path, width, col)

	if op.StartMarker != mermaid.MarkerNone {
		if ux, uy, ok := segUnit(pts[1], pts[0]); ok {
			paintMermaidMarker(p, op.StartMarker, pts[0][0], pts[0][1], ux, uy, width, col, t.Background)
		}
	}
	if op.EndMarker != mermaid.MarkerNone {
		n := len(pts)
		if ux, uy, ok := segUnit(pts[n-2], pts[n-1]); ok {
			paintMermaidMarker(p, op.EndMarker, pts[n-1][0], pts[n-1][1], ux, uy, width, col, t.Background)
		}
	}
}

func paintMermaidMarker(p *ui.Painter, m mermaid.MarkerKind, tipX, tipY, ux, uy, strokeW float32, col, bg ui.Color) {
	px, py := -uy, ux
	switch m {
	case mermaid.MarkerArrow:
		size := float32(9)
		halfW := float32(4.2)
		var tri ui.Path
		tri.MoveTo(tipX, tipY)
		tri.LineTo(tipX-ux*size+px*halfW, tipY-uy*size+py*halfW)
		tri.LineTo(tipX-ux*size-px*halfW, tipY-uy*size-py*halfW)
		tri.Close()
		p.FillPath(&tri, col)

	case mermaid.MarkerOpenArrow:
		size := float32(8.5)
		halfW := float32(4.5)
		var v ui.Path
		v.Cap(ui.RoundCap).Join(ui.RoundJoin)
		v.MoveTo(tipX-ux*size+px*halfW, tipY-uy*size+py*halfW)
		v.LineTo(tipX, tipY)
		v.LineTo(tipX-ux*size-px*halfW, tipY-uy*size-py*halfW)
		p.StrokePath(&v, max(1.3, strokeW), col)

	case mermaid.MarkerCircle:
		r := float32(3.8)
		cx, cy := tipX-ux*r, tipY-uy*r
		var circ ui.Path
		circ.Circle(cx, cy, r)
		p.FillPath(&circ, bg)
		p.StrokePath(&circ, max(1.3, strokeW), col)

	case mermaid.MarkerCross:
		r := float32(3.8)
		cx, cy := tipX-ux*5, tipY-uy*5
		dx1, dy1 := (ux+px)*r*0.75, (uy+py)*r*0.75
		dx2, dy2 := (ux-px)*r*0.75, (uy-py)*r*0.75
		p.Line(cx-dx1, cy-dy1, cx+dx1, cy+dy1, max(1.4, strokeW), col)
		p.Line(cx-dx2, cy-dy2, cx+dx2, cy+dy2, max(1.4, strokeW), col)
	}
}

func segDirLen(a, b [2]float32) (ux, uy, dist float32) {
	dx := b[0] - a[0]
	dy := b[1] - a[1]
	d := float32(math.Hypot(float64(dx), float64(dy)))
	if d < 0.001 {
		return 0, 0, 0
	}
	return dx / d, dy / d, d
}

func segUnit(a, b [2]float32) (ux, uy float32, ok bool) {
	u, v, d := segDirLen(a, b)
	return u, v, d >= 0.001
}
