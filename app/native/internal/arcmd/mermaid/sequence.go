package mermaid

import (
	"fmt"
	"strconv"
	"strings"
)

type seqParticipant struct {
	id    string
	label string
	actor bool
	w, h  float32
	lw    float32
	lh    float32
	x     float32 // center X
}

type seqEventKind uint8

const (
	seqEventMsg seqEventKind = iota
	seqEventNote
)

type seqNotePos uint8

const (
	seqNoteLeft seqNotePos = iota
	seqNoteRight
	seqNoteOver
)

type seqActivation struct {
	part   int
	depth  int
	startY float32
	endY   float32
	closed bool
}

type seqFragClause struct {
	y     float32
	label string
	lw    float32
	lh    float32
}

type seqFragment struct {
	kind     string
	title    string
	titleW   float32
	titleH   float32
	kwW      float32
	kwH      float32
	depth    int
	startY   float32
	endY     float32
	minX     float32
	maxX     float32
	hasSpan  bool
	clauses  []seqFragClause
	parent   int
	maxChild int // max depth below this fragment
}

type seqEvent struct {
	kind       seqEventKind
	from, to   int
	style      EdgeKind
	startM     MarkerKind
	endM       MarkerKind
	text       string
	notePos    seqNotePos
	lw, lh     float32
	boxW, boxH float32
	y          float32
	minX, maxX float32
	fragStack  []int
}

