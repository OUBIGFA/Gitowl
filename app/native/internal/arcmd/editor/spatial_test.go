package editor

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestSpatialQueriesMatchExhaustiveScan(t *testing.T) {
	rng := rand.New(rand.NewSource(104))
	rects := make([]ui.Rect, 257)
	for i := range rects {
		rects[i] = ui.Rect{X: float32(i%3) * 200, Y: float32(i/3) * 32, W: 180, H: float32(25 + rng.Intn(140))}
	}
	var index spatialIndex
	index.reset(len(rects), func(i int) ui.Rect { return rects[i] })
	for step := 0; step < 300; step++ {
		at := rng.Intn(len(rects))
		if step%2 == 0 {
			dy := float32(rng.Intn(100) - 50)
			for i := at; i < len(rects); i++ {
				rects[i].Y += dy
			}
			index.refitSuffix(at)
		} else {
			rects[at].H = float32(rng.Intn(180) + 1)
			index.update(at, rects[at])
		}
		y := float32(rng.Intn(3200) - 200)
		var want []int
		for i, r := range rects {
			if r.Y+r.H >= y && r.Y <= y+50 {
				want = append(want, i)
			}
		}
		if got := slices.Collect(index.between(y, y+50)); !slices.Equal(got, want) {
			t.Fatalf("range %d: %v / %v", step, got, want)
		}
		p := ui.Point{X: float32(rng.Intn(900) - 100), Y: y}
		best, distance := 0, float32(math.MaxFloat32)
		for i, r := range rects {
			dx := max(r.X-p.X, p.X-r.X-r.W, 0)
			dy := max(r.Y-p.Y, p.Y-r.Y-r.H, 0)
			d := dx*dx + dy*dy
			if d < distance {
				best, distance = i, d
			}
		}
		if got := index.nearest(p); got != best {
			t.Fatalf("nearest %d: %d / %d", step, got, best)
		}
	}
}

// Exercise the actual MyGo callback type, which is intentionally internal to
// MyGo, without importing it or relying on unsafe access to the tester.
func checkAccessValue[T any](t *testing.T, access func(func(T)) ui.Element, read func() string) {
	t.Helper()
	var callback func(T)
	accessTextValue(func(fn func(T)) ui.Element { callback = fn; return access(fn) }, read)
	node := reflect.New(reflect.TypeFor[T]().Elem()).Interface().(T)
	callback(node)
	if got := reflect.ValueOf(node).Elem().FieldByName("Value").String(); got != read() {
		t.Fatal("accessibility text differs", got)
	}
}

func TestLazyTextAndAccessibilityPreservePreedit(t *testing.T) {
	e, tt := newTester("# Title\n\n中文😀 body\n\nend")
	focus(e, tt)
	e.SetSelection(e.Len(), e.Len())
	tt.Type(" added")
	if e.textKnown {
		t.Fatal("ordinary typing materialized the entire document")
	}
	original := e.Text()
	tt.Compose("候选😀", 4)
	if e.Text() != original {
		t.Fatal("preedit leaked into committed text")
	}
	ui.NewTester(func(c *ui.Context) {
		checkAccessValue(t, ui.Box(c).Access, e.displayText)
	}, 100, 100).Frame()
	if e.displayText() != e.Doc.PlainText() || e.displayText() == original {
		t.Fatal("screen reader did not receive current preedit")
	}
	tt.Key(0, ui.KeyEscape)
	if e.Text() != original {
		t.Fatal("cancel changed committed text")
	}
}

