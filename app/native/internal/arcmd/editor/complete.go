package editor

// Inline completion beyond [[links]]: ":name" offers emoji by name, as
// GitHub's :shortcodes:, and "@" offers today's, tomorrow's and
// yesterday's dates and documents to link to. A list opens under the
// caret; the arrows step, Enter and Tab pick, Escape closes. Typing the
// closing colon of a shortcode that names one emoji puts the emoji in at
// once.

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// EmojiItem is an emoji offered for a :shortcode:, with its shortcode.
type EmojiItem struct {
	Emoji, Name string
}

// completeItem is a row of the list: what it shows and what it puts in
// place of the trigger and what was typed after it.
type completeItem struct {
	icon, label, hint, insert string
}

// completion is the open list: trigger is ':' or '@', start the offset of
// the trigger, typed what follows it up to the caret, and dismissed the
// start Escape closed it at.
type completion struct {
	open      bool
	trigger   rune
	start     int
	typed     string
	index     int
	dismissed int
	items     []completeItem
	reveal    bool
	scroll    ui.ScrollState
}

// triggerBefore finds a trigger the caret is completing in before (the
// block's text up to the caret): the trigger at a word's start, and what
// follows it without spaces. A shortcode is letters, digits, _, + and -.
func triggerBefore(before string) (trigger rune, at int, typed string, ok bool) {
	for i := len(before); i > 0; {
		r, n := utf8.DecodeLastRuneInString(before[:i])
		i -= n
		if r == ':' || r == '@' {
			typed = before[i+n:]
			if i > 0 {
				prev, _ := utf8.DecodeLastRuneInString(before[:i])
				if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == ':' {
					return 0, 0, "", false
				}
			}
			if r == ':' {
				for _, c := range typed {
					if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '+' || c == '-') {
						return 0, 0, "", false
					}
				}
			}
			return r, i, typed, true
		}
		if unicode.IsSpace(r) || len(before)-i > 40 {
			return 0, 0, "", false
		}
	}
	return 0, 0, "", false
}

