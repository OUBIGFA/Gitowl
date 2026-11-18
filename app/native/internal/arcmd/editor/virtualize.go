package editor

import (
	"math"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

const (
	// Virtualization activates only on large documents so ordinary notes and
	// resize tests continue to use the exact eager layout path.
	virtualizeLeafThreshold = 800
	virtualizeTextThreshold = 120_000
	maxCachedShapes         = 768
	cacheTrimHysteresis     = 128
	virtualOverscanMin      = 1200
)

// runsStamp is a pointer-free 128-bit fingerprint of a block's styled runs so
// thousands of offscreen height hints retain zero heap pointers for GC.
type runsStamp struct {
	lo uint64
	hi uint64
}

func makeRunsStamp(runs []document.Run) runsStamp {
	lo := uint64(14695981039346656037)
	hi := uint64(0x9e3779b97f4a7c15) ^ uint64(len(runs))
	const prime2 = uint64(6364136223846793005)
	lo = fnvMixUint64(lo, uint64(len(runs)))
	for i := range runs {
		r := &runs[i]
		lo = fnvMixStyle(lo, r.Style)
		lo = fnvMixString(lo, r.Text)
		hi = (hi^uint64(len(r.Text)))*prime2 + uint64(i+1)
		if r.Style != (document.Style{}) {
			hi ^= fnvMixStyle(hi, r.Style)
		}
		for j := 0; j < len(r.Text); j++ {
			hi = (hi ^ uint64(r.Text[j]) ^ uint64(j+1)<<8) * prime2
		}
	}
	return runsStamp{lo: lo, hi: hi}
}

func fontStamp(f ui.Font) uint64 {
	h := uint64(14695981039346656037)
	h = fnvMixString(h, f.Family)
	h = fnvMixString(h, f.Features)
	h = fnvMixUint32(h, math.Float32bits(f.Size))
	h = fnvMixUint32(h, math.Float32bits(f.LineHeight))
	h = fnvMixUint64(h, uint64(f.Weight))
	if f.Italic {
		h = fnvMixByte(h, 1)
	} else {
		h = fnvMixByte(h, 0)
	}
	return h
}

type heightHint struct {
	width    float32
	height   float32
	fontHash uint64
	stamp    runsStamp
	exact    bool
}

type blockMetric struct {
	font       ui.Font
	left       float32
	right      float32
	top        float32
	bottom     float32
	gap        float32
	shapeWidth float32
}

func (e *Editor) shouldVirtualize() bool {
	if e.printing {
		return false
	}
	return e.Engine.LeafCount() > virtualizeLeafThreshold || e.Engine.TextLength() > virtualizeTextThreshold
}

func (e *Editor) virtualWindow() (top, bottom float32) {
	vp := e.viewport
	if vp <= 0 {
		vp = 900
	}
	overscan := max(float32(virtualOverscanMin), vp)
	return e.Scroll.Y - overscan, e.Scroll.Y + vp + overscan
}

func (e *Editor) nextCacheClock() uint64 {
	e.cacheClock++
	return e.cacheClock
}

func canDeferKind(kind string) bool {
	switch kind {
	case "image", "rule", "columns", "cell":
		return false
	}
	return true
}

func tableStamp(b *document.Block) runsStamp {
	lo := uint64(14695981039346656037)
	hi := uint64(0x9e3779b97f4a7c15) ^ uint64(len(b.Children))
	lo = fnvMixUint64(lo, uint64(len(b.Widths)))
	for _, w := range b.Widths {
		lo = fnvMixUint32(lo, math.Float32bits(w))
	}
	for ri, tr := range b.Children {
		hi = fnvMixUint64(hi, uint64(ri+1)<<8|uint64(len(tr.Children)))
		for _, cell := range tr.Children {
			st := makeRunsStamp(cell.Runs)
			lo = fnvMixUint64(lo, st.lo^uint64(cell.ColSpan)<<16^uint64(cell.RowSpan))
			hi = fnvMixUint64(hi, st.hi)
		}
	}
	return runsStamp{lo: lo, hi: hi}
}

func (e *Editor) recordTableHeight(b *document.Block, width float32, cols int, stamp runsStamp, innerH float32) {
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	meta := uint64(len(b.Children))<<16 | uint64(cols)
	e.heightHints[b.ID] = heightHint{
		width:    width,
		height:   innerH,
		fontHash: meta,
		stamp:    stamp,
		exact:    true,
	}
}

func (e *Editor) estimateTableInnerHeight(b *document.Block, w float32, widths []float32, cols int, stamp runsStamp) float32 {
	nrows := len(b.Children)
	meta := uint64(nrows)<<16 | uint64(cols)
	if e.heightHints != nil {
		if hint, ok := e.heightHints[b.ID]; ok && hint.width == w && hint.fontHash == meta && hint.stamp == stamp {
			return max(42, hint.height)
		}
	}
	total := float32(0)
	for _, tr := range b.Children {
		rh := float32(42)
		for ci, cell := range tr.Children {
			if ci < len(widths) {
				cw := max(24, widths[ci]-28)
				lines := 1
				cur := float32(0)
				for _, run := range cell.Runs {
					for _, r := range run.Text {
						if r == '\n' {
							lines++
							cur = 0
							continue
						}
						adv := float32(7.5)
						if r >= 0x1100 {
							adv = 14
						}
						if cur+adv > cw && cur > 0 {
							lines++
							cur = adv
						} else {
							cur += adv
						}
					}
				}
				rh = max(rh, float32(lines)*22+4)
			}
		}
		total += rh
	}
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	e.heightHints[b.ID] = heightHint{
		width:    w,
		height:   total,
		fontHash: meta,
		stamp:    stamp,
		exact:    false,
	}
	return total
}

func (e *Editor) layoutTable(b *document.Block, x, y, w float32, canDefer bool, winTop, winBottom float32, leafOf func(string) document.Leaf, cellTop *float32, header *bool, blockLayout func(*document.Block, float32, float32, float32) float32) float32 {
	start := y
	y += 22
	top := y
	cols := len(b.Children[0].Children)
	widths := make([]float32, cols)
	sum := float32(cols)
	if len(b.Widths) == cols {
		sum = 0
		for _, v := range b.Widths {
			sum += v
		}
	}
	for i := range widths {
		value := float32(1)
		if len(b.Widths) == cols {
			value = b.Widths[i]
		}
		widths[i] = w * value / sum
	}
	colEdges := []float32{x}
	for _, cw := range widths {
		colEdges = append(colEdges, colEdges[len(colEdges)-1]+cw)
	}
	covered := document.CoveredCells(b)
	nrows := len(b.Children)

	var tStamp runsStamp
	if canDefer || e.shouldVirtualize() {
		tStamp = tableStamp(b)
	}
	if canDefer {
		estInner := e.estimateTableInnerHeight(b, w, widths, cols, tStamp)
		estTotal := estInner + 44
		if start+estTotal < winTop || start > winBottom {
			rowH := estInner / float32(max(1, nrows))
			rowEdges := make([]float32, nrows+1)
			ry := top
			rowEdges[0] = ry
			for ri := 0; ri < nrows; ri++ {
				ry += rowH
				rowEdges[ri+1] = ry
			}
			table := ui.Rect{X: x, Y: top, W: w, H: estInner}
			var cells []decoration
			for ri, tr := range b.Children {
				for ci, cell := range tr.Children {
					if covered[cell] {
						continue
					}
					spanCols, spanRows := document.Span(cell)
					spanCols, spanRows = min(spanCols, cols-ci), min(spanRows, nrows-ri)
					rect := ui.Rect{X: colEdges[ci], Y: rowEdges[ri], W: colEdges[ci+spanCols] - colEdges[ci], H: rowEdges[ri+spanRows] - rowEdges[ri]}
					e.rows = append(e.rows, row{
						leafRevision: e.Revision(),
						leaf:         leafOf(cell.ID),
						rect:         rect,
						x:            colEdges[ci] + 14,
						y:            rect.Y + 7,
						size:         14,
						tableHeader:  ri == 0,
					})
					cells = append(cells, decoration{kind: "cell", rect: rect, table: table, header: ri == 0, right: ci+spanCols < cols, below: ri+spanRows < nrows, tone: cell.Background, id: cell.ID, ownerID: b.ID, rowIdx: ri, colIdx: ci})
				}
			}
			e.decorations = append(e.decorations, cells...)
			e.decorations = append(e.decorations, decoration{kind: "table", rect: table, id: b.ID, ownerID: b.ID, cols: colEdges, rows: rowEdges})
			cx := x
			for col := 0; col < cols-1; col++ {
				cx += widths[col]
				e.resizers = append(e.resizers, columnHandle{id: b.ID, col: col, x: cx, y: top, h: estInner, width: w, widths: widths})
			}
			return estTotal
		}
	}

	var cells []decoration
	type placed struct {
		index, r, c, rows, cols int
		h                       float32
		cell                    *document.Block
	}
	var spots []placed
	heights := make([]float32, nrows)
	rowTop := top
	for ri, tr := range b.Children {
		heights[ri] = 42
		*header = ri == 0
		*cellTop = rowTop
		for ci, cell := range tr.Children {
			if covered[cell] {
				continue
			}
			spanCols, spanRows := document.Span(cell)
			spanCols, spanRows = min(spanCols, cols-ci), min(spanRows, nrows-ri)
			cw := colEdges[ci+spanCols] - colEdges[ci]
			index := len(e.rows)
			h := blockLayout(cell, colEdges[ci]+14, 0, max(24, cw-28)) + 4
			spots = append(spots, placed{index, ri, ci, spanRows, spanCols, h, cell})
			if spanRows == 1 {
				heights[ri] = max(heights[ri], h)
			}
		}
		*header = false
		rowTop += heights[ri]
	}
	*cellTop = 0
	for _, sp := range spots {
		if sp.rows > 1 {
			sum := float32(0)
			for _, h := range heights[sp.r : sp.r+sp.rows] {
				sum += h
			}
			if sum < sp.h {
				heights[sp.r+sp.rows-1] += sp.h - sum
			}
		}
	}
	rowEdges := []float32{y}
	for _, h := range heights {
		y += h
		rowEdges = append(rowEdges, y)
	}
	for _, sp := range spots {
		rect := ui.Rect{X: colEdges[sp.c], Y: rowEdges[sp.r], W: colEdges[sp.c+sp.cols] - colEdges[sp.c], H: rowEdges[sp.r+sp.rows] - rowEdges[sp.r]}
		r := &e.rows[sp.index]
		r.y += rect.Y + 7
		r.rect = rect
		cells = append(cells, decoration{kind: "cell", rect: rect, header: sp.r == 0, right: sp.c+sp.cols < cols, below: sp.r+sp.rows < nrows, tone: sp.cell.Background, id: sp.cell.ID, ownerID: b.ID, rowIdx: sp.r, colIdx: sp.c})
	}
	table := ui.Rect{X: x, Y: top, W: w, H: y - top}
	for _, d := range cells {
		d.table = table
		e.decorations = append(e.decorations, d)
	}
	e.decorations = append(e.decorations, decoration{kind: "table", rect: table, id: b.ID, ownerID: b.ID, cols: colEdges, rows: rowEdges})
	cx := x
	for col := 0; col < cols-1; col++ {
		cx += widths[col]
		e.resizers = append(e.resizers, columnHandle{id: b.ID, col: col, x: cx, y: top, h: y - top, width: w, widths: widths})
	}
	if e.shouldVirtualize() {
		e.recordTableHeight(b, w, cols, tStamp, y-top)
	}
	return y - start + 22
}

func (e *Editor) blockMetrics(b *document.Block, w float32, header bool) blockMetric {
	// Body text is 16 on 26 with 8 between blocks; table cells are 14 on 22.
	m := blockMetric{
		font:   ui.Font{Family: bodyFamily(e.fontFamily), Size: 16, LineHeight: 26.0 / 16},
		bottom: 8,
		gap:    8,
	}
	if e.readingSize >= 12 && e.readingSize <= 28 {
		m.font.Size = e.readingSize
	}
	if e.readingLine >= 1.2 && e.readingLine <= 2.2 {
		m.font.LineHeight = e.readingLine
	}
	if b.Kind == "cell" {
		m.font.Size, m.font.LineHeight = 14, 22.0/14
	}
	switch b.Kind {
	case "heading":
		level := min(6, max(1, b.Level))
		m.font.Size = []float32{26, 22, 20, 18, 16, 16}[level-1]
		m.font.LineHeight = []float32{36.4, 30.8, 28, 25.2, 26, 26}[level-1] / m.font.Size
		m.font.Weight = 500
		m.top = []float32{26, 22, 20, 18, 18, 16}[level-1]
		m.bottom = []float32{10, 8, 8, 8, 8, 8}[level-1]
		if len(e.Doc.Blocks) > 0 && e.Doc.Blocks[0].ID == b.ID {
			// The first heading is the page title: 34 on 55.25.
			m.font.Size, m.font.Weight, m.font.LineHeight = 34, 500, 55.25/34
			m.top, m.bottom = 0, 22
		}
	case "code", "raw", "math":
		m.font.Family, m.font.Size, m.font.LineHeight = "monospace", 14, 22.0/14
		m.gap = 8
		m.left, m.right, m.top, m.bottom = 20, 20, 16, 16+m.gap
		if b.Kind == "code" {
			m.left, m.top, m.bottom = 60, 40, 26+m.gap
		}
		if b.Kind == "math" {
			m.font.Size, m.font.LineHeight = 14.4, 22.0/14.4
			m.left, m.right = mathPaddingX, mathPaddingX
		}
	case "quote":
		m.left, m.top, m.bottom, m.gap = 14, 0, 8, 8
	case "callout":
		m.gap = 8
		m.left, m.right, m.top, m.bottom = 48, 16, 16, 16+m.gap
	case "bullet", "numbered", "task":
		m.left = 24 + float32(b.Indent)*24
		m.bottom = 8
	case "toggle":
		m.left = 20 + float32(b.Indent)*20
	}
	if b.Kind == "footnote" {
		m.left, m.top = 36, 12
	}
	m.shapeWidth = max(24, w-m.left-m.right)
	_ = header
	return m
}

func (e *Editor) blockShapeKey(b *document.Block, font ui.Font, header bool) shapeStyle {
	revealed := ""
	if strings.HasPrefix(e.inlineEditing, b.ID+":") {
		revealed = e.inlineEditing
	}
	lang := ""
	if b.Kind == "code" {
		lang = b.Language
	}
	return shapeStyle{font: font, align: b.Align, kind: b.Kind, language: lang, revealed: revealed, notes: e.noteKey(), checked: b.Checked, header: header, noWrap: e.isCodeNoWrap(b)}
}

func (e *Editor) blockSpans(b *document.Block, font ui.Font, header bool, theme *ui.Theme) []ui.Span {
	pg := e.page
	if b.Kind == "code" {
		if e.codeLex == nil {
			e.codeLex = map[string]*codeLexCache{}
		}
		spans := codeSpansFor(b.ID, b.Language, b.Text(), pg, e.codeLex)
		if len(spans) == 0 {
			return []ui.Span{{Text: ""}}
		}
		return spans
	}
	var spans []ui.Span
	for _, run := range b.Runs {
		style := run.Style
		span := ui.Span{Text: run.Text, Italic: style.Italic, Underline: style.Underline, Strikethrough: style.Strike}
		if style.Script != "" {
			span.Size = font.Size * .72
			span.BaselineShift = font.Size * .35
			if style.Script == "sub" {
				span.BaselineShift = -font.Size * .18
			}
		}
		if style.Bold {
			span.Weight = 600
		}
		if style.Code {
			span.Font, span.Size, span.Background, span.Radius = "monospace", font.Size*.875, pg.InlineFill, 4
			span.Color = pg.InlineText
		}
		if style.Link != "" {
			span.Color, span.Underline, span.DecorationColor = theme.Accent, style.Underline, theme.Accent.Alpha(.35)
		}
		if c, ok := pg.TextColors[style.Color]; ok {
			span.Color, span.DecorationColor = c, c
		}
		if c, ok := pg.Highlights[style.Highlight]; ok {
			span.Background, span.Radius = c, 2
		}
		switch {
		case header:
			span.Weight = max(span.Weight, 500)
		case b.Kind == "quote":
			span.Color = pg.Caption
		case b.Kind == "task" && b.Checked:
			span.Color, span.Strikethrough, span.DecorationColor = pg.TaskDone, true, pg.TaskDone
		}
		if style.Link == "" {
			spans = append(spans, e.wikiSpans(span, run, b.Kind, theme)...)
			continue
		}
		spans = append(spans, span)
	}
	if b.Kind != "math" && b.Kind != "raw" {
		spans = padAnnotations(spans, b.Runs, font.Size)
	}
	if len(spans) == 0 {
		spans = []ui.Span{{Text: ""}}
	}
	return spans
}

func (e *Editor) shapeBlock(b *document.Block, key shapeStyle, shapeWidth float32, theme *ui.Theme) cachedShape {
	font := key.font
	spans := e.blockSpans(b, font, key.header, theme)
	align := ui.Start
	if b.Align == "center" {
		align = ui.Center
	}
	if b.Align == "right" {
		align = ui.End
	}
	if b.Kind == "math" || b.Kind == "image" {
		align = ui.Center
	}
	layoutWidth := shapeWidth
	if key.noWrap {
		layoutWidth = 0
	}
	return cachedShape{
		key:      key,
		runs:     slices.Clone(b.Runs),
		width:    shapeWidth,
		layout:   e.shapeInline(b, spans, font, ui.TextShape{Width: layoutWidth, Align: align}),
		lastUsed: e.nextCacheClock(),
	}
}

func (e *Editor) recordExactHeight(id string, width, height float32, font ui.Font, runs []document.Run) {
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	e.heightHints[id] = heightHint{
		width:    width,
		height:   height,
		fontHash: fontStamp(font),
		stamp:    makeRunsStamp(runs),
		exact:    true,
	}
}

// estimateBlockHeight predicts wrapped text height without invoking CoreText
// or allocating span slices. Exact measurements and unchanged heuristic estimates
// are reused via heightHints while content, font, and wrap width match.
func (e *Editor) estimateBlockHeight(b *document.Block, font ui.Font, shapeWidth float32) float32 {
	lineH := font.Size * font.LineHeight
	fHash := fontStamp(font)
	stamp := makeRunsStamp(b.Runs)
	if e.heightHints != nil {
		if hint, ok := e.heightHints[b.ID]; ok && hint.width == shapeWidth && hint.fontHash == fHash && hint.stamp == stamp {
			if b.Kind == "math" || isMermaidBlock(b) {
				return max(hint.height, 40)
			}
			return max(hint.height, lineH)
		}
	}
	if b.Kind == "math" {
		est := float32(62 + 8)
		if e.heightHints == nil {
			e.heightHints = make(map[string]heightHint, 64)
		}
		e.heightHints[b.ID] = heightHint{width: shapeWidth, height: est, fontHash: fHash, stamp: stamp, exact: false}
		return est
	}
	if isMermaidBlock(b) {
		est := float32(180 + 8)
		if e.heightHints == nil {
			e.heightHints = make(map[string]heightHint, 64)
		}
		e.heightHints[b.ID] = heightHint{width: shapeWidth, height: est, fontHash: fHash, stamp: stamp, exact: false}
		return est
	}
	noWrap := shapeWidth < 0
	avail := shapeWidth
	if noWrap {
		avail = -shapeWidth
	} else if font.Family != "monospace" {
		// Word-boundary wrapping breaks slightly earlier than glyph sums.
		avail *= 0.94
	}
	avail = max(16, avail)
	lines := 1
	curW := float32(0)
	baseMono := font.Family == "monospace"
	for i := range b.Runs {
		run := &b.Runs[i]
		size := font.Size
		mono := baseMono || run.Style.Code
		if run.Style.Code {
			size *= 0.875
		} else if run.Style.Script != "" {
			size *= 0.72
		}
		for _, r := range run.Text {
			if r == '\n' || r == '\u2028' {
				lines++
				curW = 0
				continue
			}
			if noWrap {
				continue
			}
			var adv float32
			switch {
			case r >= 0x1100:
				adv = size
			case r == '\t':
				adv = 2 * size
			case mono:
				adv = 0.60 * size
			case r < 0x80:
				switch r {
				case 'i', 'I', 'l', '1', '!', '|', '.', ',', ';', ':', '\'', '"', '[', ']', '(', ')', ' ':
					adv = 0.30 * size
				case 'M', 'W', 'm', 'w', '@', '#', '%', '&':
					adv = 0.78 * size
				default:
					if r >= 'A' && r <= 'Z' {
						adv = 0.65 * size
					} else {
						adv = 0.52 * size
					}
				}
			default:
				adv = 0.58 * size
			}
			if curW+adv > avail && curW > 0 {
				lines++
				curW = adv
			} else {
				curW += adv
			}
		}
	}
	est := float32(lines) * lineH
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, 64)
	}
	e.heightHints[b.ID] = heightHint{
		width:    shapeWidth,
		height:   est,
		fontHash: fHash,
		stamp:    stamp,
		exact:    false,
	}
	return est
}

