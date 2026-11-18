package editor

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func selectionScrollTester(typewriter bool) (*Editor, *ui.Tester) {
	e := New(strings.Repeat("Select across screens with 中文 and emoji 😀.\n\n", 120))
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), Typewriter: typewriter})
	}, 900, 650)
	tt.Frame()
	return e, tt
}

func TestDragSelectionFollowsManualScroll(t *testing.T) {
	for _, typewriter := range []bool{false, true} {
		name := "normal"
		if typewriter {
			name = "typewriter"
		}
		t.Run(name, func(t *testing.T) {
			e, tt := selectionScrollTester(typewriter)
			tt.Scroll(350, 290, 0, 600)
			tt.Press(350, 180)
			anchor := e.Anchor
			tt.Move(350, 290)
			if e.Scroll.Y != 600 {
				t.Errorf("moving the selection scrolled to %v", e.Scroll.Y)
			}
			for _, delta := range []float32{720, 720, -380} {
				before := e.Scroll.Y
				tt.Scroll(350, 290, 0, delta)
				if e.Scroll.Y != before+delta {
					t.Errorf("manual scroll: got %v, want %v", e.Scroll.Y, before+delta)
				}
				want, ok := e.IndexForPoint(ui.Point{X: 350, Y: 290 + e.Scroll.Y})
				if !ok || e.Caret != want || e.Anchor != anchor {
					t.Errorf("stationary pointer: selection %d:%d, want %d:%d", e.Anchor, e.Caret, anchor, want)
				}
			}
			y, selection := e.Scroll.Y, e.Selection().Range
			tt.Release(350, 290)
			if e.Scroll.Y != y || e.Selection().Range != selection {
				t.Errorf("release changed viewport or selection: y=%v, want %v", e.Scroll.Y, y)
			}
		})
	}
}

func TestDragSelectionPreservesWheelGlide(t *testing.T) {
	e, tt := selectionScrollTester(false)
	tt.Press(350, 180)
	tt.Move(350, 290)
	anchor := e.Anchor
	tt.Wheel(350, 290, 0, 600)
	previous := e.Scroll.Y
	for range 60 {
		tt.Advance(time.Second / 60)
		if e.Scroll.Y < previous || e.Scroll.Y > 601 {
			t.Fatalf("wheel jumped from %v to %v", previous, e.Scroll.Y)
		}
		want, _ := e.IndexForPoint(ui.Point{X: 350, Y: 290 + e.Scroll.Y})
		if e.Caret != want || e.Anchor != anchor {
			t.Fatalf("wheel selection %d:%d, want %d:%d", e.Anchor, e.Caret, anchor, want)
		}
		previous = e.Scroll.Y
	}
	if math.Abs(float64(e.Scroll.Y-600)) > 1 {
		t.Fatalf("wheel glide stopped at %v", e.Scroll.Y)
	}
	tt.Release(350, 290)
	if e.Scroll.Y != previous {
		t.Fatal("release rewound the wheel")
	}
}

func TestCopySelectionKeepsViewport(t *testing.T) {
	e, tt := selectionScrollTester(false)
	tt.Press(350, 180)
	tt.Move(350, 290)
	tt.Release(350, 290)
	selection := e.Selection().Range
	tt.Key(ui.Cmd, ui.KeyC)
	want := tt.Clipboard()
	if want == "" {
		t.Fatal("no text selected")
	}
	tt.Scroll(350, 290, 0, 1200)
	y := e.Scroll.Y
	tt.Key(ui.Cmd, ui.KeyC)
	if e.Scroll.Y != y || e.Selection().Range != selection || tt.Clipboard() != want {
		t.Fatalf("copy changed viewport, selection or text: y=%v, want %v", e.Scroll.Y, y)
	}
}

func TestDragSelectionScrollsAtEdge(t *testing.T) {
	for _, stop := range []string{"release", "focus-loss"} {
		t.Run(stop, func(t *testing.T) {
			e, tt := selectionScrollTester(false)
			tt.Press(350, 180)
			anchor := e.Anchor
			tt.Move(350, 900)
			previous := e.Scroll.Y
			if previous != 0 {
				t.Errorf("leaving the viewport jumped to %v before time advanced", previous)
			}
			for range 30 {
				tt.Advance(time.Second / 60)
				step := e.Scroll.Y - previous
				if step <= 0 || step > 12 {
					t.Fatalf("edge scroll step=%v", step)
				}
				want, _ := e.IndexForPoint(ui.Point{X: 350, Y: e.viewport + e.Scroll.Y})
				if e.Anchor != anchor || e.Caret != want {
					t.Fatal("edge selection did not follow the visible text")
				}
				previous = e.Scroll.Y
			}
			if stop == "release" {
				tt.Release(350, 900)
			} else {
				tt.SetFocused(false)
			}
			selection := e.Selection().Range
			tt.Advance(time.Second)
			if e.Scroll.Y != previous || e.Selection().Range != selection || e.dragging {
				t.Fatal("drag kept scrolling after it ended")
			}
		})
	}
}

