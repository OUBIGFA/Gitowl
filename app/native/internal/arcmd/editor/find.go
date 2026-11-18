package editor

// Find and replace: ⌘F opens a box at the page's top right that finds text
// case-insensitively in every block, folded ones included; ⇧⌘H opens it
// with replace. Enter steps to the next match, ⇧Enter to the one before.

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// finder is the state of the find box.
type finder struct {
	open, replace bool
	query, with   string
	// matches are in document order, as UTF-16 ranges of the plain text;
	// current indexes them.
	matches []ui.TextInputRange
	current int
	// key is what matches were found for.
	key      string
	queryKey string
	revision uint64
	doc      *document.Document
	matcher  textMatcher
	// focus asks the query field for the keyboard.
	focus bool
	// pulsePending and pulseAt briefly accent the active match after stepping.
	pulsePending bool
	pulseAt      time.Time
}

// findIn finds query in text case-insensitively, without overlaps; the
// ranges are UTF-16 offsets into text.
func findIn(text, query string) [][2]int { m := newTextMatcher(query); return m.find(text) }

// KMP folds one rune at a time and keeps only a pattern-sized ring of UTF-16
// starts. Repeated-prefix queries stay linear and do not allocate per text rune.
type textMatcher struct {
	pattern         []rune
	failure, starts []int
}

func newTextMatcher(query string) textMatcher {
	q := []rune(strings.Map(unicode.ToLower, query))
	m := textMatcher{q, make([]int, len(q)), make([]int, len(q))}
	for i, j := 1, 0; i < len(q); i++ {
		for j > 0 && q[i] != q[j] {
			j = m.failure[j-1]
		}
		if q[i] == q[j] {
			j++
		}
		m.failure[i] = j
	}
	return m
}
func (m *textMatcher) find(text string) [][2]int {
	q := m.pattern
	if len(q) == 0 || len(text) < len(q) {
		return nil
	}
	var out [][2]int
	units, index, matched := 0, 0, 0
	for _, r := range text {
		m.starts[index%len(q)] = units
		units += runeUnits(r)
		r = unicode.ToLower(r)
		for matched > 0 && r != q[matched] {
			matched = m.failure[matched-1]
		}
		if r == q[matched] {
			matched++
		}
		if matched == len(q) {
			out = append(out, [2]int{m.starts[(index-len(q)+1)%len(q)], units})
			matched = 0 // the existing find contract excludes overlapping matches
		}
		index++
	}
	return out
}

// OpenFind opens the find box, with replace when replace is set. A
// selection within one line seeds the query, as a browser's find bar does.
func (e *Editor) OpenFind(replace bool) {
	if e.ReadOnly {
		replace = false
		e.find.replace = false
	}
	r := e.Selection().Range
	if r.Start != r.End {
		if text, _ := e.TextForRange(r); !strings.ContainsAny(text, "\n ") {
			e.find.query = text
		}
	}
	e.find.open, e.find.replace, e.find.focus = true, replace || e.find.replace && e.find.open, true
	e.find.key = ""
}

// FindText opens the find box on query and scrolls to its first match, as
// the command bar does when it opens a document at text it found there.
// The box takes the keyboard, so Enter steps on through the matches and
// Escape returns to the page.
func (e *Editor) FindText(query string) {
	f := &e.find
	f.query, f.open, f.replace, f.focus = query, true, false, true
	f.key, f.current = "", 0
	e.refreshMatches()
	e.showMatch()
}

// FindTextWithFallback searches for query literally first and falls back to
// fallback when the document matches multi-keyword terms rather than the literal phrase.
func (e *Editor) FindTextWithFallback(query, fallback string) {
	e.FindText(query)
	if len(e.find.matches) == 0 && fallback != "" && fallback != query {
		e.FindText(fallback)
	}
}

// FindQuery is the find box's query while it is open.
func (e *Editor) FindQuery() (string, bool) { return e.find.query, e.find.open }

