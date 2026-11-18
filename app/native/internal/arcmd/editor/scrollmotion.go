package editor

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// scrollGlideState drives damped, interruptible viewport scrolling for
// typewriter mode, caret edge reveals, find-match jumps, and footnote hops.
type scrollGlideState struct {
	active bool
	to     float32
	lastY  float32
	tau    float64
	at     time.Time
}

// glideScrollTo smoothly moves e.Scroll.Y toward wantY with exponential time
// constant tau (in seconds). Any manual wheel/trackpad scroll or drag-edge
// selection scroll cancels the glide immediately on the next frame.
func (e *Editor) glideScrollTo(c *ui.Context, wantY float32, tau float64) {
	wantY = max(0, wantY)
	e.outlineState.jump = outlineJump{}
	s := &e.scrollGlide
	if c.Preferences().ReduceMotion || math.Abs(float64(wantY-e.Scroll.Y)) < 0.75 {
		e.Scroll.Y = wantY
		s.active = false
		return
	}
	if !s.active {
		// Take an immediate responsive step on the trigger frame so keyboard
		// navigation feels zero-latency while easing the remaining distance.
		e.Scroll.Y += (wantY - e.Scroll.Y) * 0.28
		s.at = c.Now()
	}
	s.to = wantY
	s.lastY = e.Scroll.Y
	s.tau = tau
	s.active = true
	c.After(0)
}

func (e *Editor) stepScrollGlide(c *ui.Context) {
	s := &e.scrollGlide
	if !s.active {
		return
	}
	if c.Preferences().ReduceMotion {
		e.Scroll.Y = max(0, s.to)
		s.active = false
		return
	}
	if e.dragging || math.Abs(float64(e.Scroll.Y-s.lastY)) > 0.75 {
		s.active = false
		return
	}
	now := c.Now()
	dt := now.Sub(s.at).Seconds()
	if dt <= 0 {
		c.After(0)
		return
	}
	if dt > 0.25 {
		dt = 0.25
	}
	s.at = now
	tau := s.tau
	if tau <= 0 {
		tau = 0.045
	}
	alpha := 1 - float32(math.Exp(-dt/tau))
	e.Scroll.Y += (s.to - e.Scroll.Y) * alpha
	if math.Abs(float64(s.to-e.Scroll.Y)) < 0.5 {
		e.Scroll.Y = s.to
		s.lastY = e.Scroll.Y
		s.active = false
		return
	}
	s.lastY = e.Scroll.Y
	c.After(0)
}
