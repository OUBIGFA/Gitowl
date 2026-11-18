package mermaid

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type flowDir uint8

const (
	dirTD flowDir = iota
	dirBT
	dirLR
	dirRL
)

type flowNode struct {
	id       string
	label    string
	shape    ShapeKind
	subgraph int // -1 if top-level
	rank     int
	order    int
	x, y     float32
	w, h     float32
	labelW   float32
	labelH   float32
}

type flowEdge struct {
	from     int
	to       int
	label    string
	style    EdgeKind
	startM   MarkerKind
	endM     MarkerKind
	reversed bool
	labelW   float32
	labelH   float32
}

type flowSubgraph struct {
	id     string
	title  string
	parent int
	nodes  []int
	depth  int
	x, y   float32
	w, h   float32
}

func layoutFlowchart(lines []string, measure MeasureFunc) (Diagram, error) {
	fields := strings.Fields(lines[0])
	dir := dirTD
	if len(fields) >= 2 {
		switch strings.ToUpper(fields[1]) {
		case "LR":
			dir = dirLR
		case "RL":
			dir = dirRL
		case "BT":
			dir = dirBT
		case "TD", "TB":
			dir = dirTD
		}
	}

	var (
		nodes     []*flowNode
		nodeMap   = map[string]int{}
		edges     []flowEdge
		subgraphs []*flowSubgraph
		sgStack   []int
	)

	ensureNode := func(id, label string, shape ShapeKind, explicit bool) int {
		if idx, ok := nodeMap[id]; ok {
			n := nodes[idx]
			if explicit {
				n.label = label
				n.shape = shape
			}
			if n.subgraph < 0 && len(sgStack) > 0 {
				sg := sgStack[len(sgStack)-1]
				n.subgraph = sg
				subgraphs[sg].nodes = append(subgraphs[sg].nodes, idx)
			}
			return idx
		}
		sg := -1
		if len(sgStack) > 0 {
			sg = sgStack[len(sgStack)-1]
		}
		if label == "" {
			label = id
		}
		idx := len(nodes)
		nodes = append(nodes, &flowNode{
			id:       id,
			label:    label,
			shape:    shape,
			subgraph: sg,
		})
		nodeMap[id] = idx
		if sg >= 0 {
			subgraphs[sg].nodes = append(subgraphs[sg].nodes, idx)
		}
		return idx
	}

	for _, raw := range lines[1:] {
		low := strings.ToLower(raw)
		switch {
		case strings.HasPrefix(low, "subgraph ") || low == "subgraph":
			id, title := parseSubgraphHeader(strings.TrimSpace(raw[len("subgraph"):]), len(subgraphs))
			parent := -1
			depth := 0
			if len(sgStack) > 0 {
				parent = sgStack[len(sgStack)-1]
				depth = subgraphs[parent].depth + 1
			}
			sgIdx := len(subgraphs)
			subgraphs = append(subgraphs, &flowSubgraph{id: id, title: title, parent: parent, depth: depth})
			sgStack = append(sgStack, sgIdx)
			continue
		case low == "end":
			if len(sgStack) > 0 {
				sgStack = sgStack[:len(sgStack)-1]
			}
			continue
		case strings.HasPrefix(low, "direction ") ||
			strings.HasPrefix(low, "classdef ") ||
			strings.HasPrefix(low, "class ") ||
			strings.HasPrefix(low, "style ") ||
			strings.HasPrefix(low, "linkstyle ") ||
			strings.HasPrefix(low, "click "):
			continue
		}

		// Parse chained nodes & edges on this line (e.g. "A[Start] --> B & C").
		segs, ops := splitFlowChain(raw)
		if len(segs) == 0 {
			continue
		}
		var prevGroup []int
		for si, seg := range segs {
			parts := splitAmpersand(seg)
			var curGroup []int
			for _, part := range parts {
				id, label, shape, explicit, ok := parseFlowNodeExpr(part)
				if !ok {
					continue
				}
				curGroup = append(curGroup, ensureNode(id, label, shape, explicit))
			}
			if si > 0 && si-1 < len(ops) {
				op := ops[si-1]
				for _, u := range prevGroup {
					for _, v := range curGroup {
						edges = append(edges, flowEdge{
							from:   u,
							to:     v,
							label:  op.label,
							style:  op.style,
							startM: op.startM,
							endM:   op.endM,
						})
					}
				}
			}
			prevGroup = curGroup
		}
	}

	if len(nodes) == 0 {
		return Diagram{}, fmt.Errorf("flowchart has no nodes")
	}

	// Measure nodes & edge labels.
	const (
		nodeFontSize float32 = 13
		edgeFontSize float32 = 11.5
		sgFontSize   float32 = 12
	)
	for _, n := range nodes {
		lw, lh := measure(n.label, nodeFontSize, false)
		n.labelW, n.labelH = lw, lh
		padX := float32(28)
		padY := float32(18)
		switch n.shape {
		case ShapeDiamond:
			padX, padY = lw*0.5+28, lh*0.6+24
		case ShapeHexagon:
			padX, padY = 40, 20
		case ShapeCircle:
			side := max(lw+22, lh+22)
			n.w, n.h = max(40, side), max(40, side)
			continue
		case ShapeCylinder:
			padX, padY = 28, 28
		case ShapeParallelogram:
			padX, padY = 36, 18
		case ShapeStadium:
			padX, padY = 32, 18
		case ShapeSubroutine:
			padX, padY = 36, 18
		}
		n.w = max(56, lw+padX)
		n.h = max(34, lh+padY)
	}
	for i := range edges {
		if edges[i].label != "" {
			lw, lh := measure(edges[i].label, edgeFontSize, false)
			edges[i].labelW, edges[i].labelH = lw+12, lh+6
		}
	}

	// Cycle removal via DFS so rank assignment always terminates on cyclic graphs.
	adj := make([][]int, len(nodes))
	for i, e := range edges {
		if e.from != e.to {
			adj[e.from] = append(adj[e.from], i)
		}
	}
	visited := make([]uint8, len(nodes)) // 0=unvisited, 1=visiting, 2=done
	var dfs func(int)
	dfs = func(u int) {
		visited[u] = 1
		for _, ei := range adj[u] {
			v := edges[ei].to
			if visited[v] == 1 {
				edges[ei].reversed = true
			} else if visited[v] == 0 {
				dfs(v)
			}
		}
		visited[u] = 2
	}
	for u := range nodes {
		if visited[u] == 0 {
			dfs(u)
		}
	}

	// Longest-path rank assignment on DAG.
	dagOut := make([][]int, len(nodes))
	dagIn := make([]int, len(nodes))
	for _, e := range edges {
		if e.from == e.to {
			continue
		}
		u, v := e.from, e.to
		if e.reversed {
			u, v = v, u
		}
		dagOut[u] = append(dagOut[u], v)
		dagIn[v]++
	}
	var q []int
	for u := range nodes {
		if dagIn[u] == 0 {
			q = append(q, u)
		}
	}
	maxRank := 0
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		for _, v := range dagOut[u] {
			if nodes[v].rank < nodes[u].rank+1 {
				nodes[v].rank = nodes[u].rank + 1
				maxRank = max(maxRank, nodes[v].rank)
			}
			dagIn[v]--
			if dagIn[v] == 0 {
				q = append(q, v)
			}
		}
	}

	ranks := make([][]int, maxRank+1)
	for i, n := range nodes {
		ranks[n.rank] = append(ranks[n.rank], i)
	}
	for r := range ranks {
		for ord, idx := range ranks[r] {
			nodes[idx].order = ord
		}
	}

	// Build undirected-per-adjacent-rank neighbor lists for barycenter sweeps.
	preds := make([][]int, len(nodes))
	succs := make([][]int, len(nodes))
	for _, e := range edges {
		if e.from == e.to {
			continue
		}
		u, v := e.from, e.to
		if e.reversed {
			u, v = v, u
		}
		succs[u] = append(succs[u], v)
		preds[v] = append(preds[v], u)
	}
	for sweep := 0; sweep < 4; sweep++ {
		if sweep%2 == 0 {
			for r := 1; r <= maxRank; r++ {
				sortRankByBarycenter(ranks[r], preds, nodes)
			}
		} else {
			for r := maxRank - 1; r >= 0; r-- {
				sortRankByBarycenter(ranks[r], succs, nodes)
			}
		}
	}

	// Coordinate assignment.
	horizontal := dir == dirLR || dir == dirRL
	reversedAxis := dir == dirBT || dir == dirRL
	const (
		rankGapBase float32 = 44
		crossGap    float32 = 28
		sgGapExtra  float32 = 20
		outerPad    float32 = 16
	)

	// Extra gap between rank r and r+1 if an edge between them has a label.
	rankGap := make([]float32, maxRank+1)
	for r := 0; r < maxRank; r++ {
		rankGap[r] = rankGapBase
	}
	for _, e := range edges {
		if e.label == "" || e.from == e.to {
			continue
		}
		r1, r2 := nodes[e.from].rank, nodes[e.to].rank
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		if r2 == r1+1 {
			need := e.labelH + 20
			if horizontal {
				need = e.labelW + 24
			}
			rankGap[r1] = max(rankGap[r1], need)
		}
	}

	rankThick := make([]float32, maxRank+1)
	rankCenter := make([]float32, maxRank+1)
	crossSpan := make([]float32, maxRank+1)
	maxCross := float32(0)

	for r := 0; r <= maxRank; r++ {
		row := ranks[r]
		totalCross := float32(0)
		for idxPos, ni := range row {
			n := nodes[ni]
			thick, span := n.h, n.w
			if horizontal {
				thick, span = n.w, n.h
			}
			rankThick[r] = max(rankThick[r], thick)
			if idxPos > 0 {
				totalCross += crossGap
				if nodes[row[idxPos-1]].subgraph != n.subgraph {
					totalCross += sgGapExtra
				}
			}
			totalCross += span
		}
		crossSpan[r] = totalCross
		maxCross = max(maxCross, totalCross)
	}

	curPrimary := outerPad
	for r := 0; r <= maxRank; r++ {
		rankCenter[r] = curPrimary + rankThick[r]/2
		curPrimary += rankThick[r]
		if r < maxRank {
			curPrimary += rankGap[r]
		}
	}
	totalPrimary := curPrimary + outerPad

	if reversedAxis {
		for r := 0; r <= maxRank; r++ {
			rankCenter[r] = totalPrimary - rankCenter[r]
		}
	}

	for r := 0; r <= maxRank; r++ {
		row := ranks[r]
		curCross := outerPad + (maxCross-crossSpan[r])/2
		for idxPos, ni := range row {
			n := nodes[ni]
			span := n.w
			if horizontal {
				span = n.h
			}
			if idxPos > 0 {
				curCross += crossGap
				if nodes[row[idxPos-1]].subgraph != n.subgraph {
					curCross += sgGapExtra
				}
			}
			cc := curCross + span/2
			if horizontal {
				n.x = rankCenter[r] - n.w/2
				n.y = cc - n.h/2
			} else {
				n.x = cc - n.w/2
				n.y = rankCenter[r] - n.h/2
			}
			curCross += span
		}
	}

	// Compute subgraph bounding boxes bottom-up (deepest first).
	var diag Diagram
	diag.Kind = "flowchart"

	if len(subgraphs) > 0 {
		sgOrder := make([]int, len(subgraphs))
		for i := range subgraphs {
			sgOrder[i] = i
		}
		slices.SortFunc(sgOrder, func(a, b int) int { return subgraphs[b].depth - subgraphs[a].depth })
		for _, si := range sgOrder {
			sg := subgraphs[si]
			minX, minY := float32(1e9), float32(1e9)
			maxX, maxY := float32(-1e9), float32(-1e9)
			hasAny := false
			for _, ni := range sg.nodes {
				n := nodes[ni]
				minX = min(minX, n.x)
				minY = min(minY, n.y)
				maxX = max(maxX, n.x+n.w)
				maxY = max(maxY, n.y+n.h)
				hasAny = true
			}
			for _, child := range subgraphs {
				if child.parent == si && child.w > 0 {
					minX = min(minX, child.x)
					minY = min(minY, child.y)
					maxX = max(maxX, child.x+child.w)
					maxY = max(maxY, child.y+child.h)
					hasAny = true
				}
			}
			if !hasAny {
				continue
			}
			tw, th := measure(sg.title, sgFontSize, true)
			pad := float32(16)
			topPad := max(26, th+12)
			sg.x = minX - pad
			sg.y = minY - topPad
			sg.w = max(maxX-minX+2*pad, tw+2*pad)
			sg.h = maxY - minY + topPad + pad
		}
		// Shift everything if any subgraph extended into negative coordinates.
		shiftX, shiftY := float32(0), float32(0)
		for _, sg := range subgraphs {
			if sg.w <= 0 {
				continue
			}
			if sg.x < outerPad {
				shiftX = max(shiftX, outerPad-sg.x)
			}
			if sg.y < outerPad {
				shiftY = max(shiftY, outerPad-sg.y)
			}
		}
		if shiftX > 0 || shiftY > 0 {
			for _, sg := range subgraphs {
				if sg.w > 0 {
					sg.x += shiftX
					sg.y += shiftY
				}
			}
			for _, n := range nodes {
				n.x += shiftX
				n.y += shiftY
			}
		}
		// Emit shallowest subgraphs first so nested boxes paint on top of outer ones.
		slices.SortFunc(sgOrder, func(a, b int) int { return subgraphs[a].depth - subgraphs[b].depth })
		for _, si := range sgOrder {
			sg := subgraphs[si]
			if sg.w <= 0 {
				continue
			}
			tw, th := measure(sg.title, sgFontSize, true)
			diag.Shapes = append(diag.Shapes, ShapeOp{
				Kind:   ShapeRound,
				Role:   RoleSubgraph,
				X:      sg.x,
				Y:      sg.y,
				W:      sg.w,
				H:      sg.h,
				Radius: 10,
			})
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  sg.title,
				X:     sg.x + 12,
				Y:     sg.y + 8,
				W:     tw,
				H:     th,
				Size:  sgFontSize,
				Bold:  true,
				Align: -1,
				Role:  RoleSubgraph,
			})
		}
	}

	// Route edges before emitting node labels (paths paint behind nodes or between borders).
	pairCount := map[[2]int]int{}
	for _, e := range edges {
		a, b := min(e.from, e.to), max(e.from, e.to)
		pairCount[[2]int{a, b}]++
	}
	pairSeen := map[[2]int]int{}
	for _, e := range edges {
		u, v := nodes[e.from], nodes[e.to]
		key := [2]int{min(e.from, e.to), max(e.from, e.to)}
		idx := pairSeen[key]
		pairSeen[key] = idx + 1
		total := pairCount[key]
		spread := (float32(idx) - float32(total-1)/2) * 16

		pts := routeFlowEdge(u, v, horizontal, spread, nodes)
		diag.Paths = append(diag.Paths, PathOp{
			Points:      pts,
			Smooth:      true,
			Style:       e.style,
			StartMarker: e.startM,
			EndMarker:   e.endM,
			Role:        RoleEdge,
		})
		if e.label != "" {
			mx, my := polylineMidpoint(pts)
			diag.Labels = append(diag.Labels, LabelOp{
				Text:  e.label,
				X:     mx - e.labelW/2,
				Y:     my - e.labelH/2,
				W:     e.labelW,
				H:     e.labelH,
				Size:  edgeFontSize,
				Align: 0,
				Role:  RoleEdgeLabel,
				Pill:  true,
			})
		}
	}

	// Emit nodes and compute final diagram bounds.
	maxW, maxH := float32(0), float32(0)
	for _, n := range nodes {
		diag.Shapes = append(diag.Shapes, ShapeOp{
			Kind:   n.shape,
			Role:   RoleNode,
			X:      n.x,
			Y:      n.y,
			W:      n.w,
			H:      n.h,
			Radius: 6,
		})
		diag.Labels = append(diag.Labels, LabelOp{
			Text:  n.label,
			X:     n.x + (n.w-n.labelW)/2,
			Y:     n.y + (n.h-n.labelH)/2,
			W:     n.labelW,
			H:     n.labelH,
			Size:  nodeFontSize,
			Align: 0,
			Role:  RoleNode,
		})
		maxW = max(maxW, n.x+n.w+outerPad)
		maxH = max(maxH, n.y+n.h+outerPad)
	}
	for _, sg := range subgraphs {
		if sg.w > 0 {
			maxW = max(maxW, sg.x+sg.w+outerPad)
			maxH = max(maxH, sg.y+sg.h+outerPad)
		}
	}
	for _, p := range diag.Paths {
		for _, pt := range p.Points {
			maxW = max(maxW, pt[0]+outerPad)
			maxH = max(maxH, pt[1]+outerPad)
		}
	}
	for _, lb := range diag.Labels {
		maxW = max(maxW, lb.X+lb.W+outerPad)
		maxH = max(maxH, lb.Y+lb.H+outerPad)
	}
	diag.W, diag.H = maxW, maxH
	return diag, nil
}