// closeFind puts the find box away and gives the page the keyboard back.
func (e *Editor) closeFind() {
	e.find.open = false
	e.RequestFocus = true
}

// refreshMatches finds the query again when it or the document changed.
func (e *Editor) refreshMatches() {
	f := &e.find
	key := fmt.Sprint(e.Revision(), "\x00", f.query)
	if key == f.key {
		return
	}
	sameQuery := f.queryKey == f.query
	if !sameQuery {
		f.matcher = newTextMatcher(f.query)
	}
	if change, ok := e.TextChangeSince(f.revision); ok && sameQuery && f.doc == e.Doc && f.key != "" {
		first := sort.Search(len(f.matches), func(i int) bool { return f.matches[i].Start >= change.Start })
		last := sort.Search(len(f.matches), func(i int) bool { return f.matches[i].Start > change.BeforeEnd })
		delta := change.AfterEnd - change.BeforeEnd
		for i := last; i < len(f.matches); i++ {
			f.matches[i].Start += delta
			f.matches[i].End += delta
		}
		var updated []ui.TextInputRange
		for _, m := range f.matcher.find(change.After) {
			updated = append(updated, ui.TextInputRange{Start: change.Start + m[0], End: change.Start + m[1]})
		}
		f.matches = slices.Replace(f.matches, first, last, updated...)
	} else {
		f.matches = f.matches[:0]
		if f.query != "" {
			for _, leaf := range e.Doc.Leaves() {
				for _, m := range f.matcher.find(leaf.Block.Text()) {
					f.matches = append(f.matches, ui.TextInputRange{Start: leaf.Start + m[0], End: leaf.Start + m[1]})
				}
			}
		}
	}
	f.key, f.queryKey, f.revision, f.doc = key, f.query, e.Revision(), e.Doc
	f.current = max(0, min(f.current, len(f.matches)-1))
}

// stepMatch moves to the next (1) or previous (-1) match and shows it,
// unfolding a section that hides it.
func (e *Editor) stepMatch(delta int) {
	f := &e.find
	if len(f.matches) == 0 {
		return
	}
	f.current = (f.current + delta + len(f.matches)) % len(f.matches)
	e.showMatch()
}

// showMatch scrolls the current match into view.
func (e *Editor) showMatch() {
	f := &e.find
	if f.current >= len(f.matches) {
		return
	}
	m := f.matches[f.current]
	leaf := e.Engine.LeafAt(m.Start)
	if e.Engine.Reveal(leaf.Owner) || e.Engine.Reveal(leaf.Block.ID) {
		e.sync()
	}
	e.revealAt = &m
	f.pulsePending = true
	f.pulseAt = time.Time{}
}

// replaceCurrent replaces the current match and moves on to the next.
func (e *Editor) replaceCurrent() {
	f := &e.find
	if f.current >= len(f.matches) {
		return
	}
	m := f.matches[f.current]
	e.UnmarkText()
	e.Engine.Replace(m.Start, m.End, f.with)
	e.sync()
	e.refreshMatches()
	if len(f.matches) > 0 {
		e.showMatch()
	}
}

// replaceAll replaces every match as one step of the undo history.
func (e *Editor) replaceAll() {
	f := &e.find
	if len(f.matches) == 0 {
		return
	}
	e.UnmarkText()
	matches := append([]ui.TextInputRange(nil), f.matches...)
	e.Engine.Edit(func() {
		for i := len(matches) - 1; i >= 0; i-- {
			e.Engine.Replace(matches[i].Start, matches[i].End, f.with)
		}
	})
	e.sync()
	e.refreshMatches()
}

