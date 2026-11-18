package editor

import (
	"sort"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/wiki"
	"github.com/egoist/mygo/ui"
)

// WikiItem is a document offered to complete a [[link]] with: its name and
// where it is.
type WikiItem struct {
	Name, Hint string
}

// wikiState is the completion list of a [[link]] being typed: start is the
// offset of its "[[", typed what follows up to the caret, and dismissed the
// start Escape closed it at.
type wikiState struct {
	open      bool
	start     int
	typed     string
	index     int
	dismissed int
	items     []WikiItem
	reveal    bool
	scroll    ui.ScrollState
}

// wikiSpans splits a run's span where it holds [[links]] and #tags, which
// show as links and as tags; code shows them as written.
func (e *Editor) wikiSpans(span ui.Span, run document.Run, kind string, t *ui.Theme) []ui.Span {
	text := run.Text
	if run.Style.Code || kind == "code" || kind == "raw" || kind == "math" || !strings.ContainsAny(text, "[#") {
		return []ui.Span{span}
	}
	type mark struct {
		start, end int
		tag        bool
	}
	var marks []mark
	for _, l := range wiki.Links(text) {
		marks = append(marks, mark{l.Start, l.End, false})
	}
	for _, tg := range wiki.Tags(text) {
		inside := false
		for _, m := range marks {
			if tg.Start >= m.start && tg.Start < m.end {
				inside = true
			}
		}
		if !inside {
			marks = append(marks, mark{tg.Start, tg.End, true})
		}
	}
	if len(marks) == 0 {
		return []ui.Span{span}
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].start < marks[j].start })
	var out []ui.Span
	piece := func(s string, style func(*ui.Span)) {
		if s == "" {
			return
		}
		sp := span
		sp.Text = s
		if style != nil {
			style(&sp)
		}
		out = append(out, sp)
	}
	last := 0
	for _, m := range marks {
		piece(text[last:m.start], nil)
		if m.tag {
			piece(text[m.start:m.end], func(s *ui.Span) {
				s.Color, s.Background, s.Radius = t.Accent, e.page.TagFill, 4
			})
		} else {
			bracket := func(s *ui.Span) { s.Color = e.page.Caption }
			piece("[[", bracket)
			piece(text[m.start+2:m.end-2], func(s *ui.Span) {
				s.Color, s.Underline, s.DecorationColor = t.Accent, true, t.Accent.Alpha(.35)
			})
			piece("]]", bracket)
		}
		last = m.end
	}
	piece(text[last:], nil)
	return out
}

// runAt is the text of the run at the offset at, and the run's start in
// the document.
func (e *Editor) runAt(at int) (string, int, bool) {
	leaf := e.Engine.LeafAt(at)
	if k := leaf.Block.Kind; k == "code" || k == "raw" || k == "math" {
		return "", 0, false
	}
	offset := leaf.Start
	for _, run := range leaf.Block.Runs {
		n := document.Len(run.Text)
		if at <= offset+n && at >= offset {
			if run.Style.Code {
				return "", 0, false
			}
			return run.Text, offset, true
		}
		offset += n
	}
	return "", 0, false
}

// wikiAt is the target of the [[link]] at the offset at, if any.
func (e *Editor) wikiAt(at int) string {
	text, start, ok := e.runAt(at)
	if !ok {
		return ""
	}
	for _, l := range wiki.Links(text) {
		a, z := start+document.Len(text[:l.Start]), start+document.Len(text[:l.End])
		if at > a && at < z {
			return l.Target
		}
	}
	return ""
}

// tagAt is the #tag at the offset at, if any.
func (e *Editor) tagAt(at int) string {
	text, start, ok := e.runAt(at)
	if !ok {
		return ""
	}
	for _, tg := range wiki.Tags(text) {
		a, z := start+document.Len(text[:tg.Start]), start+document.Len(text[:tg.End])
		if at >= a && at <= z {
			return tg.Name
		}
	}
	return ""
}

// watchWiki opens the completion list while a [[link]] is typed before a
// collapsed caret.
func (e *Editor) watchWiki(suggest func(string) []WikiItem) {
	w := &e.wiki
	wasOpen := w.open
	w.open = false
	if suggest == nil || !e.focused || e.Anchor != e.Caret || e.mark != nil || e.slashOpen {
		return
	}
	leaf := e.Engine.LeafAt(e.Caret)
	if k := leaf.Block.Kind; k == "code" || k == "raw" || k == "math" {
		return
	}
	text := leaf.Block.Text()
	before := text[:document.ByteOffset(text, e.Caret-leaf.Start)]
	at, typed, ok := wiki.Open(before)
	if !ok {
		return
	}
	start := leaf.Start + document.Len(before[:at])
	if start == w.dismissed {
		return
	}
	if typed != w.typed || start != w.start || w.items == nil {
		w.items = suggest(typed)
		if t := strings.TrimSpace(typed); t != "" {
			exact := false
			for _, it := range w.items {
				exact = exact || wiki.Same(it.Name, t)
			}
			if !exact {
				w.items = append(w.items, WikiItem{Name: t, Hint: i18n.T("新建文档")})
			}
		}
		w.index = 0
		w.reveal = true
		w.scroll = ui.ScrollState{}
	}
	w.start, w.typed, w.open = start, typed, len(w.items) > 0
	if w.open && !wasOpen {
		w.reveal = true
		w.scroll = ui.ScrollState{}
	}
	w.index = max(0, min(w.index, len(w.items)-1))
}