func TestDragSelectionScrollsUpAndStopsAtBounds(t *testing.T) {
	e, tt := selectionScrollTester(false)
	tt.Scroll(350, 290, 0, 400)
	tt.Press(350, 290)
	anchor := e.Anchor
	tt.Move(350, -300)
	if e.Scroll.Y != 400 {
		t.Fatal("moving above the viewport jumped before time advanced")
	}
	previous := e.Scroll.Y
	// Simulate a frame delayed while the main thread was busy, without
	// running the intermediate frames that Tester.Advance normally runs.
	e.selectionDrag.at = tt.Now().Add(-3 * time.Second)
	tt.Frame()
	if step := previous - e.Scroll.Y; step <= 0 || step > 25 {
		t.Fatalf("a delayed frame jumped by %v", step)
	}
	for range 60 {
		previous = e.Scroll.Y
		tt.Advance(time.Second / 60)
		if e.Scroll.Y > previous || e.Scroll.Y < 0 {
			t.Fatalf("upward scroll went from %v to %v", previous, e.Scroll.Y)
		}
	}
	if e.Scroll.Y != 0 || e.Anchor != anchor || e.Caret >= anchor {
		t.Fatal("upward selection did not reach the beginning")
	}
	tt.Scroll(350, 290, 0, 1e6)
	tt.Move(350, 900)
	tt.Advance(time.Second)
	if e.Scroll.Y != e.Scroll.MaxY || e.Anchor != anchor || e.Caret <= anchor {
		t.Fatal("downward selection did not stop at the end")
	}
	tt.Release(350, 900)
	if e.Scroll.Y != e.Scroll.MaxY {
		t.Fatal("release moved away from the end")
	}
}

func TestSelectionPressPreservesWordAndLine(t *testing.T) {
	for clicks := 1; clicks <= 3; clicks++ {
		e, tt := selectionScrollTester(false)
		for range clicks - 1 {
			tt.ClickAt(350, 640)
		}
		tt.Press(350, 640)
		selection := e.Selection().Range
		if clicks > 1 && selection.Start == selection.End {
			t.Fatalf("%d clicks did not select text", clicks)
		}
		tt.Advance(time.Second)
		tt.Release(350, 640)
		if e.Scroll.Y != 0 || e.Selection().Range != selection {
			t.Fatalf("%d clicks changed selection or scrolled without dragging", clicks)
		}
	}
}

func TestDragSelectionWithOutlineAndInsetViewport(t *testing.T) {
	e := New("# Title\n\n## Section\n\n" + strings.Repeat("Keep selecting text under the pointer. 中文 😀\n\n", 120))
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Grow(1).Padding(20).Children(func() {
			View(c, e, Options{Width: 1180, Theme: c.Theme(), Outline: true, SetOutline: func(bool) {}})
		})
	}, 1220, 700)
	tt.Frame()
	if e.outlineState.mode != outlineBeside {
		t.Fatal("outline is not beside the page")
	}
	surface, ok := tt.Find("文档")
	if !ok {
		t.Fatal("missing writing surface")
	}
	x, y := surface.X+300, surface.Y+290
	tt.Press(x, surface.Y+180)
	anchor := e.Anchor
	tt.Move(x, y)
	tt.Scroll(x, y, 0, 900)
	want, _ := e.IndexForPoint(ui.Point{X: x - surface.X, Y: y - surface.Y + e.Scroll.Y})
	if e.Scroll.Y != 900 || e.Anchor != anchor || e.Caret != want {
		t.Fatalf("inset viewport selection %d:%d, want %d:%d; y=%v", e.Anchor, e.Caret, anchor, want, e.Scroll.Y)
	}
	tt.Release(x, y)
	if e.Scroll.Y != 900 {
		t.Fatal("release scrolled the inset viewport")
	}
}

func TestDragSelectionYieldsToWheelAtEdge(t *testing.T) {
	e, tt := selectionScrollTester(false)
	control, idle := selectionScrollTester(false)
	tt.Scroll(350, 290, 0, 1200)
	idle.Scroll(350, 290, 0, 1200)
	tt.Press(350, 290)
	tt.Move(350, 640)
	// Upward wheel scrolling must win even at the bottom edge, which
	// would otherwise scroll downward while the selection is held.
	tt.Wheel(350, 640, 0, -300)
	idle.Wheel(350, 640, 0, -300)
	for range 8 {
		tt.Advance(time.Second / 60)
		idle.Advance(time.Second / 60)
		// Selection changes can request an earlier layout frame. Sample
		// both scroll containers at the same point in their wheel glide.
		tt.Frame()
		idle.Frame()
		if e.Scroll.Y != control.Scroll.Y {
			t.Fatalf("edge drag interrupted wheel: got %v, want %v", e.Scroll.Y, control.Scroll.Y)
		}
	}
	tt.Release(350, 640)
}