func sortRankByBarycenter(row []int, neighbors [][]int, nodes []*flowNode) {
	if len(row) <= 1 {
		return
	}
	bary := make(map[int]float64, len(row))
	for _, ni := range row {
		ns := neighbors[ni]
		if len(ns) == 0 {
			bary[ni] = float64(nodes[ni].order)
			continue
		}
		sum := 0.0
		for _, nb := range ns {
			sum += float64(nodes[nb].order)
		}
		bary[ni] = sum / float64(len(ns))
	}
	slices.SortStableFunc(row, func(a, b int) int {
		if bary[a] < bary[b] {
			return -1
		}
		if bary[a] > bary[b] {
			return 1
		}
		return nodes[a].order - nodes[b].order
	})
	for ord, ni := range row {
		nodes[ni].order = ord
	}
}

func routeFlowEdge(u, v *flowNode, horizontal bool, spread float32, nodes []*flowNode) [][2]float32 {
	if u == v {
		if horizontal {
			cx := u.x + u.w/2
			cy := u.y
			return [][2]float32{{cx - 8, cy}, {cx - 22, cy - 28}, {cx + 22, cy - 28}, {cx + 8, cy}}
		}
		cx := u.x + u.w
		cy := u.y + u.h/2
		return [][2]float32{{cx, cy - 8}, {cx + 30, cy - 22}, {cx + 30, cy + 22}, {cx, cy + 8}}
	}
	ucx, ucy := u.x+u.w/2, u.y+u.h/2
	vcx, vcy := v.x+v.w/2, v.y+v.h/2

	var sx, sy, tx, ty float32
	if horizontal {
		if vcx >= ucx {
			sx, sy = u.x+u.w, ucy+spread*0.4
			tx, ty = v.x, vcy+spread*0.4
		} else {
			sx, sy = u.x, ucy+spread*0.4
			tx, ty = v.x+v.w, vcy+spread*0.4
		}
		rankDelta := u.rank - v.rank
		if rankDelta < 0 {
			rankDelta = -rankDelta
		}
		if rankDelta == 0 || rankDelta > 1 {
			if vcx < ucx {
				// Route back-edges slightly above/below intermediate nodes.
				midY := min(u.y, v.y) - 24 - abs32(spread)
				if midY < 8 {
					midY = max(u.y+u.h, v.y+v.h) + 24 + abs32(spread)
				}
				return [][2]float32{{ucx, u.y}, {ucx, midY}, {vcx, midY}, {vcx, v.y}}
			}
		}
		mx := (sx + tx) / 2
		if abs32(sy-ty) < 1 && spread == 0 {
			return [][2]float32{{sx, sy}, {tx, ty}}
		}
		return [][2]float32{{sx, sy}, {mx, sy}, {mx, ty}, {tx, ty}}
	}

	if vcy >= ucy {
		sx, sy = ucx+spread*0.4, u.y+u.h
		tx, ty = vcx+spread*0.4, v.y
	} else {
		sx, sy = ucx+spread*0.4, u.y
		tx, ty = vcx+spread*0.4, v.y+v.h
		// Back-edge detour to the right of the nodes.
		sideX := max(u.x+u.w, v.x+v.w) + 28 + abs32(spread)
		return [][2]float32{{u.x + u.w, ucy}, {sideX, ucy}, {sideX, vcy}, {v.x + v.w, vcy}}
	}
	my := (sy + ty) / 2
	if abs32(sx-tx) < 1 && spread == 0 {
		return [][2]float32{{sx, sy}, {tx, ty}}
	}
	return [][2]float32{{sx, sy}, {sx, my}, {tx, my}, {tx, ty}}
}