func layoutSequence(lines []string, measure MeasureFunc) (Diagram, error) {
	const (
		outerPad     = float32(24)
		partFontSize = float32(13)
		msgFontSize  = float32(12)
		noteFontSize = float32(11.5)
		fragFontSize = float32(11)
		minPartW     = float32(86)
		minPartH     = float32(38)
		actorIconH   = float32(30)
		actWidth     = float32(10)
	)

	var (
		parts   []*seqParticipant
		partMap = map[string]int{}
		events  []*seqEvent
		frags   []*seqFragment
		fStack  []int
		autoNum bool
		seqNum  = 1
	)

	ensurePart := func(id, label string, actor, explicit bool) int {
		id = strings.TrimSpace(id)
		id = strings.Trim(id, `"'`)
		if idx, ok := partMap[id]; ok {
			p := parts[idx]
			if explicit {
				if label != "" {
					p.label = label
				}
				if actor {
					p.actor = true
				}
			}
			return idx
		}
		lbl := label
		if lbl == "" {
			lbl = id
		}
		idx := len(parts)
		parts = append(parts, &seqParticipant{
			id:    id,
			label: lbl,
			actor: actor,
		})
		partMap[id] = idx
		return idx
	}

	// Track per-event fragment snapshots & line items in order.
	type stepKind uint8
	const (
		stepMsg stepKind = iota
		stepNote
		stepFragStart
		stepFragClause
		stepFragEnd
		stepAct
		stepDeact
	)
	type seqStep struct {
		kind    stepKind
		evIdx   int
		fragIdx int
		clause  string
		part    int
		actOp   byte // '+' or '-' or 0
	}
	var steps []seqStep

	for lineNo, raw := range lines[1:] {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		low := strings.ToLower(trimmed)

		// Optional Mermaid 10+ "create participant ..." / "destroy ...".
		if strings.HasPrefix(low, "create ") {
			trimmed = strings.TrimSpace(trimmed[len("create "):])
			low = strings.ToLower(trimmed)
		} else if strings.HasPrefix(low, "destroy ") {
			continue
		}

		// Declarations & directives.
		switch {
		case strings.HasPrefix(low, "participant ") || strings.HasPrefix(low, "actor "):
			isActor := strings.HasPrefix(low, "actor ")
			kwLen := len("participant ")
			if isActor {
				kwLen = len("actor ")
			}
			id, label := parseParticipantDecl(trimmed[kwLen:])
			if id == "" {
				return Diagram{}, fmt.Errorf("line %d: invalid %s declaration", lineNo+2, strings.Fields(low)[0])
			}
			ensurePart(id, label, isActor, true)
			continue

		case low == "autonumber" || strings.HasPrefix(low, "autonumber "):
			autoNum = true
			rest := strings.Fields(trimmed)
			if len(rest) >= 2 {
				if n, err := strconv.Atoi(rest[1]); err == nil && n > 0 {
					seqNum = n
				}
			}
			continue

		case strings.HasPrefix(low, "title ") || low == "title" ||
			strings.HasPrefix(low, "link ") || strings.HasPrefix(low, "links ") ||
			strings.HasPrefix(low, "properties ") || strings.HasPrefix(low, "details "):
			continue

		case strings.HasPrefix(low, "activate "):
			id := strings.TrimSpace(trimmed[len("activate "):])
			if id == "" {
				return Diagram{}, fmt.Errorf("line %d: activate missing participant", lineNo+2)
			}
			pIdx := ensurePart(id, "", false, false)
			steps = append(steps, seqStep{kind: stepAct, part: pIdx})
			continue

		case strings.HasPrefix(low, "deactivate "):
			id := strings.TrimSpace(trimmed[len("deactivate "):])
			if id == "" {
				return Diagram{}, fmt.Errorf("line %d: deactivate missing participant", lineNo+2)
			}
			pIdx := ensurePart(id, "", false, false)
			steps = append(steps, seqStep{kind: stepDeact, part: pIdx})
			continue

		case low == "end":
			if len(fStack) == 0 {
				return Diagram{}, fmt.Errorf("unexpected end in sequence diagram")
			}
			fIdx := fStack[len(fStack)-1]
			fStack = fStack[:len(fStack)-1]
			steps = append(steps, seqStep{kind: stepFragEnd, fragIdx: fIdx})
			continue
		}

		// Fragment starters & clauses.
		if kind, title, isStart := parseFragStart(trimmed); isStart {
			parent := -1
			if len(fStack) > 0 {
				parent = fStack[len(fStack)-1]
			}
			fIdx := len(frags)
			frags = append(frags, &seqFragment{
				kind:   kind,
				title:  title,
				depth:  len(fStack),
				parent: parent,
			})
			fStack = append(fStack, fIdx)
			steps = append(steps, seqStep{kind: stepFragStart, fragIdx: fIdx})
			continue
		}
		if clause, ok := parseFragClause(trimmed); ok {
			if len(fStack) == 0 {
				return Diagram{}, fmt.Errorf("line %d: unexpected clause outside fragment", lineNo+2)
			}
			steps = append(steps, seqStep{
				kind:    stepFragClause,
				fragIdx: fStack[len(fStack)-1],
				clause:  clause,
			})
			continue
		}

		// Note statements.
		if pos, idA, idB, text, ok := parseSeqNote(trimmed); ok {
			pA := ensurePart(idA, "", false, false)
			pB := pA
			if idB != "" {
				pB = ensurePart(idB, "", false, false)
			}
			if pos == seqNoteOver && pA > pB {
				pA, pB = pB, pA
			}
			evIdx := len(events)
			snap := append([]int(nil), fStack...)
			events = append(events, &seqEvent{
				kind:      seqEventNote,
				from:      pA,
				to:        pB,
				notePos:   pos,
				text:      text,
				fragStack: snap,
			})
			steps = append(steps, seqStep{kind: stepNote, evIdx: evIdx})
			continue
		}

		// Message statements.
		if fromID, toID, style, startM, endM, actOp, text, ok := parseSeqMessage(trimmed); ok {
			pFrom := ensurePart(fromID, "", false, false)
			pTo := ensurePart(toID, "", false, false)
			if autoNum {
				if text != "" {
					text = fmt.Sprintf("%d. %s", seqNum, text)
				} else {
					text = fmt.Sprintf("%d.", seqNum)
				}
				seqNum++
			}
			evIdx := len(events)
			snap := append([]int(nil), fStack...)
			events = append(events, &seqEvent{
				kind:      seqEventMsg,
				from:      pFrom,
				to:        pTo,
				style:     style,
				startM:    startM,
				endM:      endM,
				text:      text,
				fragStack: snap,
			})
			steps = append(steps, seqStep{kind: stepMsg, evIdx: evIdx, actOp: actOp})
			continue
		}

		return Diagram{}, fmt.Errorf("line %d: unsupported sequence statement %q", lineNo+2, trimmed)
	}

	if len(fStack) > 0 {
		open := frags[fStack[len(fStack)-1]]
		return Diagram{}, fmt.Errorf("unclosed %s block in sequence diagram", open.kind)
	}
	if len(parts) == 0 {
		return Diagram{}, fmt.Errorf("sequence diagram has no participants")
	}

	// Measure participants.
	maxHeaderH := minPartH
	for _, p := range parts {
		p.lw, p.lh = measure(p.label, partFontSize, true)
		p.w = max(minPartW, p.lw+28)
		if p.actor {
			p.h = actorIconH + 4 + p.lh + 4
		} else {
			p.h = max(minPartH, p.lh+16)
		}
		maxHeaderH = max(maxHeaderH, p.h)
	}

	// Measure events & fragment headers.
	for _, ev := range events {
		if ev.kind == seqEventMsg {
			if ev.text != "" {
				ev.lw, ev.lh = measure(ev.text, msgFontSize, false)
			}
		} else {
			ev.lw, ev.lh = measure(ev.text, noteFontSize, false)
			ev.boxW = max(72, ev.lw+20)
			ev.boxH = max(28, ev.lh+14)
		}
	}
	for _, f := range frags {
		kw := strings.ToLower(f.kind)
		if kw != "rect" {
			kwW, kwH := measure(kw, fragFontSize, true)
			f.kwW = max(40, kwW+18)
			f.kwH = max(20, kwH+6)
		}
		if f.title != "" {
			t := "[" + f.title + "]"
			f.titleW, f.titleH = measure(t, fragFontSize, true)
		}
	}

	// Compute horizontal center X coordinates for each participant.
	nParts := len(parts)
	gaps := make([]float32, max(0, nParts-1))
	for i := 0; i < nParts-1; i++ {
		gaps[i] = parts[i].w/2 + 44 + parts[i+1].w/2
	}

	ensureSpan := func(l, r int, reqSpan float32) {
		if l >= r {
			return
		}
		cur := float32(0)
		for i := l; i < r; i++ {
			cur += gaps[i]
		}
		if cur < reqSpan {
			extra := (reqSpan - cur) / float32(r-l)
			for i := l; i < r; i++ {
				gaps[i] += extra
			}
		}
	}

	for _, ev := range events {
		switch ev.kind {
		case seqEventMsg:
			if ev.from == ev.to {
				if ev.from+1 < nParts {
					// Keep room for self-loop and label before next lifeline.
					ensureSpan(ev.from, ev.from+1, 44+ev.lw+28)
				}
				continue
			}
			l, r := min(ev.from, ev.to), max(ev.from, ev.to)
			ensureSpan(l, r, ev.lw+56)

		case seqEventNote:
			switch ev.notePos {
			case seqNoteLeft:
				if ev.from > 0 {
					ensureSpan(ev.from-1, ev.from, ev.boxW+36)
				}
			case seqNoteRight:
				if ev.from+1 < nParts {
					ensureSpan(ev.from, ev.from+1, ev.boxW+36)
				}
			case seqNoteOver:
				if ev.from != ev.to {
					ensureSpan(ev.from, ev.to, ev.boxW-24)
				}
			}
		}
	}

	parts[0].x = 0
	for i := 1; i < nParts; i++ {
		parts[i].x = parts[i-1].x + gaps[i-1]
	}

	// Assign horizontal bounds to each event.
	for _, ev := range events {
		switch ev.kind {
		case seqEventMsg:
			if ev.from == ev.to {
				cx := parts[ev.from].x
				ev.minX = cx - 8
				ev.maxX = cx + 38 + ev.lw + 12
			} else {
				x1, x2 := parts[ev.from].x, parts[ev.to].x
				ev.minX = min(x1, x2) - 16
				ev.maxX = max(x1, x2) + 16
			}
		case seqEventNote:
			switch ev.notePos {
			case seqNoteLeft:
				cx := parts[ev.from].x
				ev.maxX = cx - 14
				ev.minX = ev.maxX - ev.boxW
			case seqNoteRight:
				cx := parts[ev.from].x
				ev.minX = cx + 14
				ev.maxX = ev.minX + ev.boxW
			case seqNoteOver:
				if ev.from == ev.to {
					cx := parts[ev.from].x
					ev.minX = cx - ev.boxW/2
					ev.maxX = cx + ev.boxW/2
				} else {
					lx, rx := parts[ev.from].x, parts[ev.to].x
					span := rx - lx
					pad := max(float32(28), (ev.boxW-span)/2)
					ev.minX = lx - pad
					ev.maxX = rx + pad
					ev.boxW = ev.maxX - ev.minX
				}
			}
		}
	}

	// Compute vertical progression, fragment boxes, and activation intervals.
	curY := maxHeaderH + 26
	var (
		acts       []*seqActivation
		actStacks  = make([][]int, nParts)
		hasLastMsg bool
		lastMsgY   float32
		lastFrom   int
		lastTo     int
	)

	openAct := func(p int, y float32) {
		st := actStacks[p]
		idx := len(acts)
		acts = append(acts, &seqActivation{
			part:   p,
			depth:  len(st),
			startY: y,
		})
		actStacks[p] = append(st, idx)
	}
	closeAct := func(p int, y float32) bool {
		st := actStacks[p]
		if len(st) == 0 {
			return false
		}
		idx := st[len(st)-1]
		actStacks[p] = st[:len(st)-1]
		acts[idx].endY = max(acts[idx].startY+16, y)
		acts[idx].closed = true
		return true
	}

	expandFrag := func(fIdx int, minX, maxX float32) {
		for cur := fIdx; cur >= 0; {
			f := frags[cur]
			if !f.hasSpan {
				f.minX, f.maxX, f.hasSpan = minX, maxX, true
			} else {
				f.minX = min(f.minX, minX)
				f.maxX = max(f.maxX, maxX)
			}
			cur = f.parent
		}
	}

	for _, st := range steps {
		switch st.kind {
		case stepFragStart:
			f := frags[st.fragIdx]
			f.startY = curY
			barH := max(f.kwH, f.titleH+8)
			if barH == 0 {
				barH = 14
			}
			curY += barH + 14
			hasLastMsg = false

		case stepFragClause:
			f := frags[st.fragIdx]
			divY := curY - 4
			var cw, ch float32
			label := ""
			if st.clause != "" {
				label = "[" + st.clause + "]"
				cw, ch = measure(label, fragFontSize, true)
			}
			f.clauses = append(f.clauses, seqFragClause{
				y:     divY,
				label: label,
				lw:    cw,
				lh:    ch,
			})
			curY += max(float32(18), ch+14)
			hasLastMsg = false

		case stepFragEnd:
			f := frags[st.fragIdx]
			curY += 6
			f.endY = curY
			curY += 16
			hasLastMsg = false

		case stepMsg:
			ev := events[st.evIdx]
			if ev.from == ev.to {
				msgY := curY + 4
				ev.y = msgY
				curY += max(float32(46), ev.lh+22)
				lastMsgY = msgY
			} else {
				msgY := curY + ev.lh + 6
				ev.y = msgY
				curY = msgY + 24
				lastMsgY = msgY
			}
			hasLastMsg = true
			lastFrom, lastTo = ev.from, ev.to

			if st.actOp == '+' {
				openAct(ev.to, ev.y)
			} else if st.actOp == '-' {
				// Official Mermaid: "-" on B-->>-A closes B (from), falling back to to.
				if !closeAct(ev.from, ev.y) {
					closeAct(ev.to, ev.y)
				}
			}

			for _, fIdx := range ev.fragStack {
				expandFrag(fIdx, ev.minX, ev.maxX)
			}

		case stepNote:
			ev := events[st.evIdx]
			ev.y = curY
			curY += ev.boxH + 14
			hasLastMsg = false
			for _, fIdx := range ev.fragStack {
				expandFrag(fIdx, ev.minX, ev.maxX)
			}

		case stepAct:
			y := curY - 8
			if hasLastMsg && (st.part == lastTo || st.part == lastFrom) {
				y = lastMsgY
			}
			openAct(st.part, y)

		case stepDeact:
			y := curY - 10
			if hasLastMsg && (st.part == lastFrom || st.part == lastTo) {
				y = lastMsgY
			}
			closeAct(st.part, y)
		}
	}

	lifelineBot := curY + 14
	for _, a := range acts {
		if !a.closed {
			a.endY = max(a.startY+18, lifelineBot-6)
			a.closed = true
		}
	}

	// Finalize fragment horizontal bounds (expand parent frames outside children).
	for i := len(frags) - 1; i >= 0; i-- {
		f := frags[i]
		if !f.hasSpan {
			f.minX = parts[0].x - parts[0].w/2
			f.maxX = parts[nParts-1].x + parts[nParts-1].w/2
			f.hasSpan = true
		}
		pad := float32(16)
		f.minX -= pad
		f.maxX += pad
		hdrW := f.kwW + 10 + f.titleW + 24
		for _, cl := range f.clauses {
			hdrW = max(hdrW, cl.lw+28)
		}
		if f.maxX-f.minX < hdrW {
			f.maxX = f.minX + hdrW
		}
		if f.parent >= 0 {
			p := frags[f.parent]
			if !p.hasSpan {
				p.minX, p.maxX, p.hasSpan = f.minX-6, f.maxX+6, true
			} else {
				p.minX = min(p.minX, f.minX-6)
				p.maxX = max(p.maxX, f.maxX+6)
			}
		}
	}

	// Helper returning left/right edge X of participant lifeline at given Y
	// taking active activation bars into account.
	edgeAtY := func(pIdx int, y float32, dirRight bool) float32 {
		cx := parts[pIdx].x
		bestOffset := float32(0)
		found := false
		for _, a := range acts {
			if a.part != pIdx {
				continue
			}
			if y >= a.startY-0.5 && y <= a.endY+0.5 {
				barLeft := cx - actWidth/2 + float32(a.depth)*4
				barRight := barLeft + actWidth
				if dirRight {
					if !found || barRight-cx > bestOffset {
						bestOffset = barRight - cx
						found = true
					}
				} else {
					if !found || cx-barLeft > bestOffset {
						bestOffset = cx - barLeft
						found = true
					}
				}
			}
		}
		if !found {
			return cx
		}
		if dirRight {
			return cx + bestOffset
		}
		return cx - bestOffset
	}

	var diag Diagram
	diag.Kind = "sequence"

	// 1. Fragment background boxes & dividers (outer to inner order).
	for _, f := range frags {
		diag.Shapes = append(diag.Shapes, ShapeOp{
			Kind:   ShapeRect,
			Role:   RoleFragment,
			X:      f.minX,
			Y:      f.startY,
			W:      f.maxX - f.minX,
			H:      f.endY - f.startY,
			Radius: 4,
			Dashed: f.kind == "box",
		})
		if f.kwW > 0 {
			diag.Shapes = append(diag.Shapes, ShapeOp{
				Kind: ShapePentagon,
				Role: RoleFragment,
				X:    f.minX,
				Y:    f.startY,
				W:    f.kwW,
				H:    f.kwH,
			})
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  strings.ToLower(f.kind),
				X:     f.minX,
				Y:     f.startY,
				W:     f.kwW - 4,
				H:     f.kwH,
				Size:  fragFontSize,
				Bold:  true,
				Align: 0,
				Role:  RoleFragment,
			})
		}
		if f.title != "" {
			tx := f.minX + f.kwW + 8
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  "[" + f.title + "]",
				X:     tx,
				Y:     f.startY + 2,
				W:     f.titleW + 8,
				H:     max(f.kwH-2, f.titleH),
				Size:  fragFontSize,
				Bold:  true,
				Align: -1,
				Role:  RoleFragment,
			})
		}
		for _, cl := range f.clauses {
			diag.Paths = append(diag.Paths, PathOp{
				Points: [][2]float32{{f.minX, cl.y}, {f.maxX, cl.y}},
				Style:  EdgeDashed,
				Role:   RoleFragment,
			})
			if cl.label != "" {
				diag.Labels = append(diag.Labels, LabelOp{
					Text:  cl.label,
					X:     f.minX + 10,
					Y:     cl.y + 4,
					W:     cl.lw + 6,
					H:     cl.lh + 2,
					Size:  fragFontSize,
					Bold:  true,
					Align: -1,
					Role:  RoleFragment,
				})
			}
		}
	}

	// 2. Top participants & vertical lifelines.
	emitHeader := func(topY float32, p *seqParticipant) (botY float32) {
		if p.actor {
			iconW := float32(30)
			y0 := topY + (maxHeaderH-p.h)/2
			diag.Shapes = append(diag.Shapes, ShapeOp{
				Kind: ShapeActor,
				Role: RoleActor,
				X:    p.x - iconW/2,
				Y:    y0,
				W:    iconW,
				H:    actorIconH,
			})
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  p.label,
				X:     p.x - p.w/2,
				Y:     y0 + actorIconH + 4,
				W:     p.w,
				H:     p.lh + 2,
				Size:  partFontSize,
				Bold:  true,
				Align: 0,
				Role:  RoleActor,
			})
			return y0 + p.h
		}
		y0 := topY + (maxHeaderH-p.h)/2
		diag.Shapes = append(diag.Shapes, ShapeOp{
			Kind:   ShapeRound,
			Role:   RoleActor,
			X:      p.x - p.w/2,
			Y:      y0,
			W:      p.w,
			H:      p.h,
			Radius: 6,
		})
		diag.Labels = append(diag.Labels, LabelOp{
			Text:  p.label,
			X:     p.x - p.w/2,
			Y:     y0,
			W:     p.w,
			H:     p.h,
			Size:  partFontSize,
			Bold:  true,
			Align: 0,
			Role:  RoleActor,
		})
		return y0 + p.h
	}

	for _, p := range parts {
		startY := emitHeader(0, p)
		botHeaderY := lifelineBot + (maxHeaderH-p.h)/2
		diag.Paths = append(diag.Paths, PathOp{
			Points: [][2]float32{{p.x, startY}, {p.x, botHeaderY}},
			Style:  EdgeDashed,
			Role:   RoleEdge,
		})
		emitHeader(lifelineBot, p)
	}

	// 3. Activation bars.
	for _, a := range acts {
		cx := parts[a.part].x
		diag.Shapes = append(diag.Shapes, ShapeOp{
			Kind:   ShapeRect,
			Role:   RoleActivation,
			X:      cx - actWidth/2 + float32(a.depth)*4,
			Y:      a.startY,
			W:      actWidth,
			H:      max(float32(14), a.endY-a.startY),
			Radius: 2,
		})
	}

	// 4. Messages & notes.
	for _, ev := range events {
		switch ev.kind {
		case seqEventMsg:
			if ev.from == ev.to {
				sx := edgeAtY(ev.from, ev.y, true)
				ex := edgeAtY(ev.from, ev.y+26, true)
				diag.Paths = append(diag.Paths, PathOp{
					Points: [][2]float32{
						{sx, ev.y},
						{sx + 34, ev.y},
						{sx + 34, ev.y + 26},
						{ex, ev.y + 26},
					},
					Style:     ev.style,
					EndMarker: ev.endM,
					Role:      RoleEdge,
				})
				if ev.text != "" {
					diag.Labels = append(diag.Labels, LabelOp{
						Text:  ev.text,
						X:     sx + 40,
						Y:     ev.y + 13 - ev.lh/2,
						W:     ev.lw + 4,
						H:     ev.lh,
						Size:  msgFontSize,
						Align: -1,
						Role:  RoleEdgeLabel,
						Pill:  true,
					})
				}
				continue
			}

			dirRight := parts[ev.to].x > parts[ev.from].x
			x1 := edgeAtY(ev.from, ev.y, dirRight)
			x2 := edgeAtY(ev.to, ev.y, !dirRight)
			diag.Paths = append(diag.Paths, PathOp{
				Points:      [][2]float32{{x1, ev.y}, {x2, ev.y}},
				Style:       ev.style,
				StartMarker: ev.startM,
				EndMarker:   ev.endM,
				Role:        RoleEdge,
			})
			if ev.text != "" {
				mx := (x1 + x2) / 2
				diag.Labels = append(diag.Labels, LabelOp{
					Text:  ev.text,
					X:     mx - ev.lw/2 - 2,
					Y:     ev.y - ev.lh - 5,
					W:     ev.lw + 4,
					H:     ev.lh,
					Size:  msgFontSize,
					Align: 0,
					Role:  RoleEdgeLabel,
					Pill:  true,
				})
			}

		case seqEventNote:
			diag.Shapes = append(diag.Shapes, ShapeOp{
				Kind:   ShapeRect,
				Role:   RoleNote,
				X:      ev.minX,
				Y:      ev.y,
				W:      ev.boxW,
				H:      ev.boxH,
				Radius: 4,
			})
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  ev.text,
				X:     ev.minX + 6,
				Y:     ev.y + 4,
				W:     ev.boxW - 12,
				H:     ev.boxH - 8,
				Size:  noteFontSize,
				Align: 0,
				Role:  RoleNote,
			})
		}
	}

	// Normalize coordinates to include outerPad around all content.
	minX, minY := float32(0), float32(0)
	maxX, maxY := float32(0), float32(0)
	first := true
	expandBox := func(x, y, w, h float32) {
		if first {
			minX, minY, maxX, maxY = x, y, x+w, y+h
			first = false
			return
		}
		minX = min(minX, x)
		minY = min(minY, y)
		maxX = max(maxX, x+w)
		maxY = max(maxY, y+h)
	}
	for _, s := range diag.Shapes {
		expandBox(s.X, s.Y, s.W, s.H)
	}
	for _, p := range diag.Paths {
		for _, pt := range p.Points {
			expandBox(pt[0], pt[1], 0, 0)
		}
	}
	for _, lb := range diag.Labels {
		expandBox(lb.X, lb.Y, lb.W, lb.H)
	}

	dx := outerPad - minX
	dy := outerPad - minY
	for i := range diag.Shapes {
		diag.Shapes[i].X += dx
		diag.Shapes[i].Y += dy
	}
	for i := range diag.Paths {
		for j := range diag.Paths[i].Points {
			diag.Paths[i].Points[j][0] += dx
			diag.Paths[i].Points[j][1] += dy
		}
	}
	for i := range diag.Labels {
		diag.Labels[i].X += dx
		diag.Labels[i].Y += dy
	}
	diag.W = (maxX - minX) + outerPad*2
	diag.H = (maxY - minY) + outerPad*2
	return diag, nil
}

