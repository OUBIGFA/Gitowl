package editor

import (
	"math"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

func isMermaidBlock(b *document.Block) bool {
	return b != nil && b.Kind == "code" && strings.EqualFold(strings.TrimSpace(b.Language), "mermaid")
}

// isCodeNoWrap reports whether code block b disables soft line wrapping.
// Printing and Mermaid diagrams always wrap normally.
func (e *Editor) isCodeNoWrap(b *document.Block) bool {
	if b == nil || b.Kind != "code" || e.printing || isMermaidBlock(b) {
		return false
	}
	if ov, ok := e.codeWrapOverride[b.ID]; ok {
		return ov
	}
	return e.codeNoWrap
}

// ToggleCodeWrap toggles word wrapping for the code block with id in the
// current editing session without modifying the underlying Markdown file.
func (e *Editor) ToggleCodeWrap(id string) {
	b := e.blockByID(id)
	if b == nil || b.Kind != "code" || isMermaidBlock(b) {
		return
	}
	if e.codeWrapOverride == nil {
		e.codeWrapOverride = make(map[string]bool)
	}
	next := !e.isCodeNoWrap(b)
	e.codeWrapOverride[id] = next
	if !next && e.codeScrollX != nil {
		delete(e.codeScrollX, id)
	}
	e.ready = false
}

func (e *Editor) hintWidthFor(b *document.Block, shapeWidth float32) float32 {
	if e.isCodeNoWrap(b) {
		return -shapeWidth
	}
	return shapeWidth
}

func wrapButton(d decoration) ui.Rect {
	return ui.Rect{X: d.rect.X + d.rect.W - 166, Y: d.rect.Y + 7, W: 86, H: 22}
}

func canShowWrapButton(d decoration) bool {
	lb := languageButton(d)
	wb := wrapButton(d)
	return wb.X > lb.X+lb.W+4
}

func (e *Editor) codeScrollFor(r row) (barBox ui.Rect, clipBox ui.Rect, availW, contentW, scrollX float32, noWrap bool) {
	b := r.leaf.Block
	if !e.isCodeNoWrap(b) || r.layout == nil {
		return ui.Rect{}, ui.Rect{}, 0, 0, 0, false
	}
	availW = max(24, r.rect.W-80)
	contentW, _ = r.layout.Size()
	maxScroll := max(0, contentW-availW)
	if e.codeScrollX != nil {
		scrollX = min(max(0, e.codeScrollX[b.ID]), maxScroll)
	}
	barBox = ui.Rect{X: r.x, Y: r.rect.Y + 40, W: availW, H: max(20, r.rect.H-8-40)}
	clipBox = ui.Rect{X: r.x - 2, Y: r.rect.Y, W: availW + 4, H: r.rect.H}
	return barBox, clipBox, availW, contentW, scrollX, true
}

func (e *Editor) setCodeScrollX(id string, x, maxScroll float32) {
	clamped := min(max(0, x), max(0, maxScroll))
	if e.codeScrollX == nil {
		e.codeScrollX = make(map[string]float32)
	}
	e.codeScrollX[id] = clamped
}

func (e *Editor) revealCodeCaret() {
	leaf := e.Engine.LeafAt(e.Caret)
	if !e.isCodeNoWrap(leaf.Block) {
		return
	}
	idx, ok := e.rowIDs[leaf.Block.ID]
	if !ok {
		return
	}
	r := e.ensureRow(idx)
	_, _, availW, contentW, scrollX, noWrap := e.codeScrollFor(*r)
	if !noWrap || contentW <= availW+1 {
		return
	}
	local := max(0, min(e.Caret-r.leaf.Start, r.leaf.End-r.leaf.Start))
	caret := r.layout.Caret(local)
	next := scrollX
	if caret.X < scrollX+12 {
		next = max(0, caret.X-12)
	} else if caret.X+4 > scrollX+availW-12 {
		next = min(contentW-availW, caret.X-availW+24)
	}
	if next != scrollX {
		e.setCodeScrollX(leaf.Block.ID, next, contentW-availW)
	}
}

func (e *Editor) handleCodeScrollInput(ev ui.InputEvent) bool {
	if e.codeBarDrag != "" {
		idx, ok := e.rowIDs[e.codeBarDrag]
		if !ok {
			e.codeBarDrag = ""
			return false
		}
		r := *e.indexedRow(idx)
		barBox, _, availW, contentW, scrollX, noWrap := e.codeScrollFor(r)
		if !noWrap || contentW <= availW+1 {
			e.codeBarDrag = ""
			return false
		}
		bar := ui.Scrollbar{Box: barBox, Horizontal: true, View: availW, Content: contentW, Offset: scrollX}
		switch ev.Kind {
		case ui.InputPointerMove:
			e.setCodeScrollX(e.codeBarDrag, bar.OffsetAt(ev.X-barBox.X-e.codeBarGrab), contentW-availW)
			return true
		case ui.InputPointerUp:
			e.codeBarDrag = ""
			return true
		}
	}

	if ev.Kind == ui.InputPointerDown && ev.Button == 0 && len(e.codeWrapOverride)+boolToInt(e.codeNoWrap) > 0 {
		for di := range e.visibleDecorations(20) {
			d := *e.indexedDecoration(di)
			if d.kind != "code" || d.id == "" {
				continue
			}
			idx, ok := e.rowIDs[d.id]
			if !ok {
				continue
			}
			r := *e.indexedRow(idx)
			barBox, _, availW, contentW, scrollX, noWrap := e.codeScrollFor(r)
			if !noWrap || contentW <= availW+1 {
				continue
			}
			track := ui.Rect{X: barBox.X, Y: barBox.Y + barBox.H - 14, W: barBox.W, H: 14}
			if ev.X >= track.X && ev.X <= track.X+track.W && ev.Y >= track.Y && ev.Y <= track.Y+track.H {
				bar := ui.Scrollbar{Box: barBox, Horizontal: true, View: availW, Content: contentW, Offset: scrollX}
				start, length := bar.Thumb()
				e.codeBarDrag = d.id
				if ev.X >= barBox.X+start && ev.X <= barBox.X+start+length {
					e.codeBarGrab = ev.X - (barBox.X + start)
				} else {
					e.codeBarGrab = length / 2
					e.setCodeScrollX(d.id, bar.OffsetAt(ev.X-barBox.X-e.codeBarGrab), contentW-availW)
				}
				return true
			}
		}
	}

	if ev.Kind != ui.InputScroll {
		return false
	}
	delta := ev.DX
	if delta == 0 && ev.Mods&ui.Shift != 0 {
		delta = ev.DY
	}
	if math.Abs(float64(delta)) < 0.01 {
		return false
	}
	for di := range e.visibleDecorations(20) {
		d := *e.indexedDecoration(di)
		if d.kind != "code" || d.id == "" {
			continue
		}
		if ev.X < d.rect.X || ev.X > d.rect.X+d.rect.W || ev.Y < d.rect.Y || ev.Y > d.rect.Y+d.rect.H {
			continue
		}
		idx, ok := e.rowIDs[d.id]
		if !ok {
			continue
		}
		r := *e.indexedRow(idx)
		_, _, availW, contentW, scrollX, noWrap := e.codeScrollFor(r)
		if !noWrap || contentW <= availW+1 {
			return false
		}
		next := min(max(0, scrollX+delta), contentW-availW)
		if next != scrollX {
			e.setCodeScrollX(d.id, next, contentW-availW)
			return true
		}
		return ev.DX != 0 && abs32(ev.DX) > abs32(ev.DY)
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (e *Editor) paintCodeWrapButton(p *ui.Painter, bounds ui.Rect, d decoration, t *ui.Theme) {
	if !canShowWrapButton(d) || isMermaidBlock(e.blockByID(d.id)) {
		return
	}
	b := e.blockByID(d.id)
	noWrap := e.isCodeNoWrap(b)
	if !noWrap && !e.hovered(d.rect, 0) {
		return
	}
	r := wrapButton(d)
	r.X += bounds.X
	r.Y += bounds.Y + e.ownerVisualDY(d.ownerID)
	if e.hovered(wrapButton(d), 0) {
		p.Fill(r, e.page.Hover, 4)
	} else if noWrap {
		p.Fill(r, t.Accent.Alpha(0.08), 4)
	}
	fg := e.page.Caption
	label := i18n.T("自动换行")
	if noWrap {
		fg = t.Accent
		label = i18n.T("不换行")
	}
	p.Icon(iconWrapText, ui.Rect{X: r.X + 4, Y: r.Y + 3, W: 16, H: 16}, fg)
	span := ui.Span{Text: label, Size: 13, Color: fg}
	_, h := p.MeasureText(0, span)
	p.RichText(r.X+24, r.Y+(r.H-h)/2, 0, span)
}