func polylineMidpoint(pts [][2]float32) (float32, float32) {
	if len(pts) == 0 {
		return 0, 0
	}
	if len(pts) == 1 {
		return pts[0][0], pts[0][1]
	}
	total := 0.0
	segs := make([]float64, len(pts)-1)
	for i := 0; i < len(pts)-1; i++ {
		dx := float64(pts[i+1][0] - pts[i][0])
		dy := float64(pts[i+1][1] - pts[i][1])
		d := math.Hypot(dx, dy)
		segs[i] = d
		total += d
	}
	target := total * 0.5
	acc := 0.0
	for i, d := range segs {
		if acc+d >= target && d > 0 {
			t := float32((target - acc) / d)
			return pts[i][0] + (pts[i+1][0]-pts[i][0])*t, pts[i][1] + (pts[i+1][1]-pts[i][1])*t
		}
		acc += d
	}
	last := pts[len(pts)-1]
	return last[0], last[1]
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func parseSubgraphHeader(rest string, idx int) (id, title string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return fmt.Sprintf("sg_%d", idx), fmt.Sprintf("Group %d", idx+1)
	}
	if br := strings.IndexByte(rest, '['); br >= 0 && strings.HasSuffix(rest, "]") {
		id = strings.TrimSpace(rest[:br])
		title = normalizeLabel(rest[br+1 : len(rest)-1])
		if id == "" {
			id = fmt.Sprintf("sg_%d", idx)
		}
		return id, title
	}
	if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
		first := rest[:sp]
		second := strings.TrimSpace(rest[sp+1:])
		if strings.HasPrefix(second, "\"") || strings.Contains(rest, "[") {
			return first, normalizeLabel(second)
		}
	}
	return rest, normalizeLabel(rest)
}