func (e *Editor) activeBlockOwners() (caretID, anchorID, revealID, caretOwner, anchorOwner, revealOwner string) {
	cl := e.Engine.LeafAt(e.Caret)
	caretID, caretOwner = cl.Block.ID, cl.Owner
	if e.Anchor != e.Caret {
		al := e.Engine.LeafAt(e.Anchor)
		anchorID, anchorOwner = al.Block.ID, al.Owner
	}
	if e.revealAt != nil {
		rl := e.Engine.LeafAt(e.revealAt.Start)
		revealID, revealOwner = rl.Block.ID, rl.Owner
	}
	return caretID, anchorID, revealID, caretOwner, anchorOwner, revealOwner
}

func (e *Editor) activeBlockIDs() (caretID, anchorID, revealID string) {
	caretID, anchorID, revealID, _, _, _ = e.activeBlockOwners()
	return caretID, anchorID, revealID
}

// ensureRow returns row i with its TextLayout shaped and cached, materializing
// deferred offscreen blocks on demand without breaking reading anchors.
func (e *Editor) ensureRow(i int) *row {
	r := e.indexedRow(i)
	if r.layout != nil {
		if b := r.leaf.Block; b != nil {
			if cached, ok := e.cache[b.ID]; ok {
				cached.lastUsed = e.nextCacheClock()
				e.cache[b.ID] = cached
			}
		}
		return r
	}
	e.materializeRows([]int{i}, &e.cacheTheme)
	return e.indexedRow(i)
}