// findShortcuts opens and steers the find box from anywhere in the window.
func (e *Editor) findShortcuts(c *ui.Context) {
	if b := e.binding("find"); c.Shortcut(b.Mods, b.Key) {
		e.OpenFind(false)
	}
	if b := e.binding("replace"); c.Shortcut(b.Mods, b.Key) {
		e.OpenFind(true)
	}
	if !e.find.open {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		e.closeFind()
	}
	if c.Shortcut(ui.Shift, ui.KeyEnter) {
		e.stepMatch(-1)
	}
	if c.Shortcut(ui.Cmd, ui.KeyG) {
		e.stepMatch(1)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
		e.stepMatch(-1)
	}
}

// findBox is the find (and replace) box at the page's top right.
func (e *Editor) findBox(c *ui.Context) {
	f := &e.find
	if !f.open {
		return
	}
	e.refreshMatches()
	t := c.Theme()
	pg := e.page
	button := func(key, label string, icon *ui.SVG, on bool) ui.Element {
		b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(4).Center().Label(label).Tooltip(label).KeepFocus().Cursor(ui.CursorPointer)
		if on || b.Hovered() {
			b.Background(pg.Hover)
		}
		b.Children(func() { ui.Icon(c, icon).FontSize(16).TextColor(pg.CodeText) })
		return b
	}
	field := func(key, placeholder string, value *string, focus bool) ui.Element {
		in := ui.TextInput(c.Key(key), value).Placeholder(placeholder).Label(placeholder).FontSize(14).Padding(5, 8).Grow(1).MinWidth(0)
		if focus {
			in.Focus()
		}
		return in
	}
	// Not e.popover: its fields must take the keyboard.
	box := ui.Column(c.Key("find-box")).Absolute().Right(16).Top(12).Width(404).Padding(8).Gap(8).
		Background(pg.Popover).Border(1, t.Border).Radius(8).Shadow(0, 4, 16, 0, t.Text.Alpha(.12)).
		Transition(ui.ElementTransition{
			Duration: 120 * time.Millisecond,
			Colors:   true,
			Enter:    &ui.Motion{Opacity: 0, Scale: 0.98, Y: -3},
		})
	box.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			toggleBtn := ui.ButtonBase(c.Key("find-toggle")).Size(24, 24).Radius(4).Center().
				Label(i18n.T("切换替换 (⇧⌘H)")).Tooltip(i18n.T("切换替换 (⇧⌘H)")).KeepFocus().Cursor(ui.CursorPointer).Disabled(e.ReadOnly)
			if f.replace || toggleBtn.Hovered() {
				toggleBtn.Background(pg.Hover)
			}
			rot := float32(0)
			if f.replace {
				rot = 90
			}
			toggleBtn.Children(func() {
				ui.Box(c.Key("chev")).Size(16, 16).Center().AnimatePose(ui.Pose{Rotate: rot}, ui.Snappy).Children(func() {
					ui.Icon(c, iconChevronRight).FontSize(16).TextColor(pg.CodeText)
				})
			})
			if toggleBtn.Clicked() {
				f.replace = !f.replace
			}
			query := field("find-query", i18n.T("查找"), &f.query, f.focus)
			f.focus = false
			if query.Submitted() {
				e.stepMatch(1)
			}
			count := i18n.T("无结果")
			if len(f.matches) > 0 {
				count = fmt.Sprintf("%d/%d", f.current+1, len(f.matches))
			}
			if f.query == "" {
				count = ""
			}
			ui.Text(c, count).FontSize(12).TextColor(pg.Caption).NoWrap().MinWidth(44)
			if button("find-prev", i18n.T("上一个 (⇧Enter)"), iconArrowUp, false).Clicked() {
				e.stepMatch(-1)
			}
			if button("find-next", i18n.T("下一个 (Enter)"), iconArrowDown, false).Clicked() {
				e.stepMatch(1)
			}
			if button("find-close", i18n.T("关闭查找 (Esc)"), iconClose, false).Clicked() {
				e.closeFind()
			}
		})
		if !f.replace {
			return
		}
		ui.Row(c.Key("replace-row")).Gap(6).AlignItems(ui.Center).Padding(0, 0, 0, 30).
			Transition(ui.ElementTransition{
				Duration: 120 * time.Millisecond,
				Enter:    &ui.Motion{Opacity: 0, Y: -3},
			}).Children(func() {
			with := field("find-with", i18n.T("替换为"), &f.with, false)
			if with.Submitted() {
				e.replaceCurrent()
			}
			for _, b := range []struct {
				label string
				run   func()
			}{{i18n.T("替换"), e.replaceCurrent}, {i18n.T("全部替换"), e.replaceAll}} {
				el := ui.ButtonBase(c.Key("find:"+b.label)).Height(28).Width(80).Radius(6).Border(1, t.Border).Center().Label(b.label).KeepFocus().Cursor(ui.CursorPointer)
				if len(f.matches) == 0 {
					el.Disabled(true).Opacity(.4)
				} else if el.Hovered() {
					el.Background(pg.Hover)
				}
				el.Children(func() { ui.Text(c, b.label).FontSize(13).TextColor(t.Text) })
				if el.Clicked() {
					b.run()
				}
			}
		})
	})
}

