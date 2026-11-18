package editor

import (
	"iter"
	"math"

	"github.com/egoist/mygo/ui"
)

// spatialIndex preserves document/paint order while bounding groups of rows or
// decorations. Unlike a Y-sorted list it also handles side-by-side columns and
// overlapping merged cells. Point/range queries prune groups outside their
// search area. Height changes refit the translated float32 rectangles, which
// preserves exact caret hit testing even after many fractional translations.
type spatialIndex struct {
	n      int
	bounds []spatialBounds
	lazy   []float32
	rect   func(int) ui.Rect
}
type spatialBounds struct{ left, top, right, bottom float32 }

func rectBounds(r ui.Rect) spatialBounds { return spatialBounds{r.X, r.Y, r.X + r.W, r.Y + r.H} }
func (b spatialBounds) union(c spatialBounds) spatialBounds {
	return spatialBounds{min(b.left, c.left), min(b.top, c.top), max(b.right, c.right), max(b.bottom, c.bottom)}
}
func (s *spatialIndex) reset(n int, rect func(int) ui.Rect) {
	s.n, s.rect = n, rect
	if cap(s.bounds) < 4*n {
		s.bounds = make([]spatialBounds, 4*n)
	} else {
		s.bounds = s.bounds[:4*n]
	}
	if cap(s.lazy) < 4*n {
		s.lazy = make([]float32, 4*n)
	} else {
		s.lazy = s.lazy[:4*n]
		clear(s.lazy)
	}
	var build func(int, int, int)
	build = func(node, a, z int) {
		if z-a == 1 {
			s.bounds[node] = rectBounds(rect(a))
			return
		}
		s.push(node)
		m := (a + z) / 2
		build(node*2, a, m)
		build(node*2+1, m, z)
		s.bounds[node] = s.bounds[node*2].union(s.bounds[node*2+1])
	}
	if n > 0 {
		build(1, 0, n)
	}
}

func (s *spatialIndex) applyLazy(node int, dy float32) {
	s.bounds[node].top += dy
	s.bounds[node].bottom += dy
	s.lazy[node] += dy
}

func (s *spatialIndex) push(node int) {
	dy := s.lazy[node]
	if dy == 0 {
		return
	}
	s.lazy[node] = 0
	s.applyLazy(node*2, dy)
	s.applyLazy(node*2+1, dy)
}

// shiftSuffix translates the Y bounds of [first, n) in O(log N) via lazy propagation.
func (s *spatialIndex) shiftSuffix(first int, dy float32) {
	if dy == 0 || first >= s.n || s.n == 0 {
		return
	}
	if first < 0 {
		first = 0
	}
	var shift func(int, int, int)
	shift = func(node, a, z int) {
		if first >= z {
			return
		}
		if first <= a {
			s.applyLazy(node, dy)
			return
		}
		s.push(node)
		m := (a + z) / 2
		shift(node*2, a, m)
		shift(node*2+1, m, z)
		s.bounds[node] = s.bounds[node*2].union(s.bounds[node*2+1])
	}
	shift(1, 0, s.n)
}

// refitSuffix follows the existing geometry translation, visiting each affected
// leaf once. Lazy sums would round differently from the displayed rectangles.
func (s *spatialIndex) refitSuffix(first int) {
	if first >= s.n || s.n == 0 {
		return
	}
	var change func(int, int, int)
	change = func(node, a, z int) {
		if first >= z {
			return
		}
		if z-a == 1 {
			s.lazy[node] = 0
			s.bounds[node] = rectBounds(s.rect(a))
			return
		}
		s.push(node)
		m := (a + z) / 2
		change(node*2, a, m)
		change(node*2+1, m, z)
		s.bounds[node] = s.bounds[node*2].union(s.bounds[node*2+1])
	}
	change(1, 0, s.n)
}
func (s *spatialIndex) update(at int, r ui.Rect) {
	if s.n == 0 {
		return
	}
	var change func(int, int, int)
	change = func(node, a, z int) {
		if z-a == 1 {
			s.lazy[node] = 0
			s.bounds[node] = rectBounds(r)
			return
		}
		s.push(node)
		m := (a + z) / 2
		if at < m {
			change(node*2, a, m)
		} else {
			change(node*2+1, m, z)
		}
		s.bounds[node] = s.bounds[node*2].union(s.bounds[node*2+1])
	}
	change(1, 0, s.n)
}
func (s *spatialIndex) between(top, bottom float32) iter.Seq[int] {
	return func(yield func(int) bool) {
		var visit func(int, int, int) bool
		visit = func(node, a, z int) bool {
			b := s.bounds[node]
			if b.bottom < top || b.top > bottom {
				return true
			}
			if z-a == 1 {
				return yield(a)
			}
			s.push(node)
			m := (a + z) / 2
			return visit(node*2, a, m) && visit(node*2+1, m, z)
		}
		if s.n > 0 {
			visit(1, 0, s.n)
		}
	}
}
func (s *spatialIndex) nearest(p ui.Point) int {
	if s.n == 0 {
		return -1
	}
	best, distance := 0, float32(math.MaxFloat32)
	dist := func(b spatialBounds) float32 {
		// At large document coordinates, cancellation near an edge can make
		// the summed bound differ from (point-origin)-size. Widen only the
		// pruning bounds by a few coordinate ULPs; leaves use exact geometry.
		scale := max(math.Abs(float64(p.X)), math.Abs(float64(p.Y)), math.Abs(float64(b.left)), math.Abs(float64(b.top)), math.Abs(float64(b.right)), math.Abs(float64(b.bottom)))
		margin := float32(scale * 1e-6)
		x := max(b.left-p.X-margin, p.X-b.right-margin, 0)
		y := max(b.top-p.Y-margin, p.Y-b.bottom-margin, 0)
		return x*x + y*y
	}
	var visit func(int, int, int)
	visit = func(node, a, z int) {
		d := dist(s.bounds[node])
		// Bounds can round a few ULPs differently from the original rectangle
		// distance expression. Keep near ties and compare exact leaf distances.
		if d > distance+max(1, distance)*1e-6 {
			return
		}
		if z-a == 1 {
			r := s.rect(a)
			dx := max(r.X-p.X, p.X-r.X-r.W, 0)
			dy := max(r.Y-p.Y, p.Y-r.Y-r.H, 0)
			d = dx*dx + dy*dy
			if d < distance || d == distance && a < best {
				best, distance = a, d
			}
			return
		}
		s.push(node)
		m := (a + z) / 2
		if dist(s.bounds[node*2]) <= dist(s.bounds[node*2+1]) {
			visit(node*2, a, m)
			visit(node*2+1, m, z)
		} else {
			visit(node*2+1, m, z)
			visit(node*2, a, m)
		}
	}
	visit(1, 0, s.n)
	return best
}

func (e *Editor) visibleRows(margin float32) iter.Seq[int] {
	return e.rowSpace.between(e.Scroll.Y-margin, e.Scroll.Y+e.viewport+margin)
}
func (e *Editor) visibleDecorations(margin float32) iter.Seq[int] {
	return e.decorationSpace.between(e.Scroll.Y-margin, e.Scroll.Y+e.viewport+margin)
}