// materializeViewport ensures every visible/overscan row is shaped and trims
// offscreen TextLayouts beyond the LRU budget.
func (e *Editor) materializeViewport(o Options) {
	if !e.shouldVirtualize() {
		return
	}
	winTop, winBottom := e.virtualWindow()
	targets := e.collectUnshapedRows(winTop, winBottom, true)
	if len(targets) > 0 {
		e.materializeRows(targets, o.Theme)
	}
	e.trimShapeCache(winTop, winBottom)
}

// ensureVisibleShaped shapes any deferred rows inside [Scroll.Y-slack, Scroll.Y+viewport+slack]
// before paint traverses rowSpace/decorationSpace.
func (e *Editor) ensureVisibleShaped(slack float32, theme *ui.Theme) {
	if !e.shouldVirtualize() {
		return
	}
	targets := e.collectUnshapedRows(e.Scroll.Y-slack, e.Scroll.Y+e.viewport+slack, false)
	if len(targets) > 0 {
		e.materializeRows(targets, theme)
	}
}

func (e *Editor) collectUnshapedRows(top, bottom float32, includeActive bool) []int {
	var targets []int
	for i := range e.rowSpace.between(top, bottom) {
		if e.indexedRow(i).layout == nil {
			targets = append(targets, i)
		}
	}
	if includeActive {
		caretID, anchorID, revealID := e.activeBlockIDs()
		for _, id := range []string{caretID, anchorID, revealID} {
			if id == "" {
				continue
			}
			if idx, ok := e.rowIDs[id]; ok && e.indexedRow(idx).layout == nil {
				targets = append(targets, idx)
			}
		}
	}
	if len(targets) > 1 {
		slices.Sort(targets)
		targets = slices.Compact(targets)
	}
	return targets
}