// paintMatches marks the matches on the page, the current one stronger.
func (e *Editor) paintMatches(p *ui.Painter, bounds ui.Rect) {
	f := &e.find
	if !f.open || len(f.matches) == 0 {
		return
	}
	pulseU := float32(-1)
	if f.pulsePending {
		f.pulsePending = false
		if !e.printing && !e.reduceMotion {
			f.pulseAt = p.Now()
		}
	}
	if !e.printing && !e.reduceMotion && !f.pulseAt.IsZero() {
		const pulseDur = 160 * time.Millisecond
		if elapsed := p.Now().Sub(f.pulseAt); elapsed >= 0 && elapsed < pulseDur {
			pulseU = float32(elapsed) / float32(pulseDur)
			p.AnimationFrame()
		} else {
			f.pulseAt = time.Time{}
		}
	}
	for ri := range e.visibleRows(40) {
		r := *e.indexedRow(ri)
		if r.layout == nil || r.rect.Y+r.rect.H < e.Scroll.Y || r.rect.Y > e.Scroll.Y+e.viewport+40 {
			continue
		}
		dx, dy := e.visualOffset(r)
		_, clipBox, _, _, scrollX, noWrap := e.codeScrollFor(r)
		paintRowMatches := func(sx float32) {
			start := sort.Search(len(f.matches), func(i int) bool { return f.matches[i].End > r.leaf.Start })
			for i := start; i < len(f.matches) && f.matches[i].Start < r.leaf.End+1; i++ {
				m := f.matches[i]
				local := ui.TextInputRange{Start: max(0, m.Start-r.leaf.Start), End: min(r.leaf.End, m.End) - r.leaf.Start}
				color := e.page.FindMatch
				isCurrent := i == f.current
				if isCurrent {
					color = e.page.FindCurrent
				}
				for _, rect := range r.layout.SelectionRects(local) {
					rect.X += bounds.X + r.x + dx - sx
					rect.Y += bounds.Y + r.y + dy
					p.Fill(rect, color, 2.5)
					if isCurrent && pulseU >= 0 {
						pad := 1.5 + 4*(1-(1-pulseU)*(1-pulseU))
						ring := ui.Rect{X: rect.X - pad, Y: rect.Y - pad*0.7, W: rect.W + 2*pad, H: rect.H + 1.4*pad}
						p.Stroke(ring, p.Theme().Accent.Alpha(0.75*(1-pulseU)), 4, 1.5)
					}
				}
			}
		}
		if noWrap {
			p.Clip(ui.Rect{X: bounds.X + clipBox.X + dx, Y: bounds.Y + clipBox.Y + dy, W: clipBox.W, H: clipBox.H}, 0, func() {
				paintRowMatches(scrollX)
			})
		} else {
			paintRowMatches(0)
		}
	}
}