type flowLinkOp struct {
	style  EdgeKind
	startM MarkerKind
	endM   MarkerKind
	label  string
}

func splitFlowChain(line string) ([]string, []flowLinkOp) {
	var segs []string
	var ops []flowLinkOp
	start := 0
	inQuote := byte(0)
	depth := 0
	n := len(line)
	for i := 0; i < n; {
		c := line[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < n {
				i += 2
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			i++
			continue
		}
		if c == '[' || c == '(' || c == '{' {
			depth++
			i++
			continue
		}
		if (c == ']' || c == ')' || c == '}') && depth > 0 {
			depth--
			i++
			continue
		}
		if depth == 0 {
			if op, opLen, ok := matchFlowEdge(line[i:]); ok {
				segs = append(segs, strings.TrimSpace(line[start:i]))
				ops = append(ops, op)
				i += opLen
				start = i
				continue
			}
		}
		i++
	}
	tail := strings.TrimSpace(line[start:])
	if tail != "" || len(segs) > 0 {
		segs = append(segs, tail)
	}
	return segs, ops
}

var flowEdgePrefixes = []struct {
	tok    string
	style  EdgeKind
	startM MarkerKind
	endM   MarkerKind
}{
	{"<-.->", EdgeDashed, MarkerArrow, MarkerArrow},
	{"<-->", EdgeSolid, MarkerArrow, MarkerArrow},
	{"<==>", EdgeThick, MarkerArrow, MarkerArrow},
	{"-.->", EdgeDashed, MarkerNone, MarkerArrow},
	{"-.-", EdgeDashed, MarkerNone, MarkerNone},
	{"o--o", EdgeSolid, MarkerCircle, MarkerCircle},
	{"x--x", EdgeSolid, MarkerCross, MarkerCross},
	{"==>", EdgeThick, MarkerNone, MarkerArrow},
	{"===", EdgeThick, MarkerNone, MarkerNone},
	{"-->", EdgeSolid, MarkerNone, MarkerArrow},
	{"---", EdgeSolid, MarkerNone, MarkerNone},
	{"--o", EdgeSolid, MarkerNone, MarkerCircle},
	{"--x", EdgeSolid, MarkerNone, MarkerCross},
}

func matchFlowEdge(s string) (flowLinkOp, int, bool) {
	// 1. Check standard arrow operators first (optionally followed by |label|)
	// so tokens like "--x", "---", "-->", "-.-" aren't misread as inline labels.
	for _, p := range flowEdgePrefixes {
		if strings.HasPrefix(s, p.tok) {
			op := flowLinkOp{style: p.style, startM: p.startM, endM: p.endM}
			consumed := len(p.tok)
			rest := strings.TrimLeft(s[consumed:], " \t")
			if strings.HasPrefix(rest, "|") {
				if end := strings.IndexByte(rest[1:], '|'); end >= 0 {
					op.label = normalizeLabel(rest[1 : 1+end])
					ws := len(s[consumed:]) - len(rest)
					consumed += ws + 1 + end + 1
				}
			}
			return op, consumed, true
		}
	}

	// 2. Check inline text links, both spaced ("-- label -->") and compact ("--label-->").
	for _, inline := range []struct {
		open   string
		style  EdgeKind
		closes []struct {
			tok  string
			endM MarkerKind
		}
	}{
		{
			open:  "-.",
			style: EdgeDashed,
			closes: []struct {
				tok  string
				endM MarkerKind
			}{
				{".->", MarkerArrow},
				{".-", MarkerNone},
			},
		},
		{
			open:  "==",
			style: EdgeThick,
			closes: []struct {
				tok  string
				endM MarkerKind
			}{
				{"==>", MarkerArrow},
				{"===", MarkerNone},
			},
		},
		{
			open:  "--",
			style: EdgeSolid,
			closes: []struct {
				tok  string
				endM MarkerKind
			}{
				{"-->", MarkerArrow},
				{"---", MarkerNone},
			},
		},
	} {
		if !strings.HasPrefix(s, inline.open) {
			continue
		}
		afterOpen := s[len(inline.open):]
		bestEnd := -1
		bestLen := 0
		bestEndM := MarkerNone
		for _, cl := range inline.closes {
			idx := strings.Index(afterOpen, cl.tok)
			if idx < 0 {
				continue
			}
			if bestEnd < 0 || idx < bestEnd || (idx == bestEnd && len(cl.tok) > bestLen) {
				bestEnd = idx
				bestLen = len(cl.tok)
				bestEndM = cl.endM
			}
		}
		if bestEnd >= 0 {
			label := normalizeLabel(afterOpen[:bestEnd])
			return flowLinkOp{
				style: inline.style,
				endM:  bestEndM,
				label: label,
			}, len(inline.open) + bestEnd + bestLen, true
		}
	}

	return flowLinkOp{}, 0, false
}

func splitAmpersand(seg string) []string {
	var out []string
	inQuote := byte(0)
	depth := 0
	start := 0
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(seg) {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			continue
		}
		switch c {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			if depth > 0 {
				depth--
			}
		case '&':
			if depth == 0 {
				if p := strings.TrimSpace(seg[start:i]); p != "" {
					out = append(out, p)
				}
				start = i + 1
			}
		}
	}
	if p := strings.TrimSpace(seg[start:]); p != "" {
		out = append(out, p)
	}
	return out
}