func (e *Editor) syncMotionAfterShift(owner string, afterY, dy float32) {
	if dy == 0 {
		return
	}
	hi, hasHI := e.handleIDs[owner]
	for id, bs := range e.motion.blocks {
		if mhi, ok := e.handleIDs[id]; ok {
			if (hasHI && mhi > hi) || (!hasHI && e.indexedHandle(mhi).top >= afterY) {
				bs.laidY += dy
				e.motion.blocks[id] = bs
			}
		}
	}
	for trID, slide := range e.motion.tableRows {
		if slide.laidY >= afterY {
			slide.laidY += dy
			e.motion.tableRows[trID] = slide
		}
	}
}

func (e *Editor) materializeTable(rowIndex int, owner string, o Options) bool {
	table := e.blockByID(owner)
	if table == nil || table.Kind != "table" {
		return false
	}
	di, found := e.decorationIDs[owner]
	if !found {
		return false
	}
	oldTable := *e.indexedDecoration(di)
	if oldTable.kind != "table" {
		return false
	}
	from, to := rowIndex, rowIndex+1
	for from > 0 && e.indexedRow(from-1).leaf.Owner == owner {
		from--
	}
	for to < len(e.rows) && e.indexedRow(to).leaf.Owner == owner {
		to++
	}
	leaves := make(map[string]document.Leaf, to-from)
	for k := from; k < to; k++ {
		leaf, ok := e.Engine.LeafByID(e.indexedRow(k).leaf.Block.ID)
		if !ok {
			return false
		}
		leaves[leaf.Block.ID] = leaf
	}
	rows, decorations, resizers := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = nil, nil, nil
	layout := e.blockLayouter(o, func(id string) document.Leaf { return leaves[id] }, nil, true)
	start := oldTable.rect.Y - 22
	height := layout(table, oldTable.rect.X, start, oldTable.rect.W)
	updated, decorated, handles := e.rows, e.decorations, e.resizers
	e.rows, e.decorations, e.resizers = rows, decorations, resizers
	if len(updated) != to-from || len(decorated) == 0 {
		return false
	}
	for k := range updated {
		r := updated[k]
		r.yApplied = e.rowYOff.query64(from + k)
		e.rows[from+k] = r
		e.rowSpace.update(from+k, r.rect)
	}
	decEnd := di + 1
	decStart := decEnd - len(decorated)
	for k := range decorated {
		dc := decorated[k]
		dc.yApplied = e.decYOff.query64(decStart + k)
		e.decorations[decStart+k] = dc
		e.decorationSpace.update(decStart+k, dc.rect)
	}
	next := 0
	for j := range e.resizers {
		if e.resizers[j].id == owner && next < len(handles) {
			rs := handles[next]
			rs.yApplied = e.resizerYOff.query64(j)
			e.resizers[j] = rs
			next++
		}
	}
	oldTotal := oldTable.rect.H + 44
	dy := height - oldTotal
	e.shiftFollowing(to, start+oldTotal, dy, decStart, decEnd, owner, 0)
	e.syncMotionAfterShift(owner, start+oldTotal, dy)
	return true
}