func parseParticipantDecl(rest string) (id, label string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	// Find " as " case-insensitively outside quotes.
	low := strings.ToLower(rest)
	inQ := byte(0)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inQ != 0 {
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		if (c == ' ' || c == '\t') && i+4 <= len(rest) && low[i+1:i+3] == "as" && (low[i+3] == ' ' || low[i+3] == '\t') {
			id = strings.Trim(strings.TrimSpace(rest[:i]), `"'`)
			label = normalizeLabel(rest[i+4:])
			return id, label
		}
	}
	id = strings.Trim(rest, `"'`)
	return id, id
}

func parseFragStart(trimmed string) (kind, title string, ok bool) {
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", "", false
	}
	kw := strings.ToLower(fields[0])
	switch kw {
	case "loop", "alt", "opt", "par", "critical", "break", "rect", "box":
		rest := strings.TrimSpace(trimmed[len(fields[0]):])
		if kw == "rect" {
			// Drop optional "rgb(...)" / "rgba(...)" / color word so the box renders cleanly.
			lowRest := strings.ToLower(rest)
			if strings.HasPrefix(lowRest, "rgb(") || strings.HasPrefix(lowRest, "rgba(") || strings.HasPrefix(lowRest, "#") {
				rest = ""
			}
		}
		return kw, normalizeLabel(rest), true
	}
	return "", "", false
}

