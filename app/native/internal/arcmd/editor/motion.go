package editor

import (
	"math"
	"sort"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

const (
	caretGlideTau = 0.036 // Seconds; reaches 95% in ~108ms.
	blockSlideTau = 0.048 // Seconds; reaches 95% in ~144ms.
	motionEpsilon = 0.25
)

type caretGlide struct {
	current, target ui.Rect
	at              time.Time
	doc             *document.Document
	ok              bool
	gliding         bool
}

type blockSlide struct {
	laidX, laidY float32
	dx, dy       float32
}

type calloutMotion struct {
	fill, border, icon ui.Color
}

type motionState struct {
	caret      caretGlide
	blocks     map[string]blockSlide
	indents    map[string]blockSlide
	tableRows  map[string]blockSlide
	tableCols  map[string]blockSlide
	dragShifts map[string]float32
	foldAngles map[string]float32
	foldFade   map[string]float32
	dimGlow    map[string]float32
	callouts   map[string]calloutMotion
	seen       map[string]bool
	dimOwner   string
	wasDim     bool
	at         time.Time
	doc        *document.Document
	width      float32
	resizing   bool
	ready      bool
	moving     bool
}

func (e *Editor) motionWindow() (top, bottom float32) {
	vp := e.viewport
	if vp <= 0 {
		vp = 900
	}
	return e.Scroll.Y - 1200, e.Scroll.Y + vp + 1200
}

func (e *Editor) visibleHandleRange(top, bottom float32) (int, int) {
	if len(e.handles) == 0 {
		return 0, 0
	}
	if !e.shouldVirtualize() {
		return 0, len(e.handles)
	}
	start := sort.Search(len(e.handles), func(i int) bool { return e.indexedHandle(i).top >= top })
	if start > 0 {
		start--
	}
	end := sort.Search(len(e.handles), func(i int) bool { return e.indexedHandle(i).top > bottom })
	return start, end
}

func (e *Editor) visibleFoldRange(top, bottom float32) (int, int) {
	if len(e.foldButtons) == 0 {
		return 0, 0
	}
	if !e.shouldVirtualize() {
		return 0, len(e.foldButtons)
	}
	start := sort.Search(len(e.foldButtons), func(i int) bool { return e.indexedFold(i).rect.Y+16 >= top })
	if start > 0 {
		start--
	}
	end := sort.Search(len(e.foldButtons), func(i int) bool { return e.indexedFold(i).rect.Y > bottom })
	return start, end
}

func (e *Editor) visibleRowRange(fn func(int)) {
	if !e.shouldVirtualize() {
		for i := range e.rows {
			fn(i)
		}
		return
	}
	for i := range e.visibleRows(1200) {
		fn(i)
	}
}

func (e *Editor) visibleDecRange(fn func(int)) {
	if !e.shouldVirtualize() {
		for i := range e.decorations {
			fn(i)
		}
		return
	}
	for i := range e.visibleDecorations(1200) {
		fn(i)
	}
}

// reset clears transient offsets when switching documents, printing, or resizing.
func (m *motionState) reset(doc *document.Document, width float32, now time.Time) {
	clear(m.blocks)
	clear(m.indents)
	clear(m.tableRows)
	clear(m.tableCols)
	clear(m.dragShifts)
	clear(m.foldAngles)
	clear(m.foldFade)
	clear(m.dimGlow)
	clear(m.callouts)
	clear(m.seen)
	m.dimOwner = ""
	m.wasDim = false
	m.doc = doc
	m.width = width
	m.at = now
	m.moving = false
}

// updateMotion reconciles canonical post-layout block positions with smoothed
// visual offsets. Canonical layout geometry is never mutated.
func (e *Editor) updateMotion(now time.Time, o Options) {
	m := &e.motion
	if m.blocks == nil {
		m.blocks = make(map[string]blockSlide, 32)
		m.indents = make(map[string]blockSlide, 32)
		m.tableRows = make(map[string]blockSlide, 16)
		m.tableCols = make(map[string]blockSlide, 16)
		m.dragShifts = make(map[string]float32, 16)
		m.foldAngles = make(map[string]float32, 16)
		m.foldFade = make(map[string]float32, 16)
		m.dimGlow = make(map[string]float32, 8)
		m.callouts = make(map[string]calloutMotion, 8)
		m.seen = make(map[string]bool, 64)
	}
	winTop, winBottom := e.motionWindow()
	hs, he := e.visibleHandleRange(winTop, winBottom)
	fs, fe := e.visibleFoldRange(winTop, winBottom)
	resizing := o.Resizing || e.resizePending
	if e.printing || e.reduceMotion || !m.ready || m.doc != e.Doc || m.width != e.width || resizing || m.resizing {
		e.unfoldPending = false
		m.reset(e.Doc, e.width, now)
		m.resizing = resizing
		m.ready = true
		for hi := hs; hi < he; hi++ {
			h := *e.indexedHandle(hi)
			m.blocks[h.id] = blockSlide{laidY: h.top}
		}
		e.visibleRowRange(func(i int) {
			r := e.indexedRow(i)
			if r.leaf.Block.Kind != "cell" {
				m.indents[r.leaf.Block.ID] = blockSlide{laidX: r.x}
			}
		})
		for fi := fs; fi < fe; fi++ {
			f := *e.indexedFold(fi)
			if f.folded {
				m.foldAngles[f.id] = -90
			} else {
				m.foldAngles[f.id] = 0
			}
		}
		if e.dim && len(e.handles) > 0 {
			m.wasDim = true
			m.dimOwner = e.Engine.LeafAt(e.Caret).Owner
		}
		e.recordCalloutColors(true, 1)
		e.recordTableRowAnchors(true, 1)
		e.recordTableColAnchors(true, 1)
		return
	}
	m.resizing = false
	wasMoving := m.moving
	dt := 0.0
	if wasMoving {
		dt = now.Sub(m.at).Seconds()
		if dt < 0 {
			dt = 0
		}
		if dt > 0.25 {
			dt = 0.25
		}
	}
	m.at = now
	decay := float32(1)
	if dt > 0 {
		decay = float32(math.Exp(-dt / blockSlideTau))
	}
	m.moving = false

	unfold := e.unfoldPending
	e.unfoldPending = false

	clear(m.seen)
	for hi := hs; hi < he; hi++ {
		h := *e.indexedHandle(hi)
		m.seen[h.id] = true
		prev, ok := m.blocks[h.id]
		var dy float32
		if ok {
			dy = prev.dy*decay + (prev.laidY - h.top)
			if math.Abs(float64(dy)) > 900 {
				dy = 0
			}
			if math.Abs(float64(dy)) < motionEpsilon {
				dy = 0
			}
		} else if unfold && len(m.blocks) > 0 {
			dy = -10
			m.foldFade[h.id] = 0.25
			m.moving = true
		}
		if dy != 0 {
			m.moving = true
		}
		m.blocks[h.id] = blockSlide{laidY: h.top, dy: dy}
	}
	if e.dragID != "" && !m.seen[e.dragID] {
		if hi := e.handleIndex(e.dragID); hi >= 0 {
			h := *e.indexedHandle(hi)
			m.seen[h.id] = true
			m.blocks[h.id] = blockSlide{laidY: h.top}
		}
	}
	for id, prev := range m.blocks {
		if m.seen[id] {
			continue
		}
		if prev.dy != 0 {
			dy := prev.dy * decay
			if math.Abs(float64(dy)) >= motionEpsilon {
				m.moving = true
				prev.dy = dy
				m.blocks[id] = prev
				m.seen[id] = true
				continue
			}
		}
		delete(m.blocks, id)
	}
	for id, op := range m.foldFade {
		if !m.seen[id] {
			delete(m.foldFade, id)
			continue
		}
		if dt > 0 {
			op += (1 - op) * (1 - float32(math.Exp(-dt/0.045)))
		}
		if op >= 0.99 {
			delete(m.foldFade, id)
		} else {
			m.foldFade[id] = op
			m.moving = true
		}
	}

	dimSeen := false
	if e.dim && len(e.handles) > 0 {
		owner := e.Engine.LeafAt(e.Caret).Owner
		dimSeen = m.seen[owner]
		_ = dimSeen
		if m.wasDim && owner != m.dimOwner && m.dimOwner != "" && m.seen[m.dimOwner] {
			m.dimGlow[m.dimOwner] = max(m.dimGlow[m.dimOwner], 0.85)
			m.moving = true
		}
		delete(m.dimGlow, owner)
		m.dimOwner = owner
		m.wasDim = true
		glowDecay := float32(1)
		if dt > 0 {
			glowDecay = float32(math.Exp(-dt / 0.065))
		}
		for id, g := range m.dimGlow {
			if !m.seen[id] {
				delete(m.dimGlow, id)
				continue
			}
			g *= glowDecay
			if g < 0.02 {
				delete(m.dimGlow, id)
			} else {
				m.dimGlow[id] = g
				m.moving = true
			}
		}
	} else {
		m.wasDim = false
		m.dimOwner = ""
		clear(m.dimGlow)
	}

	clear(m.seen)
	foldAlpha := float32(0)
	if dt > 0 {
		foldAlpha = 1 - float32(math.Exp(-dt/0.038))
	}
	for fi := fs; fi < fe; fi++ {
		f := *e.indexedFold(fi)
		m.seen[f.id] = true
		target := float32(0)
		if f.folded {
			target = -90
		}
		cur, ok := m.foldAngles[f.id]
		if !ok {
			m.foldAngles[f.id] = target
			continue
		}
		if foldAlpha > 0 {
			cur += (target - cur) * foldAlpha
		}
		if math.Abs(float64(target-cur)) < 0.6 {
			cur = target
		} else {
			m.moving = true
		}
		m.foldAngles[f.id] = cur
	}
	for id := range m.foldAngles {
		if !m.seen[id] {
			delete(m.foldAngles, id)
		}
	}

	clear(m.seen)
	e.visibleRowRange(func(i int) {
		r := e.indexedRow(i)
		if r.leaf.Block == nil || r.leaf.Block.Kind == "cell" {
			return
		}
		id := r.leaf.Block.ID
		m.seen[id] = true
		prev, ok := m.indents[id]
		var dx float32
		if ok {
			dx = prev.dx*decay + (prev.laidX - r.x)
			if math.Abs(float64(dx)) > 320 {
				dx = 0
			}
			if math.Abs(float64(dx)) < motionEpsilon {
				dx = 0
			}
		}
		if dx != 0 {
			m.moving = true
		}
		m.indents[id] = blockSlide{laidX: r.x, dx: dx}
	})
	for id, prev := range m.indents {
		if m.seen[id] {
			continue
		}
		if prev.dx != 0 {
			dx := prev.dx * decay
			if math.Abs(float64(dx)) >= motionEpsilon {
				m.moving = true
				prev.dx = dx
				m.indents[id] = prev
				continue
			}
		}
		delete(m.indents, id)
	}

	calloutAlpha := float32(0)
	if dt > 0 {
		calloutAlpha = 1 - float32(math.Exp(-dt/0.042))
	}
	e.recordCalloutColors(false, calloutAlpha)
	e.recordTableRowAnchors(false, decay)
	e.recordTableColAnchors(false, decay)
	e.updateDragShifts(dt)
}

// stepMotion advances active visual slide offsets during paint-only animation
// frames without rebuilding the widget tree.
func (e *Editor) stepMotion(now time.Time) {
	m := &e.motion
	if e.printing || e.reduceMotion || !m.ready || m.resizing {
		return
	}
	plan := e.currentDragPlan()
	if !m.moving && !plan.active {
		m.at = now
		return
	}
	dt := now.Sub(m.at).Seconds()
	if dt <= 0 {
		return
	}
	if dt > 0.25 {
		dt = 0.25
	}
	m.at = now
	decay := float32(math.Exp(-dt / blockSlideTau))
	m.moving = false
	for id, b := range m.blocks {
		if b.dy != 0 {
			b.dy *= decay
			if math.Abs(float64(b.dy)) < motionEpsilon {
				b.dy = 0
			} else {
				m.moving = true
			}
			m.blocks[id] = b
		}
	}
	for id, ind := range m.indents {
		if ind.dx != 0 {
			ind.dx *= decay
			if math.Abs(float64(ind.dx)) < motionEpsilon {
				ind.dx = 0
			} else {
				m.moving = true
			}
			m.indents[id] = ind
		}
	}
	for id, tr := range m.tableRows {
		if tr.dy != 0 {
			tr.dy *= decay
			if math.Abs(float64(tr.dy)) < motionEpsilon {
				tr.dy = 0
			} else {
				m.moving = true
			}
			m.tableRows[id] = tr
		}
	}
	for id, tc := range m.tableCols {
		if tc.dx != 0 {
			tc.dx *= decay
			if math.Abs(float64(tc.dx)) < motionEpsilon {
				tc.dx = 0
			} else {
				m.moving = true
			}
			m.tableCols[id] = tc
		}
	}
	winTop, winBottom := e.motionWindow()
	fs, fe := e.visibleFoldRange(winTop, winBottom)
	foldAlpha := 1 - float32(math.Exp(-dt/0.038))
	for fi := fs; fi < fe; fi++ {
		f := *e.indexedFold(fi)
		target := float32(0)
		if f.folded {
			target = -90
		}
		if cur, ok := m.foldAngles[f.id]; ok && cur != target {
			cur += (target - cur) * foldAlpha
			if math.Abs(float64(target-cur)) < 0.6 {
				cur = target
			} else {
				m.moving = true
			}
			m.foldAngles[f.id] = cur
		}
	}
	fadeAlpha := 1 - float32(math.Exp(-dt/0.045))
	for id, op := range m.foldFade {
		op += (1 - op) * fadeAlpha
		if op >= 0.99 {
			delete(m.foldFade, id)
		} else {
			m.foldFade[id] = op
			m.moving = true
		}
	}
	glowDecay := float32(math.Exp(-dt / 0.065))
	for id, g := range m.dimGlow {
		g *= glowDecay
		if g < 0.02 {
			delete(m.dimGlow, id)
		} else {
			m.dimGlow[id] = g
			m.moving = true
		}
	}
	e.recordCalloutColors(false, 1-float32(math.Exp(-dt/0.042)))
	e.updateDragShifts(dt)
}

func colorClose(a, b ui.Color) bool {
	diff := func(x, y uint8) int {
		d := int(x) - int(y)
		if d < 0 {
			return -d
		}
		return d
	}
	return diff(a.R, b.R) <= 1 && diff(a.G, b.G) <= 1 && diff(a.B, b.B) <= 1 && diff(a.A, b.A) <= 1
}

func (e *Editor) recordCalloutColors(snap bool, alpha float32) {
	m := &e.motion
	clear(m.seen)
	e.visibleDecRange(func(i int) {
		d := e.indexedDecoration(i)
		if d.kind != "callout" || d.id == "" {
			return
		}
		m.seen[d.id] = true
		target := e.page.Callouts[d.tone]
		cur, ok := m.callouts[d.id]
		if snap || !ok {
			m.callouts[d.id] = calloutMotion{fill: target.Fill, border: target.Border, icon: target.Icon}
			return
		}
		if alpha > 0 {
			cur.fill = cur.fill.Mix(target.Fill, alpha)
			cur.border = cur.border.Mix(target.Border, alpha)
			cur.icon = cur.icon.Mix(target.Icon, alpha)
		}
		if colorClose(cur.fill, target.Fill) && colorClose(cur.border, target.Border) && colorClose(cur.icon, target.Icon) {
			cur = calloutMotion{fill: target.Fill, border: target.Border, icon: target.Icon}
		} else {
			m.moving = true
		}
		m.callouts[d.id] = cur
	})
	for id := range m.callouts {
		if !m.seen[id] {
			delete(m.callouts, id)
		}
	}
}

func (e *Editor) recordTableRowAnchors(snap bool, decay float32) {
	m := &e.motion
	clear(m.seen)
	e.visibleDecRange(func(i int) {
		d := e.indexedDecoration(i)
		if d.kind != "table" || d.id == "" {
			return
		}
		b := e.blockByID(d.id)
		if b == nil || len(b.Children)+1 != len(d.rows) {
			return
		}
		for ri, tr := range b.Children {
			m.seen[tr.ID] = true
			y := d.rows[ri]
			if snap {
				m.tableRows[tr.ID] = blockSlide{laidY: y}
				continue
			}
			prev, ok := m.tableRows[tr.ID]
			var dy float32
			if ok {
				dy = prev.dy*decay + (prev.laidY - y)
				if math.Abs(float64(dy)) > 600 || math.Abs(float64(dy)) < motionEpsilon {
					dy = 0
				}
			} else if len(m.tableRows) > 0 && ri > 0 {
				// Newly inserted table rows slide open gently from the seam above.
				h := d.rows[ri+1] - d.rows[ri]
				dy = -min(18, h*0.45)
			}
			if dy != 0 {
				m.moving = true
			}
			m.tableRows[tr.ID] = blockSlide{laidY: y, dy: dy}
		}
	})
	for id := range m.tableRows {
		if !m.seen[id] {
			delete(m.tableRows, id)
		}
	}
}

func (e *Editor) recordTableColAnchors(snap bool, decay float32) {
	m := &e.motion
	clear(m.seen)
	e.visibleDecRange(func(i int) {
		d := e.indexedDecoration(i)
		if d.kind != "table" || d.id == "" {
			return
		}
		b := e.blockByID(d.id)
		if b == nil || len(b.Children) == 0 || len(b.Children[0].Children)+1 != len(d.cols) {
			return
		}
		for ci, cell := range b.Children[0].Children {
			m.seen[cell.ID] = true
			x := d.cols[ci]
			if snap {
				m.tableCols[cell.ID] = blockSlide{laidX: x}
				continue
			}
			prev, ok := m.tableCols[cell.ID]
			var dx float32
			if ok {
				dx = prev.dx*decay + (prev.laidX - x)
				if math.Abs(float64(dx)) > 720 || math.Abs(float64(dx)) < motionEpsilon {
					dx = 0
				}
			} else if len(m.tableCols) > 0 && ci > 0 {
				w := d.cols[ci+1] - d.cols[ci]
				dx = -min(18, w*0.35)
			}
			if dx != 0 {
				m.moving = true
			}
			m.tableCols[cell.ID] = blockSlide{laidX: x, dx: dx}
		}
	})
	for id := range m.tableCols {
		if !m.seen[id] {
			delete(m.tableCols, id)
		}
	}
}

// blockBottom returns the bottom Y of handle index j in page coordinates.
func (e *Editor) blockBottom(j int) float32 {
	if j+1 < len(e.handles) {
		return e.indexedHandle(j + 1).top
	}
	return e.contentEnd
}

// dragPlan describes the active top-level block drag displacement and slot.
type dragPlan struct {
	active       bool
	first, last  int
	slotY, slotH float32
	floatDY      float32
}

func (e *Editor) currentDragPlan() dragPlan {
	if e.dragID == "" || !e.dragActive || len(e.handles) == 0 {
		return dragPlan{}
	}
	old := e.handleIndex(e.dragID)
	if old < 0 {
		return dragPlan{}
	}
	first, last := old, old
	multi := len(e.blocks.ids) > 1 && e.blockSelected(e.dragID)
	if multi {
		first, last = e.selectionEnds()
		if first < 0 || last < 0 {
			return dragPlan{}
		}
	}
	slotH := max(26, e.blockBottom(last)-e.indexedHandle(first).top)
	slotY := e.indexedHandle(first).top
	target := max(0, min(e.dropIndex, len(e.handles)-1))
	if multi {
		switch {
		case target < first:
			slotY = e.indexedHandle(target).top
		case target > last:
			slotY = e.blockBottom(target) - slotH
		}
	} else {
		switch {
		case target < old:
			slotY = e.indexedHandle(target).top
		case target > old:
			slotY = e.blockBottom(target) - slotH
		}
	}
	return dragPlan{
		active:  true,
		first:   first,
		last:    last,
		slotY:   slotY,
		slotH:   slotH,
		floatDY: e.dragY - e.dragStartY,
	}
}

func (e *Editor) updateDragShifts(dt float64) {
	m := &e.motion
	plan := e.currentDragPlan()
	if !plan.active {
		if len(m.dragShifts) > 0 {
			decay := float32(1)
			if dt > 0 {
				decay = float32(math.Exp(-dt / blockSlideTau))
			}
			for id, v := range m.dragShifts {
				v *= decay
				if math.Abs(float64(v)) < motionEpsilon {
					delete(m.dragShifts, id)
				} else {
					m.dragShifts[id] = v
					m.moving = true
				}
			}
		}
		return
	}
	alpha := float32(0)
	if dt > 0 {
		alpha = 1 - float32(math.Exp(-dt/0.038))
	}
	target := max(0, min(e.dropIndex, len(e.handles)-1))
	for idx, h := range e.handles {
		var want float32
		switch {
		case idx >= plan.first && idx <= plan.last:
			want = 0
		case target < plan.first && idx >= target && idx < plan.first:
			want = plan.slotH
		case target > plan.last && idx > plan.last && idx <= target:
			want = -plan.slotH
		}
		cur := m.dragShifts[h.id]
		if alpha > 0 {
			cur += (want - cur) * alpha
		}
		if math.Abs(float64(want-cur)) < motionEpsilon {
			cur = want
		} else {
			m.moving = true
		}
		if cur == 0 && want == 0 {
			delete(m.dragShifts, h.id)
		} else {
			m.dragShifts[h.id] = cur
		}
	}
}

func (e *Editor) isDraggedOwner(owner string, plan dragPlan) bool {
	if !plan.active {
		return false
	}
	idx := e.handleIndex(owner)
	return idx >= plan.first && idx <= plan.last
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// visualOffset returns the paint-time (dx, dy) offset for row r.
func (e *Editor) visualOffset(r row) (dx, dy float32) {
	if e.printing || e.reduceMotion || r.leaf.Block == nil {
		return 0, 0
	}
	blockID, ownerID := r.leaf.Block.ID, r.leaf.Owner
	dy = e.ownerVisualDY(ownerID)
	m := &e.motion
	if ind, ok := m.indents[blockID]; ok {
		dx += ind.dx
	}
	if blockID != ownerID && r.leaf.Block.Kind == "cell" {
		if _, rowIdx, colIdx, ok := e.Doc.CellAt(blockID); ok {
			dy += e.tableRowVisualDY(ownerID, rowIdx)
			if e.tableDrag.active && e.tableDrag.id == ownerID && e.tableDrag.from == rowIdx {
				dy += e.tableDrag.y - e.tableDrag.startY
			}
			dx += e.tableColVisualDX(ownerID, colIdx)
			if e.tableColDrag.active && e.tableColDrag.id == ownerID && e.tableColDrag.from == colIdx {
				dx += e.tableColDrag.x - e.tableColDrag.startX
			}
		}
	}
	return dx, dy
}

func (e *Editor) ownerVisualDY(ownerID string) float32 {
	if e.printing || e.reduceMotion || ownerID == "" {
		return 0
	}
	var dy float32
	if b, ok := e.motion.blocks[ownerID]; ok {
		dy += b.dy
	}
	if s, ok := e.motion.dragShifts[ownerID]; ok {
		dy += s
	}
	if plan := e.currentDragPlan(); plan.active && e.isDraggedOwner(ownerID, plan) {
		dy += plan.floatDY
	}
	return dy
}

func (e *Editor) tableRowVisualDY(tableID string, rowIdx int) float32 {
	if e.printing || e.reduceMotion {
		return 0
	}
	var dy float32
	if b := e.blockByID(tableID); b != nil && rowIdx >= 0 && rowIdx < len(b.Children) {
		if slide, ok := e.motion.tableRows[b.Children[rowIdx].ID]; ok {
			dy += slide.dy
		}
	}
	dy += e.tableDragShift(tableID, rowIdx)
	return dy
}

func (e *Editor) tableColVisualDX(tableID string, colIdx int) float32 {
	if e.printing || e.reduceMotion {
		return 0
	}
	var dx float32
	if b := e.blockByID(tableID); b != nil && len(b.Children) > 0 && colIdx >= 0 && colIdx < len(b.Children[0].Children) {
		if slide, ok := e.motion.tableCols[b.Children[0].Children[colIdx].ID]; ok {
			dx += slide.dx
		}
	}
	dx += e.tableColDragShift(tableID, colIdx)
	return dx
}

// commitBlockDragMotion seeds block slide offsets right before MoveBlock/MoveBlocks
// so dropped blocks settle smoothly from their dragged coordinates without jumping.
func (e *Editor) commitBlockDragMotion() {
	if e.reduceMotion || e.printing {
		return
	}
	plan := e.currentDragPlan()
	if !plan.active {
		return
	}
	m := &e.motion
	for idx, h := range e.handles {
		cur := h.top + m.blocks[h.id].dy
		if idx >= plan.first && idx <= plan.last {
			cur = h.top + plan.floatDY
		} else {
			cur += m.dragShifts[h.id]
		}
		m.blocks[h.id] = blockSlide{laidY: cur}
	}
	clear(m.dragShifts)
}

// stepCaretGlide advances the smoothed caret rectangle toward target and reports
// the painted rectangle and whether the caret is actively gliding.
func (e *Editor) stepCaretGlide(now time.Time, target ui.Rect) (ui.Rect, bool) {
	g := &e.motion.caret
	if e.printing || e.reduceMotion || e.motion.resizing {
		*g = caretGlide{current: target, target: target, at: now, doc: e.Doc, ok: true}
		return target, false
	}
	dist := math.Hypot(float64(target.X-g.current.X), float64(target.Y-g.current.Y))
	if !g.ok || g.doc != e.Doc || dist > 640 {
		*g = caretGlide{current: target, target: target, at: now, doc: e.Doc, ok: true}
		return target, false
	}
	dt := 0.0
	if g.gliding {
		dt = now.Sub(g.at).Seconds()
		if dt < 0 {
			dt = 0
		}
		if dt > 0.25 {
			dt = 0.25
		}
	}
	g.at = now
	g.target = target
	if dt > 0 {
		alpha := 1 - float32(math.Exp(-dt/caretGlideTau))
		g.current.X += (target.X - g.current.X) * alpha
		g.current.Y += (target.Y - g.current.Y) * alpha
		g.current.W = target.W
		g.current.H += (target.H - g.current.H) * alpha
	}
	if math.Abs(float64(target.X-g.current.X)) < 0.2 &&
		math.Abs(float64(target.Y-g.current.Y)) < 0.2 &&
		math.Abs(float64(target.H-g.current.H)) < 0.2 {
		g.current = target
		g.gliding = false
		return target, false
	}
	g.gliding = true
	return g.current, true
}