// materializeRows shapes ascending row indices using O(log B) suffix Fenwick
// shifts while preserving the current reading anchor and scroll glide target.
func (e *Editor) materializeRows(targets []int, theme *ui.Theme) {
	if len(targets) == 0 || theme == nil {
		return
	}
	anchor := e.resizeAnchor()
	if e.cache == nil {
		e.cache = make(map[string]cachedShape)
	}
	o := e.layoutOptions()
	o.Theme = theme
	seenTables := make(map[string]bool)

	for _, idx := range targets {
		if idx < 0 || idx >= len(e.rows) {
			continue
		}
		r := e.indexedRow(idx)
		if r.layout != nil || r.leaf.Block == nil {
			continue
		}
		b := r.leaf.Block
		if b.Kind == "cell" && r.leaf.Owner != b.ID {
			if seenTables[r.leaf.Owner] {
				continue
			}
			seenTables[r.leaf.Owner] = true
			e.materializeTable(idx, r.leaf.Owner, o)
			continue
		}

		origY, origH := r.rect.Y, r.rect.H
		m := e.blockMetrics(b, r.rect.W, r.tableHeader)
		key := e.blockShapeKey(b, m.font, r.tableHeader)
		cached, ok := e.cache[b.ID]
		if !ok || cached.key != key || cached.width != m.shapeWidth || !slices.Equal(cached.runs, b.Runs) {
			cached = e.shapeBlock(b, key, m.shapeWidth, theme)
			e.cache[b.ID] = cached
		} else {
			cached.lastUsed = e.nextCacheClock()
			e.cache[b.ID] = cached
		}
		r.layout = cached.layout
		_, h := cached.layout.Size()
		h = max(h, m.font.Size*m.font.LineHeight)
		newH := m.top + h + m.bottom
		r.y = origY + m.top
		r.rect.H = newH
		if b.Kind == "math" {
			newH = e.mathRow(r, h)
			r.rect.H = newH
		}
		var wellOff float32
		if isMermaidBlock(b) {
			newH, wellOff = e.mermaidRow(r, h, m.gap)
			r.rect.H = newH
		}
		if canDeferKind(b.Kind) {
			hintH := h
			if b.Kind == "math" || isMermaidBlock(b) {
				hintH = newH
			}
			e.recordExactHeight(b.ID, e.hintWidthFor(b, m.shapeWidth), hintH, m.font, b.Runs)
		}
		e.rowSpace.update(idx, r.rect)

		decStart, decEnd := -1, -1
		if di, hasDec := e.decorationIDs[b.ID]; hasDec {
			dc := e.indexedDecoration(di)
			dc.rect.H = newH - m.gap
			if isMermaidBlock(b) {
				dc.mermaidHidden = r.mermaidHidden
				dc.mermaidWellOff = wellOff
			}
			e.decorationSpace.update(di, dc.rect)
			decStart, decEnd = di, di+1
		}

		handleShift := float32(0)
		if hi, hasH := e.handleIDs[b.ID]; hasH {
			hd := e.indexedHandle(hi)
			prevY := hd.rect.Y
			caret := cached.layout.Caret(0)
			hd.rect.Y = r.y + caret.Y + max(caret.H, 20)/2 - 12
			handleShift = hd.rect.Y - prevY
		}
		dy := newH - origH
		e.shiftFollowing(idx+1, origY+origH, dy, decStart, decEnd, b.ID, handleShift)
		e.syncMotionAfterShift(b.ID, origY+origH, dy)
	}
	e.restoreResizeAnchor(anchor)
}

