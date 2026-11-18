package editor

import (
	"sort"
	"strconv"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// The outline lists the page title and the headings left of the page, as
// Feishu's documents do. Collapsed, a button at the page's top left opens
// it; open, it takes room beside the page, which slides over to make it,
// and its « button collapses it again. The reader's choice is the host's
// to keep (Options.Outline / SetOutline). A pane too narrow to give up the
// room opens the outline over the page instead, until a press elsewhere.

const (
	// outlineWidth is the room the open outline takes.
	outlineWidth = 248
	// outlineMinPage is the narrowest page the outline makes room beside;
	// narrower panes float it over the page.
	outlineMinPage = 600
	// outlineRow is the height of an entry.
	outlineRow = 28
	// outlineAll is how many headings the outline builds every row for;
	// a longer one builds those in view and outlineSpare on either side.
	outlineAll   = 36
	outlineSpare = 12
	// outlineIndent is how far each heading level steps in.
	outlineIndent = 12
	// outlineSlide is how long the page takes to slide over.
	outlineSlide = 220 * time.Millisecond
	// jumpDuration is how long a click on an entry scrolls for.
	jumpDuration = 320 * time.Millisecond
)

type outlineMode int

const (
	outlineOff      outlineMode = iota // no outline: no headings, or no host
	outlineBeside                      // the outline takes room beside the page
	outlineFloating                    // too narrow: it floats over the page
)

type outlineState struct {
	open     bool // the host's choice, for outlineBeside
	set      func(bool)
	mode     outlineMode
	floating bool    // the floating outline is showing
	target   float32 // the room the outline is to take
	slide    outlineSlideState
	toggled  bool // the reader opened or collapsed it since the last frame
	// buttonTop is where both the « and the ≡ buttons sit, so collapsing
	// and opening keep the pointer on the button.
	buttonTop float32
	scrollMax float32
	jump      outlineJump
	// list is where the entries are scrolled; dragging is the heading
	// being dragged, whose row is built while it is out of view so that
	// the drag goes on (outlineList).
	list     ui.ScrollState
	dragging string

	headingsDoc   *document.Document
	headingsRev   uint64
	headingsKnown bool
	headingsHas   bool
}

// outlineSlideState moves the outline's room from one width to another.
type outlineSlideState struct {
	from, to float32
	start    time.Time
}

// outlineJump scrolls the page to a heading picked in the outline.
type outlineJump struct {
	id             string
	from, to, last float32
	start          time.Time
}

// hasHeadings reports whether the outline has more than the page title to
// list.
func (e *Editor) hasHeadings() bool {
	s := &e.outlineState
	rev := e.Revision()
	if s.headingsKnown && s.headingsDoc == e.Doc {
		if s.headingsRev == rev {
			return s.headingsHas
		}
		if change, ok := e.TextChangeSince(s.headingsRev); ok {
			if leaf, exists := e.Engine.LeafByID(change.ID); exists && leaf.Block.Kind != "heading" {
				s.headingsRev = rev
				return s.headingsHas
			}
		}
	}
	has := false
	for i, b := range e.Doc.Blocks {
		if i > 0 && b.Kind == "heading" && b.Text() != "" {
			has = true
			break
		}
	}
	s.headingsDoc, s.headingsRev, s.headingsKnown, s.headingsHas = e.Doc, rev, true, has
	return has
}

// hasBookmarks reports whether the outline has bookmarks to list.
func (e *Editor) hasBookmarks() bool {
	if len(e.bookmarks.marks) == 0 || e.bookmarks.toggle == nil {
		return false
	}
	for _, b := range e.Doc.Blocks {
		if _, ok := e.bookmarks.marks[b.ID]; ok {
			return true
		}
	}
	return false
}

// outlineReserve settles the outline's mode for the pane width and returns
// the room it takes beside the page now, moving to it.
func (e *Editor) outlineReserve(c *ui.Context, o Options) float32 {
	s := &e.outlineState
	s.open, s.set = o.Outline, o.SetOutline
	was := s.mode
	s.mode = outlineOff
	if s.set != nil && (e.hasHeadings() || e.hasBookmarks()) {
		s.mode = outlineFloating
		if o.Width-outlineWidth >= outlineMinPage {
			s.mode = outlineBeside
		}
	}
	if s.mode != outlineFloating {
		s.floating = false
	}
	if e.ready && was == outlineOff && s.mode == outlineBeside && s.open {
		// The first bookmark of a page without headings brings the outline
		// in under the reader's pointer: it slides, as when opened by hand.
		s.toggled = true
	}
	// Beside the title, unless the page's margin without the outline is
	// too narrow for the button and the title's handle: then above it.
	s.buttonTop = pageTop + 12
	if (o.Width-columnWidth(o.Column))/2 < 96 {
		s.buttonTop = 12
	}
	s.target = 0
	if s.mode == outlineBeside && s.open {
		s.target = outlineWidth
	}
	return e.outlineRoom(c)
}

// outlineRoom is the room the outline takes now. Only the reader opening
// or collapsing it slides the page; a pane growing or shrinking past where
// the outline fits beside the page, or another document, settles at once,
// rather than squeezing the page while it moves.
func (e *Editor) outlineRoom(c *ui.Context) float32 {
	s := &e.outlineState
	now := c.Now()
	at := func() float32 {
		t := float32(now.Sub(s.slide.start)) / float32(outlineSlide)
		if t >= 1 {
			return s.slide.to
		}
		t = 1 - (1-t)*(1-t)*(1-t)
		return s.slide.from + (s.slide.to-s.slide.from)*t
	}
	if s.target != s.slide.to {
		if s.toggled && !c.Preferences().ReduceMotion {
			s.slide = outlineSlideState{from: at(), to: s.target, start: now}
		} else {
			s.slide = outlineSlideState{from: s.target, to: s.target}
		}
	}
	s.toggled = false
	room := at()
	if room != s.slide.to {
		c.After(0)
	}
	return room
}

// OutlineOpen reports whether the outline shows.
func (e *Editor) OutlineOpen() bool {
	s := &e.outlineState
	return s.mode == outlineBeside && s.open || s.mode == outlineFloating && s.floating
}

// ToggleOutline opens or collapses the outline: beside the page it is the
// reader's lasting choice, over a narrow page it shows until dismissed.
func (e *Editor) ToggleOutline() {
	s := &e.outlineState
	switch s.mode {
	case outlineBeside:
		if s.set != nil {
			s.set(!s.open)
			s.open = !s.open
			s.toggled = true
		}
	case outlineFloating:
		s.floating = !s.floating
	}
}

// jumpTo scrolls the page to y, easing out.
func (e *Editor) jumpTo(c *ui.Context, id string, y float32) {
	e.scrollGlide.active = false
	to := min(max(0, y-24), e.outlineState.scrollMax)
	if c.Preferences().ReduceMotion {
		e.Scroll.Y = to
		return
	}
	e.outlineState.jump = outlineJump{id: id, from: e.Scroll.Y, to: to, last: e.Scroll.Y, start: c.Now()}
	c.After(0)
}

// stepJump moves a jump on; scrolling by hand stops it.
func (e *Editor) stepJump(c *ui.Context) {
	j := &e.outlineState.jump
	if j.id == "" {
		return
	}
	if d := e.Scroll.Y - j.last; d > 1 || d < -1 {
		j.id = ""
		return
	}
	t := float32(c.Now().Sub(j.start)) / float32(jumpDuration)
	if t >= 1 {
		e.Scroll.Y = j.to
		j.id = ""
		return
	}
	t = 1 - (1-t)*(1-t)*(1-t)
	e.Scroll.Y = j.from + (j.to-j.from)*t
	j.last = e.Scroll.Y
	c.After(0)
}

// outlineView draws the collapsed outline's button or the outline; reserve
// is the room it has beside the page now.
func (e *Editor) outlineView(c *ui.Context, pane ui.Element, reserve float32) {
	s := &e.outlineState
	if s.mode == outlineOff {
		return
	}
	t := c.Theme()
	// shown is how far the outline has come in, 0 to 1.
	shown := reserve / outlineWidth
	if s.mode == outlineFloating {
		target := float32(0)
		if s.floating {
			target = 1
		}
		shown = pane.Animate("outline-float", target, 160*time.Millisecond)
	}
	if shown < 1 {
		open := ui.ButtonBase(c.Key("outline-open")).Absolute().Left(16).Top(s.buttonTop).Size(28, 28).Radius(6).Center().
			Label(i18n.T("展开目录")).Tooltip(i18n.T("展开目录")).KeepFocus().Cursor(ui.CursorPointer).Opacity(1 - shown)
		color := t.Text.Alpha(.55)
		if open.Hovered() {
			open.Background(t.Text.Alpha(.06))
			color = t.Text.Alpha(.85)
		}
		open.Children(func() { ui.Icon(c, iconOutline).FontSize(18).TextColor(color) })
		if open.Clicked() {
			e.ToggleOutline()
		}
	}
	if shown <= 0 {
		return
	}
	panel := ui.Column(c.Key("outline")).Absolute().Left(0).Top(0).Width(outlineWidth).Height(e.viewport).
		Padding(s.buttonTop, 12, 16, 16).Gap(8).Opacity(shown).Translate((shown-1)*24, 0)
	if s.mode == outlineFloating {
		panel.Background(e.page.Popover).Shadow(4, 0, 24, 0, t.Text.Alpha(.12)).KeepFocus()
	}
	panel.Children(func() {
		close := ui.ButtonBase(c.Key("outline-close")).Size(28, 28).Radius(6).Center().AlignSelf(ui.Start).
			Label(i18n.T("收起目录")).Tooltip(i18n.T("收起目录")).KeepFocus().Cursor(ui.CursorPointer)
		color := t.Text.Alpha(.55)
		if close.Hovered() {
			close.Background(t.Text.Alpha(.06))
			color = t.Accent
		}
		close.Children(func() { ui.Icon(c, iconChevronsLeft).FontSize(18).TextColor(color) })
		if close.Clicked() {
			e.ToggleOutline()
		}
		e.outlineList(c)
	})
	if s.mode == outlineFloating && s.floating && panel.PressedOutside() {
		s.floating = false
	}
}

type outlinePillMatch struct{ ed *Editor }
type outlineBarMatch struct{ ed *Editor }

// outlineList lists the entries: the section being read in the accent
// color, a click scrolling to it, a heading dragged onto another moving
// its section before it.
func (e *Editor) outlineList(c *ui.Context) {
	t := c.Theme()
	title := ""
	if len(e.Doc.Blocks) > 0 {
		title = e.Doc.Blocks[0].ID
	}
	active := max(0, sort.Search(len(e.outline), func(i int) bool { return e.indexedOutline(i).y > e.Scroll.Y+96 })-1)
	if e.outlineState.jump.id != "" {
		// A jump marks where it goes at once, not each heading it passes.
		for i, item := range e.outline {
			if item.id == e.outlineState.jump.id {
				active = i
			}
		}
	}
	top := 6
	for _, item := range e.outline {
		top = min(top, item.level)
	}
	// Only the rows near what the list shows are built: a long page has
	// thousands of headings, and building each one every frame was most of
	// a frame. Spacers keep the rows out of view taking their room.
	s := &e.outlineState
	from, to := 0, len(e.outline)
	if len(e.outline) > outlineAll {
		// Y may be set past the end (the container clamps it as it lays
		// out); MaxY is how far the last frame let it go.
		y := max(0, min(s.list.Y, s.list.MaxY))
		from = min(len(e.outline), max(0, int(y/outlineRow)-outlineSpare))
		to = min(len(e.outline), from+int(e.viewport/outlineRow)+2*outlineSpare)
	}
	dragAt := -1
	if s.dragging != "" {
		for i, item := range e.outline {
			if item.id == s.dragging {
				dragAt = i
				break
			}
		}
	}
	dragging := ""
	ui.Scroll(c.Key("outline-scroll")).Grow(1).MinHeight(0).TrackScroll(&s.list).Children(func() {
		// Keep the offscreen drag source at its original keyed position, with
		// spacers for everything between it and the visible rows.
		cursor := 0
		rows := func(a, z int) {
			if a >= z {
				return
			}
			if a > cursor {
				ui.Box(c.Key("outline-gap:" + strconv.Itoa(cursor))).Height(float32(a-cursor) * outlineRow)
			}
			for i := a; i < z; i++ {
				item := *e.indexedOutline(i)
				on := i == active
				row := ui.ButtonBase(c.Key("outline:"+item.id)).Height(outlineRow).Radius(6).
					Padding(0, 8, 0, 8+float32(min(item.level-top, 4))*outlineIndent).
					Label(i18n.T("跳到 ") + item.text).Tooltip(item.text).KeepFocus().Cursor(ui.CursorPointer)
				if item.id != title {
					row.Drag(outlineDrag{item.id})
				}
				if row.Hovered() {
					row.Background(t.Text.Alpha(.05))
				}
				// A section dragged onto a heading goes before it.
				dragged, over := ui.DragOver[outlineDrag](row)
				over = over && dragged.id != item.id && item.id != title
				if d, ok := ui.Drop[outlineDrag](row); ok && d.id != item.id && item.id != title {
					e.Engine.MoveSection(d.id, item.id)
				}
				if row.Dragging() {
					row.Opacity(.4)
					dragging = item.id
				}
				if over {
					row.Draw(func(p *ui.Painter, r ui.Rect) {
						p.Fill(ui.Rect{X: r.X, Y: r.Y - 1, W: r.W, H: 2}, t.Accent, 1)
					})
				}
				color, weight := t.Text.Alpha(.72), 400
				if on {
					color, weight = t.Accent, 500
				}
				row.Children(func() {
					if on {
						slidingPill(c, outlinePillMatch{e}, t.Accent.Alpha(0.08), 6)
						ui.Box(c.Key("active-bar")).Absolute().Left(1).Top(6).Bottom(6).Width(2.5).
							Radius(1.5).Background(t.Accent).PassThrough().
							Transition(ui.ElementTransition{Duration: 120 * time.Millisecond, Match: outlineBarMatch{e}})
					}
					ui.Row(c).Grow(1).MinWidth(0).Justify(ui.Start).AlignItems(ui.Center).Children(func() {
						ui.Text(c, item.text).FontSize(14).FontWeight(weight).TextColor(color).SingleLine().MinWidth(0).Shrink(1)
					})
				})
				if row.Clicked() {
					e.jumpTo(c, item.id, item.y)
				}
			}
			cursor = z
		}
		if dragAt >= 0 && dragAt < from {
			rows(dragAt, dragAt+1)
		}
		rows(from, to)
		if dragAt >= to {
			rows(dragAt, dragAt+1)
		}
		if cursor < len(e.outline) {
			ui.Box(c.Key("outline-gap:" + strconv.Itoa(cursor))).Height(float32(len(e.outline)-cursor) * outlineRow)
		}
		// Below the last heading: the end of the document.
		end := ui.Box(c.Key("outline-end")).Height(16)
		_, over := ui.DragOver[outlineDrag](end)
		if d, ok := ui.Drop[outlineDrag](end); ok {
			e.Engine.MoveSection(d.id, "")
		}
		if over {
			end.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(ui.Rect{X: r.X, Y: r.Y + 1, W: r.W, H: 2}, t.Accent, 1)
			})
		}
		e.bookmarkSection(c)
	})
	s.dragging = dragging
}