func parseFragClause(trimmed string) (clause string, ok bool) {
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}
	kw := strings.ToLower(fields[0])
	switch kw {
	case "else", "and", "option":
		rest := strings.TrimSpace(trimmed[len(fields[0]):])
		return normalizeLabel(rest), true
	}
	return "", false
}

func parseSeqNote(trimmed string) (pos seqNotePos, idA, idB, text string, ok bool) {
	low := strings.ToLower(trimmed)
	if !strings.HasPrefix(low, "note ") {
		return 0, "", "", "", false
	}
	rest := strings.TrimSpace(trimmed[len("note "):])
	colon := strings.IndexByte(rest, ':')
	if colon < 0 {
		return 0, "", "", "", false
	}
	target := strings.TrimSpace(rest[:colon])
	text = normalizeLabel(rest[colon+1:])
	lowTarget := strings.ToLower(target)

	switch {
	case strings.HasPrefix(lowTarget, "left of "):
		idA = strings.Trim(strings.TrimSpace(target[len("left of "):]), `"'`)
		return seqNoteLeft, idA, "", text, idA != ""
	case strings.HasPrefix(lowTarget, "right of "):
		idA = strings.Trim(strings.TrimSpace(target[len("right of "):]), `"'`)
		return seqNoteRight, idA, "", text, idA != ""
	case strings.HasPrefix(lowTarget, "over "):
		ids := strings.TrimSpace(target[len("over "):])
		if comma := strings.IndexByte(ids, ','); comma >= 0 {
			idA = strings.Trim(strings.TrimSpace(ids[:comma]), `"'`)
			idB = strings.Trim(strings.TrimSpace(ids[comma+1:]), `"'`)
			return seqNoteOver, idA, idB, text, idA != "" && idB != ""
		}
		idA = strings.Trim(ids, `"'`)
		return seqNoteOver, idA, "", text, idA != ""
	}
	return 0, "", "", "", false
}