// trimShapeCache evicts the least-recently-used offscreen TextLayouts once the
// cache grows past budget, recording exact heightHints before freeing memory.
func (e *Editor) trimShapeCache(winTop, winBottom float32) {
	if len(e.cache) <= maxCachedShapes+cacheTrimHysteresis {
		return
	}
	caretID, anchorID, revealID := e.activeBlockIDs()
	type candidate struct {
		id   string
		used uint64
		row  int
	}
	candidates := make([]candidate, 0, len(e.cache)-maxCachedShapes)
	for id, cached := range e.cache {
		if id == caretID || id == anchorID || id == revealID || id == e.dragID || id == e.mathEditing || id == e.mermaidEditing || strings.HasPrefix(e.inlineEditing, id+":") {
			continue
		}
		idx, ok := e.rowIDs[id]
		if !ok {
			candidates = append(candidates, candidate{id: id, used: cached.lastUsed, row: -1})
			continue
		}
		r := e.indexedRow(idx)
		if r.leaf.Block == nil {
			continue
		}
		if r.leaf.Block.Kind == "cell" {
			if tdi, tok := e.decorationIDs[r.leaf.Owner]; tok {
				td := *e.indexedDecoration(tdi)
				if td.rect.Y+td.rect.H >= winTop && td.rect.Y <= winBottom {
					continue
				}
			}
		} else {
			if r.leaf.Owner != id || !canDeferKind(r.leaf.Block.Kind) {
				continue
			}
			if r.rect.Y+r.rect.H >= winTop && r.rect.Y <= winBottom {
				continue
			}
		}
		candidates = append(candidates, candidate{id: id, used: cached.lastUsed, row: idx})
	}
	need := len(e.cache) - maxCachedShapes
	if need <= 0 || len(candidates) == 0 {
		return
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		if a.used < b.used {
			return -1
		}
		if a.used > b.used {
			return 1
		}
		return 0
	})
	if len(candidates) > need {
		candidates = candidates[:need]
	}
	if e.heightHints == nil {
		e.heightHints = make(map[string]heightHint, len(candidates))
	}
	for _, c := range candidates {
		cached := e.cache[c.id]
		if cached.layout != nil {
			hw := cached.width
			if cached.key.noWrap {
				hw = -hw
			}
			var r *row
			if c.row >= 0 && c.row < len(e.rows) {
				r = e.indexedRow(c.row)
			}
			if r != nil && r.leaf.Block != nil && (r.leaf.Block.Kind == "math" || isMermaidBlock(r.leaf.Block)) {
				e.recordExactHeight(c.id, hw, r.rect.H, cached.key.font, cached.runs)
			} else {
				_, h := cached.layout.Size()
				h = max(h, cached.key.font.Size*cached.key.font.LineHeight)
				e.recordExactHeight(c.id, hw, h, cached.key.font, cached.runs)
			}
		}
		delete(e.cache, c.id)
		if c.row >= 0 && c.row < len(e.rows) && e.rows[c.row].leaf.Block != nil && e.rows[c.row].leaf.Block.ID == c.id {
			e.rows[c.row].layout = nil
		}
	}
}