// completeWiki finishes the link being typed with the item's name.
func (e *Editor) completeWiki(it WikiItem) {
	w := &e.wiki
	from := w.start + 2
	text := e.Engine.LeafAt(e.Caret).Block.Text()
	leaf := e.Engine.LeafAt(e.Caret)
	after := text[document.ByteOffset(text, e.Caret-leaf.Start):]
	end, insert := e.Caret, it.Name+"]]"
	if strings.HasPrefix(after, "]]") {
		end += 2
	}
	e.ReplaceText(&ui.TextInputRange{Start: from, End: end}, insert)
	w.open, w.items, w.typed = false, nil, ""
	w.dismissed = w.start
}

// wikiKeys moves through the completion list: the arrows step, Enter and
// Tab complete, Escape closes.
func (e *Editor) wikiKeys(ev ui.InputEvent) bool {
	w := &e.wiki
	if !w.open || e.mark != nil {
		return false
	}
	switch ev.Key {
	case ui.KeyEscape:
		w.open, w.dismissed = false, w.start
	case ui.KeyDown:
		w.index = min(len(w.items)-1, w.index+1)
		w.reveal = true
	case ui.KeyUp:
		w.index = max(0, w.index-1)
		w.reveal = true
	case ui.KeyEnter, ui.KeyTab:
		if len(w.items) > 0 {
			e.completeWiki(w.items[w.index])
		}
	default:
		return false
	}
	return true
}

type wikiPillMatch struct{ ed *Editor }

// wikiMenu is the completion list under the caret.
func (e *Editor) wikiMenu(c *ui.Context, surface ui.Element) {
	w := &e.wiki
	if !w.open || !e.focused {
		return
	}
	t := c.Theme()
	bounds := e.surfaceBounds(surface)
	caret, _, _ := e.BoundsForRange(e.Selection().Range)
	windowW, windowH := c.Size()
	const width, rowH = 280, 36
	height := float32(min(len(w.items), 8))*rowH + 40
	anchor := ui.Rect{X: bounds.X + caret.X, Y: bounds.Y + caret.Y, W: caret.W, H: caret.H}
	x, y, maxH := placeMenu(anchor, width, height, 4, windowW, windowH)
	ui.Overlay(c, func() {
		menu := ui.Column(c.Key("wiki-menu")).Absolute().Left(x).Top(y).Width(width).Padding(1)
		e.popover(c, menu).Children(func() {
			ui.Text(c, i18n.T("链接到文档")).FontSize(12).TextColor(e.page.Caption).Padding(8, 12, 2, 12).AlignSelf(ui.Start)
			ui.Scroll(c.Key("wiki-scroll")).MaxHeight(min(8*rowH+10, max(rowH, maxH-30))).TrackScroll(&w.scroll).Children(func() {
				topAnchor := ui.Box(c.Key("wiki-top")).Height(0)
				var selectedEl ui.Element
				var hasSelected bool
				ui.Column(c).Padding(4, 6, 6, 6).Children(func() {
					for i, it := range w.items {
						selected := i == w.index
						row := ui.ButtonBase(c.Key("wiki-row:" + it.Name + it.Hint)).Height(rowH).PaddingX(8).Gap(8).Radius(4).
							KeepFocus().Label(i18n.T("链接到 ") + it.Name).Cursor(ui.CursorPointer).AlignItems(ui.Center)
						if !selected && row.Hovered() {
							row.Background(e.page.Hover)
						}
						row.Children(func() {
							if selected {
								slidingPill(c, wikiPillMatch{e}, e.page.Hover, 4)
							}
							icon := iconLink
							if it.Hint == i18n.T("新建文档") {
								icon = iconPlus
							}
							ui.Icon(c, icon).FontSize(15).TextColor(e.page.Caption)
							ui.Text(c, it.Name).FontSize(14).TextColor(t.Text).SingleLine().Grow(1).MinWidth(0)
							if it.Hint != "" {
								ui.Text(c, it.Hint).FontSize(12).TextColor(e.page.Caption).SingleLine().Shrink(1).MinWidth(0)
							}
						})
						if selected {
							selectedEl, hasSelected = row, true
						}
						if row.Clicked() {
							e.completeWiki(it)
							e.RequestFocus = true
						}
					}
				})
				bottomAnchor := ui.Box(c.Key("wiki-bottom")).Height(0)
				if w.reveal {
					w.reveal = false
					if len(w.items) > 0 {
						if w.index == 0 {
							topAnchor.ScrollIntoView()
						} else if w.index == len(w.items)-1 {
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