func TestSpatialHitTestingMatchesRowsAfterEdits(t *testing.T) {
	e, tt := newTester("# Title\n\n" + strings.Repeat("first paragraph wraps 中文😀 and text\n\n", 60) + "| A | B | C |\n|---|---|---|\n| a | b | c |\n| d | e | f |\n\n## Last\n\nend")
	for _, b := range e.Doc.Blocks {
		if b.Kind == "table" {
			e.Engine.MergeCells(b.ID, document.CellRange{Row0: 0, Col0: 0, Row1: 1, Col1: 1})
			e.Engine.SetWidths(b.ID, []float32{1, 2, 3})
		}
	}
	e.Engine.Insert(document.Columns())
	tt.SetSize(917, 651)
	focus(e, tt)
	rng := rand.New(rand.NewSource(147))
	for step := 0; step < 75; step++ {
		leaves := e.Doc.Leaves()
		l := leaves[1+step%min(12, len(leaves)-1)]
		e.SetSelection(l.Start, l.End)
		tt.Type(strings.Repeat("中文😀 wrap ", rng.Intn(28)+1))
		// Include exact row edges and gap midpoints, where tied rectangles must
		// still choose the first row in document order.
		for j := 0; j < 100; j++ {
			r := e.rows[rng.Intn(len(e.rows))].rect
			p := ui.Point{X: r.X + r.W*float32(rng.Float64()*1.2), Y: r.Y + r.H*float32(rng.Float64()*1.2)}
			if j%4 == 0 {
				p = ui.Point{X: r.X + r.W, Y: r.Y + r.H}
			}
			best, distance := 0, float32(math.MaxFloat32)
			for i, row := range e.rows {
				dx := max(row.rect.X-p.X, p.X-row.rect.X-row.rect.W, 0)
				dy := max(row.rect.Y-p.Y, p.Y-row.rect.Y-row.rect.H, 0)
				if d := dx*dx + dy*dy; d < distance {
					best, distance = i, d
				}
			}
			if got := e.rowSpace.nearest(p); got != best {
				t.Fatalf("step %d point %v: indexed row %d, exhaustive %d", step, p, got, best)
			}
			row := e.indexedRow(best)
			want := row.leaf.Start + row.layout.IndexAt(ui.Point{X: p.X - row.x, Y: p.Y - row.y})
			if got, ok := e.IndexForPoint(p); !ok || got != want {
				t.Fatalf("point %v: got %d, want %d", p, got, want)
			}
		}
	}
}

func TestSpatialNearestAtLargeFractionalCoordinates(t *testing.T) {
	rng := rand.New(rand.NewSource(411))
	rects := make([]ui.Rect, 129)
	for i := range rects {
		rects[i] = ui.Rect{X: 173.37, Y: 1e6 + float32(i)*31.17, W: 236.37, H: 31.16}
	}
	var index spatialIndex
	index.reset(len(rects), func(i int) ui.Rect { return rects[i] })
	for step := 0; step < 10000; step++ {
		r := rects[rng.Intn(len(rects))]
		p := ui.Point{X: r.X + r.W, Y: r.Y + r.H + float32(rng.Intn(3)-1)/32}
		best, distance := 0, float32(math.MaxFloat32)
		for i, r := range rects {
			dx := max(r.X-p.X, p.X-r.X-r.W, 0)
			dy := max(r.Y-p.Y, p.Y-r.Y-r.H, 0)
			if d := dx*dx + dy*dy; d < distance {
				best, distance = i, d
			}
		}
		if got := index.nearest(p); got != best {
			t.Fatalf("point %v: got %d, want %d", p, got, best)
		}
	}
}

func TestSpatialShiftSuffixMatchesFullRebuild(t *testing.T) {
	const n = 257
	rects := make([]ui.Rect, n)
	for i := range rects {
		rects[i] = ui.Rect{X: 48, Y: float32(i) * 32, W: 700, H: 28}
	}
	var lazyIdx, fullIdx spatialIndex
	lazyIdx.reset(n, func(i int) ui.Rect { return rects[i] })
	fullIdx.reset(n, func(i int) ui.Rect { return rects[i] })

	for _, op := range []struct {
		first int
		dy    float32
	}{
		{40, 125.5},
		{180, -40.25},
		{17, 64},
	} {
		for i := op.first; i < n; i++ {
			rects[i].Y += op.dy
		}
		lazyIdx.shiftSuffix(op.first, op.dy)
		fullIdx.reset(n, func(i int) ui.Rect { return rects[i] })
	}

	// Single leaf update interleaved with lazy shifts.
	rects[90].H += 18
	for i := 91; i < n; i++ {
		rects[i].Y += 18
	}
	lazyIdx.update(90, rects[90])
	lazyIdx.shiftSuffix(91, 18)
	fullIdx.reset(n, func(i int) ui.Rect { return rects[i] })

	for _, band := range [][2]float32{{0, 600}, {1200, 2600}, {5000, 7500}} {
		var got, want []int
		for i := range lazyIdx.between(band[0], band[1]) {
			got = append(got, i)
		}
		for i := range fullIdx.between(band[0], band[1]) {
			want = append(want, i)
		}
		if len(got) != len(want) {
			t.Fatalf("between %v len=%d, want %d", band, len(got), len(want))
		}
		for k := range got {
			if got[k] != want[k] {
				t.Fatalf("between %v got[%d]=%d, want %d", band, k, got[k], want[k])
			}
		}
	}
	for y := float32(0); y < 8600; y += 137 {
		p := ui.Point{X: 100, Y: y}
		if got, want := lazyIdx.nearest(p), fullIdx.nearest(p); got != want {
			t.Fatalf("nearest %v: got %d, want %d", p, got, want)
		}
	}
}