func parseFlowNodeExpr(expr string) (id, label string, shape ShapeKind, explicit, ok bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", "", ShapeRect, false, false
	}
	// Strip optional :::class annotation.
	if idx := strings.Index(expr, ":::"); idx >= 0 {
		expr = strings.TrimSpace(expr[:idx])
	}
	delims := strings.IndexAny(expr, "[({")
	if delims < 0 {
		return expr, expr, ShapeRound, false, isValidNodeID(expr)
	}
	id = strings.TrimSpace(expr[:delims])
	if !isValidNodeID(id) {
		return "", "", ShapeRect, false, false
	}
	rest := strings.TrimSpace(expr[delims:])
	for _, pat := range []struct {
		open, close string
		shape       ShapeKind
	}{
		{"(((", ")))", ShapeCircle},
		{"((", "))", ShapeCircle},
		{"([", "])", ShapeStadium},
		{"[[", "]]", ShapeSubroutine},
		{"[(", ")]", ShapeCylinder},
		{"{{", "}}", ShapeHexagon},
		{"[/", "/]", ShapeParallelogram},
		{"[\\", "\\]", ShapeParallelogram},
		{"[", "]", ShapeRect},
		{"(", ")", ShapeRound},
		{"{", "}", ShapeDiamond},
	} {
		if strings.HasPrefix(rest, pat.open) && strings.HasSuffix(rest, pat.close) && len(rest) >= len(pat.open)+len(pat.close) {
			inner := rest[len(pat.open) : len(rest)-len(pat.close)]
			return id, normalizeLabel(inner), pat.shape, true, true
		}
	}
	return id, id, ShapeRound, false, true
}

func isValidNodeID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r == ' ' || r == '\t' || r == '[' || r == ']' || r == '(' || r == ')' || r == '{' || r == '}' {
			return false
		}
	}
	return true
}