var seqMsgOps = []struct {
	tok    string
	style  EdgeKind
	startM MarkerKind
	endM   MarkerKind
}{
	{"<<-->>", EdgeDashed, MarkerArrow, MarkerArrow},
	{"<<->>", EdgeSolid, MarkerArrow, MarkerArrow},
	{"-->>", EdgeDashed, MarkerNone, MarkerArrow},
	{"->>", EdgeSolid, MarkerNone, MarkerArrow},
	{"-->", EdgeDashed, MarkerNone, MarkerOpenArrow},
	{"->", EdgeSolid, MarkerNone, MarkerOpenArrow},
	{"--x", EdgeDashed, MarkerNone, MarkerCross},
	{"-x", EdgeSolid, MarkerNone, MarkerCross},
	{"--)", EdgeDashed, MarkerNone, MarkerOpenArrow},
	{"-)", EdgeSolid, MarkerNone, MarkerOpenArrow},
}

func parseSeqMessage(trimmed string) (from, to string, style EdgeKind, startM, endM MarkerKind, actOp byte, text string, ok bool) {
	inQ := byte(0)
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if inQ != 0 {
			if c == '\\' && i+1 < len(trimmed) {
				i++
				continue
			}
			if c == inQ {
				inQ = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQ = c
			continue
		}
		for _, op := range seqMsgOps {
			if !strings.HasPrefix(trimmed[i:], op.tok) {
				continue
			}
			from = strings.Trim(strings.TrimSpace(trimmed[:i]), `"'`)
			after := trimmed[i+len(op.tok):]
			after = strings.TrimLeft(after, " \t")
			if len(after) > 0 && (after[0] == '+' || after[0] == '-') {
				actOp = after[0]
				after = strings.TrimLeft(after[1:], " \t")
			}
			if colon := strings.IndexByte(after, ':'); colon >= 0 {
				to = strings.Trim(strings.TrimSpace(after[:colon]), `"'`)
				text = normalizeLabel(after[colon+1:])
			} else {
				to = strings.Trim(strings.TrimSpace(after), `"'`)
			}
			if from == "" || to == "" {
				return "", "", 0, 0, 0, 0, "", false
			}
			return from, to, op.style, op.startM, op.endM, actOp, text, true
		}
	}
	return "", "", 0, 0, 0, 0, "", false
}