// watchComplete opens, fills or closes the list for what is typed before
// a collapsed caret.
func (e *Editor) watchComplete(now time.Time, emoji func(string) []EmojiItem, docs func(string) []WikiItem) {
	m := &e.complete
	wasOpen := m.open
	m.open = false
	if !e.focused || e.Anchor != e.Caret || e.mark != nil || e.slashOpen || e.wiki.open || e.ReadOnly {
		return
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if k := leaf.Block.Kind; k == "code" || k == "raw" || k == "math" || e.Engine.CurrentStyle().Code {
		return
	}
	text := leaf.Block.Text()
	before := text[:document.ByteOffset(text, e.Caret-leaf.Start)]
	// A shortcode closed by its colon becomes its emoji.
	if emoji != nil && strings.HasSuffix(before, ":") {
		if r, at, typed, ok := triggerBefore(before[:len(before)-1]); ok && r == ':' && len(typed) >= 2 {
			for _, it := range emoji(typed) {
				if it.Name == strings.ToLower(typed) {
					start := leaf.Start + document.Len(before[:at])
					e.ReplaceText(&ui.TextInputRange{Start: start, End: e.Caret}, it.Emoji)
					m.items = nil
					return
				}
			}
		}
	}
	trigger, at, typed, ok := triggerBefore(before)
	if !ok {
		return
	}
	start := leaf.Start + document.Len(before[:at])
	if start == m.dismissed {
		return
	}
	if trigger != m.trigger || typed != m.typed || start != m.start || m.items == nil {
		m.items = nil
		switch trigger {
		case ':':
			if emoji != nil && len(typed) >= 2 {
				for _, it := range emoji(typed) {
					m.items = append(m.items, completeItem{icon: it.Emoji, label: ":" + it.Name + ":", insert: it.Emoji})
					if len(m.items) == 8 {
						break
					}
				}
			}
		case '@':
			m.items = mentionDates(now, typed)
			if docs != nil && typed != "" {
				for _, d := range docs(typed) {
					m.items = append(m.items, completeItem{label: d.Name, hint: d.Hint, insert: "[[" + d.Name + "]]"})
					if len(m.items) >= 8 {
						break
					}
				}
			}
		}
		m.index = 0
		m.reveal = true
		m.scroll = ui.ScrollState{}
	}
	m.trigger, m.start, m.typed, m.open = trigger, start, typed, len(m.items) > 0
	if m.open && !wasOpen {
		m.reveal = true
		m.scroll = ui.ScrollState{}
	}
	m.index = max(0, min(m.index, len(m.items)-1))
}

// mentionDates are the dates "@" offers, those whose names start with what
// is typed: in Chinese, pinyin initials or English.
func mentionDates(now time.Time, typed string) []completeItem {
	typed = strings.ToLower(typed)
	var out []completeItem
	for _, d := range []struct {
		name, keys string
		days       int
	}{{i18n.T("今天"), "today jt jintian", 0}, {i18n.T("明天"), "tomorrow mt mingtian", 1}, {i18n.T("昨天"), "yesterday zt zuotian", -1}} {
		match := typed == "" || strings.HasPrefix(d.name, typed)
		for _, k := range strings.Fields(d.keys) {
			match = match || strings.HasPrefix(k, typed)
		}
		if match {
			day := now.AddDate(0, 0, d.days).Format("2006-01-02")
			out = append(out, completeItem{icon: "📅", label: d.name, hint: day, insert: day})
		}
	}
	return out
}

// pick puts the item in place of the trigger and what was typed after it.
func (e *Editor) pick(it completeItem) {
	m := &e.complete
	e.ReplaceText(&ui.TextInputRange{Start: m.start, End: e.Caret}, it.insert)
	m.open, m.items, m.typed = false, nil, ""
	m.dismissed = m.start
}

// completeKeys moves through the list.
func (e *Editor) completeKeys(ev ui.InputEvent) bool {
	m := &e.complete
	if !m.open || e.mark != nil {
		return false
	}
	switch ev.Key {
	case ui.KeyEscape:
		m.open, m.dismissed = false, m.start
	case ui.KeyDown:
		m.index = min(len(m.items)-1, m.index+1)
		m.reveal = true
	case ui.KeyUp:
		m.index = max(0, m.index-1)
		m.reveal = true
	case ui.KeyEnter, ui.KeyTab:
		if len(m.items) > 0 {
			e.pick(m.items[m.index])
		}
	default:
		return false
	}
	return true
}

type completePillMatch struct{ ed *Editor }

// completeMenu is the list under the caret.
func (e *Editor) completeMenu(c *ui.Context, surface ui.Element) {
	m := &e.complete
	if !m.open || !e.focused {
		return
	}
	t := c.Theme()
	bounds := e.surfaceBounds(surface)
	caret, _, _ := e.BoundsForRange(e.Selection().Range)
	windowW, windowH := c.Size()
	const width, rowH = 280, 34
	height := float32(min(len(m.items), 8))*rowH + 40
	anchor := ui.Rect{X: bounds.X + caret.X, Y: bounds.Y + caret.Y, W: caret.W, H: caret.H}
	x, y, maxH := placeMenu(anchor, width, height, 4, windowW, windowH)
	title := i18n.T("表情")
	if m.trigger == '@' {
		title = i18n.T("提及日期或文档")
	}
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("complete-menu")).Absolute().Left(x).Top(y).Width(width).Padding(1)
		e.popover(c, menu).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(e.page.Caption).Padding(8, 12, 2, 12).AlignSelf(ui.Start)
			ui.Scroll(c.Key("complete-scroll")).MaxHeight(min(8*rowH+10, max(rowH, maxH-30))).TrackScroll(&m.scroll).Children(func() {
				topAnchor := ui.Box(c.Key("complete-top")).Height(0)
				var selectedEl ui.Element
				var hasSelected bool
				ui.Column(c).Padding(4, 6, 6, 6).Children(func() {
					for i, it := range m.items {
						selected := i == m.index
						row := ui.ButtonBase(c.Key("complete-row:" + it.label + it.hint)).Height(rowH).PaddingX(8).Gap(8).Radius(4).
							KeepFocus().Label(it.label).Cursor(ui.CursorPointer).AlignItems(ui.Center)
						if !selected && row.Hovered() {
							row.Background(e.page.Hover)
						}
						row.Children(func() {
							if selected {
								slidingPill(c, completePillMatch{e}, e.page.Hover, 4)
							}
							if it.icon != "" {
								ui.Text(c, it.icon).FontSize(16).Width(20).NoWrap()
							} else {
								ui.Box(c).Width(20).Center().Children(func() { ui.Icon(c, iconLink).FontSize(15).TextColor(e.page.Caption) })
							}
							ui.Text(c, it.label).FontSize(14).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
							if it.hint != "" {
								ui.Text(c, it.hint).FontSize(12).TextColor(e.page.Caption).SingleLine().Shrink(1).MinWidth(0)
							}
						})
						if selected {
							selectedEl, hasSelected = row, true
						}
						if row.Clicked() {
							e.pick(it)
							e.RequestFocus = true
						}
					}
				})
				bottomAnchor := ui.Box(c.Key("complete-bottom")).Height(0)
				if m.reveal {
					m.reveal = false
					if len(m.items) > 0 {
						if m.index == 0 {
							topAnchor.ScrollIntoView()
						} else if m.index == len(m.items)-1 {
							bottomAnchor.ScrollIntoView()
						}
						if hasSelected {
							selectedEl.ScrollIntoView()
						}
					}
				}
			})
		})
	})
}
