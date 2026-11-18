package editor

import (
	"strings"
	"time"
	"unicode"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/tex"
	"github.com/egoist/mygo/ui"
)

// Bookmarks mark top-level blocks the reader wants to come back to. The
// host keeps them (Options.Bookmarks, ToggleBookmark, LabelBookmark); the
// page shows a mark in its right margin beside each, the outline lists them
// below the headings, and RevealBlock scrolls to one and flashes it.

const (
	// bookmarkMark is the size of the mark beside a bookmarked block.
	bookmarkMark = 20
	// flashHold is how long a revealed block stays marked, and flashFade
	// how long the mark then takes to go.
	flashHold = 400 * time.Millisecond
	flashFade = 800 * time.Millisecond
	// excerptRunes is how much of a block's text an excerpt keeps.
	excerptRunes = 80
)

var (
	iconBookmark       = lucide(`<path d="m19 21-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16z"/>`)
	iconBookmarkFilled = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m19 21-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16z"/></svg>`))
)

// bookmarkState is the page's share of the bookmarks: which blocks have
// one, with the reader's label, and the block being revealed.
type bookmarkState struct {
	marks  map[string]string
	toggle func(id string)
	label  func(id, label string)
	// key is the shortcut that toggles a bookmark, for the mark's tooltip.
	key string
	// ghost is the block whose hollow mark the pointer was on last frame.
	ghost string
	// reveal is the block RevealBlock asked for, scrolled to once laid out;
	// quiet skips moving focus/flashing when syncing reading mode scroll;
	// flash the block lit up since flashAt.
	reveal  string
	quiet   bool
	flash   string
	flashAt time.Time
	// renaming is the outline entry whose label is being typed.
	renaming, name string
	focused        bool
}

// BlockExcerpt is what a block is called where its bookmark is listed: the
// start of its text on one line, or what kind of block it is when it has
// no text, as an image, a table without words or a rule.
func BlockExcerpt(b *document.Block) string {
	var text strings.Builder
	var walk func(*document.Block)
	walk = func(b *document.Block) {
		if t := b.Text(); t != "" {
			if text.Len() > 0 {
				text.WriteByte(' ')
			}
			text.WriteString(t)
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(b)
	s := strings.Join(strings.FieldsFunc(text.String(), unicode.IsSpace), " ")
	if r := []rune(s); len(r) > excerptRunes {
		s = string(r[:excerptRunes]) + "…"
	}
	if s != "" {
		return s
	}
	switch b.Kind {
	case "image":
		if name := b.URL[strings.LastIndex(b.URL, "/")+1:]; name != "" {
			return i18n.T("图片 · ") + name
		}
		return i18n.T("图片")
	case "table":
		return i18n.T("表格")
	case "columns":
		return i18n.T("分栏")
	case "rule":
		return i18n.T("分隔线")
	case "math":
		return i18n.T("公式")
	case "code":
		return i18n.T("代码块")
	}
	return i18n.T("空白段落")
}

// BookmarkTarget is the top-level block a bookmark at the caret marks.
func (e *Editor) BookmarkTarget() *document.Block {
	e.sync()
	return e.blockByID(e.Engine.LeafAt(e.Caret).Owner)
}

// CaretBlock is the index among the top-level blocks of the caret's.
func (e *Editor) CaretBlock() int {
	id := e.Engine.LeafAt(e.Caret).Owner
	for i, b := range e.Doc.Blocks {
		if b.ID == id {
			return i
		}
	}
	return 0
}

// RevealBlock puts the caret at the start of the top-level block, unfolds
// what hides it, scrolls to it and lights it up for a moment.
func (e *Editor) RevealBlock(id string) {
	if e.blockByID(id) == nil {
		return
	}
	e.Engine.Reveal(id)
	e.Engine.FocusBlock(id)
	e.sync()
	e.bookmarks.reveal = id
	e.bookmarks.quiet = false
	e.RequestFocus = true
}

// ScrollToBlock places the top-level block near the top of the viewport once
// laid out, without stealing selection or flashing it.
func (e *Editor) ScrollToBlock(id string) {
	if id == "" || e.blockByID(id) == nil {
		return
	}
	e.bookmarks.reveal = id
	e.bookmarks.quiet = true
}

// ActiveOrTopBlockID returns the top-level block owning the caret when it is
// currently in view, or else the topmost block visible in the viewport.
func (e *Editor) ActiveOrTopBlockID() string {
	owner := e.Engine.LeafAt(e.Caret).Owner
	if len(e.handles) == 0 {
		if owner != "" {
			return owner
		}
		if len(e.Doc.Blocks) > 0 {
			return e.Doc.Blocks[0].ID
		}
		return ""
	}
	viewY := e.Scroll.Y
	if e.scrollGlide.active {
		viewY = e.scrollGlide.to
	}
	if owner != "" && e.Caret > 0 && viewY == 0 {
		return owner
	}
	if owner != "" && e.viewport > 0 {
		if span, ok := e.blockSpan(owner); ok {
			if span.Y+span.H >= viewY-32 && span.Y <= viewY+e.viewport+32 {
				return owner
			}
		}
	}
	for j := range e.handles {
		h := *e.indexedHandle(j)
		bottom := e.contentEnd
		if j+1 < len(e.handles) {
			bottom = e.indexedHandle(j + 1).top
		}
		if bottom >= viewY+16 {
			return h.id
		}
	}
	return e.handles[len(e.handles)-1].id
}

// revealPending scrolls to the block RevealBlock asked for, once the page
// is laid out for it.
func (e *Editor) revealPending(c *ui.Context) {
	b := &e.bookmarks
	if b.reveal == "" {
		return
	}
	id, quiet := b.reveal, b.quiet
	b.reveal, b.quiet = "", false
	if hi, ok := e.handleIDs[id]; ok {
		h := *e.indexedHandle(hi)
		if quiet {
			e.scrollGlide = scrollGlideState{}
			e.Scroll.Y = max(0, h.top-48)
			c.After(0)
			return
		}
		e.jumpTo(c, id, h.top)
		b.flash, b.flashAt = id, c.Now()
		c.After(0)
		return
	}
}

// blockSpan is the band of the page a top-level block takes, from its top
// to the next block's.
func (e *Editor) blockSpan(id string) (ui.Rect, bool) {
	if j, ok := e.handleIDs[id]; ok {
		h := *e.indexedHandle(j)
		bottom := e.contentEnd
		if j+1 < len(e.handles) {
			bottom = e.indexedHandle(j + 1).top
		}
		return ui.Rect{X: e.padding - 8, Y: h.top - 4, W: e.width - 2*e.padding + 16, H: bottom - h.top}, true
	}
	return ui.Rect{}, false
}

func (e *Editor) flashBlock(id string) {
	if id == "" {
		return
	}
	e.bookmarks.flash = id
	e.bookmarks.flashAt = time.Time{}
}

// paintFlash lights up the block just revealed, as a find match is lit,
// then lets it fade.
func (e *Editor) paintFlash(p *ui.Painter, bounds ui.Rect) {
	b := &e.bookmarks
	if b.flash == "" {
		return
	}
	if b.flashAt.IsZero() {
		b.flashAt = p.Now()
	}
	since := p.Now().Sub(b.flashAt)
	if since >= flashHold+flashFade {
		b.flash = ""
		return
	}
	r, ok := e.blockSpan(b.flash)
	if !ok {
		return
	}
	k := float32(1)
	if since > flashHold {
		k = 1 - float32(since-flashHold)/float32(flashFade)
	}
	color := e.page.FindMatch
	r.X += bounds.X
	r.Y += bounds.Y + e.ownerVisualDY(b.flash)
	p.Fill(r, color.Alpha(float32(color.A)/255*k*.7), 6)
	p.After(0)
}

// bookmarkMarks draws the mark beside each bookmarked block, right of the
// text column: a click takes the bookmark away. The block under the pointer
// shows a hollow mark in the same place, which adds one, as the + and the
// grip show left of it. They are elements beside the surface, not painted
// on it, for their tooltips, and so that they work in a read-only page too.
func (e *Editor) bookmarkMarks(c *ui.Context) {
	b := &e.bookmarks
	if b.toggle == nil {
		b.ghost = ""
		return
	}
	if len(b.marks) == 0 && e.hoverID == "" && b.ghost == "" {
		return
	}
	t := c.Theme()
	x := e.width - e.padding + 12
	ghost := ""
	winTop := e.Scroll.Y - 1200
	winBottom := e.Scroll.Y + max(e.viewport, 900) + 64
	hs, he := e.visibleHandleRange(winTop, winBottom)
	for hi := hs; hi < he; hi++ {
		h := *e.indexedHandle(hi)
		_, marked := b.marks[h.id]
		// The hollow mark stays while the pointer is on it, though the
		// pointer has left the block for the margin.
		hover := !marked && e.dragID == "" && (h.id == e.hoverID || h.id == b.ghost)
		if !marked && !hover {
			continue
		}
		if h.rect.Y+bookmarkMark < e.Scroll.Y || h.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		id := h.id
		top := h.rect.Y + 2
		if blk := e.blockByID(id); blk != nil && blk.Kind == "table" {
			// A table's handle sits by its column bar; the mark goes by its
			// first row.
			top = h.top + 32
		}
		label, tip, icon := i18n.T("移除书签"), e.bookmarkTip(i18n.T("书签 · 点击移除")), iconBookmarkFilled
		color := t.Accent.Alpha(.75)
		if !marked {
			label, tip, icon = i18n.T("添加书签"), e.bookmarkTip(i18n.T("添加书签")), iconBookmark
			color = t.Text.Alpha(.3)
		}
		mark := ui.ButtonBase(c.Key("bookmark-mark:"+id)).Absolute().Left(x).Top(top).Size(bookmarkMark, bookmarkMark).
			Radius(4).Center().Label(label).Tooltip(tip).KeepFocus().Cursor(ui.CursorPointer)
		if mark.Hovered() {
			mark.Background(t.Text.Alpha(.06))
			color = t.Accent
			if !marked {
				ghost = id
			}
		}
		mark.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(color) })
		if mark.Clicked() {
			b.toggle(id)
		}
	}
	b.ghost = ghost
}

// bookmarkTip is a mark's tooltip, with the key that does the same.
func (e *Editor) bookmarkTip(tip string) string {
	if e.bookmarks.key != "" {
		tip += " (" + e.bookmarks.key + ")"
	}
	return tip
}

// bookmarkSection lists the page's bookmarks below the outline's headings,
// in the order of the page: a click goes to one, its × or its menu takes
// it away, and its menu names it.
func (e *Editor) bookmarkSection(c *ui.Context) {
	b := &e.bookmarks
	if len(b.marks) == 0 {
		return
	}
	var blocks []*document.Block
	for _, block := range e.Doc.Blocks {
		if _, ok := b.marks[block.ID]; ok {
			blocks = append(blocks, block)
		}
	}
	if len(blocks) == 0 {
		return
	}
	t := c.Theme()
	ui.Row(c.Key("outline-bookmarks")).Height(outlineRow).Padding(0, 8, 0, 8).Margin(12, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		ui.Text(c, i18n.T("书签 · ")+itoa(len(blocks))).FontSize(12).FontWeight(500).TextColor(t.Text.Alpha(.5))
	})
	for _, block := range blocks {
		id := block.ID
		title := b.marks[id]
		var formula *tex.Box
		height := float32(outlineRow)
		if title == "" {
			title = BlockExcerpt(block)
			if block.Kind == "math" && strings.TrimSpace(block.Text()) != "" {
				// The excerpt is for searching and recovering the bookmark;
				// render from the complete source, never its shortened text.
				s := e.formula(id, strings.TrimSpace(block.Text()))
				if s.err == nil && s.box.W > 0 {
					formula = &s.box
					height = max(height, min(64, (s.box.Ascent+s.box.Descent)*14/mathSize+8))
				}
			}
		}
		if b.renaming == id {
			input := ui.TextInput(c.Key("bookmark-name:"+id), &b.name).Placeholder(BlockExcerpt(block)).
				Label(i18n.T("书签名称")).FontSize(14).Padding(4, 8)
			// The field asks for the focus until it has it; only then does
			// leaving it keep the name.
			if !b.focused {
				input.Focus()
			}
			left := b.focused && !input.Focused()
			if input.Focused() {
				b.focused = true
			}
			switch {
			case input.Submitted():
				b.label(id, strings.TrimSpace(b.name))
				b.renaming = ""
			case c.Shortcut(0, ui.KeyEscape):
				b.renaming = ""
			case left:
				// Leaving the field keeps what was typed, as the sidebar's
				// rename does.
				b.label(id, strings.TrimSpace(b.name))
				b.renaming = ""
			}
			continue
		}
		row := ui.ButtonBase(c.Key("outline-bookmark:"+id)).Height(height).Radius(6).Padding(0, 4, 0, 8).
			Label(i18n.T("跳到书签 ") + title).Tooltip(title).KeepFocus().Cursor(ui.CursorPointer)
		hovered := row.Hovered()
		if hovered {
			row.Background(t.Text.Alpha(.05))
		}
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item(i18n.T("重命名…")).Chosen() {
				b.renaming, b.name, b.focused = id, b.marks[id], false
			}
			if m.Item(i18n.T("移除书签")).Chosen() {
				b.toggle(id)
			}
		})
		removed := false
		row.Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconBookmark).FontSize(13).TextColor(t.Accent.Alpha(.8))
				if formula != nil {
					ui.Box(c.Key("bookmark-formula:" + id)).Height(height).MinWidth(0).Shrink(1).Grow(1).
						Draw(func(p *ui.Painter, r ui.Rect) { e.paintBookmarkFormula(p, r, *formula, t) })
				} else {
					ui.Text(c, title).FontSize(14).TextColor(t.Text.Alpha(.72)).SingleLine().MinWidth(0).Shrink(1).Grow(1)
				}
				if hovered {
					x := ui.ButtonBase(c.Key("outline-bookmark-remove:"+id)).Size(20, 20).Radius(4).Center().
						Label(i18n.T("移除书签")).Tooltip(i18n.T("移除书签")).KeepFocus().Cursor(ui.CursorPointer)
					color := t.Text.Alpha(.45)
					if x.Hovered() {
						x.Background(t.Text.Alpha(.08))
						color = t.Text.Alpha(.85)
					}
					x.Children(func() { ui.Icon(c, iconClose).FontSize(12).TextColor(color) })
					if x.Clicked() {
						removed = true
					}
				}
			})
		})
		switch {
		case removed:
			b.toggle(id)
		case row.Clicked():
			e.RevealBlock(id)
		}
	}
}

// paintBookmarkFormula keeps the formula readable in the outline. Tall
// formulas fit the row; wide ones end in an ellipsis instead of shrinking
// every symbol to fit. Clipping also leaves room for the remove button.
func (e *Editor) paintBookmarkFormula(p *ui.Painter, r ui.Rect, box tex.Box, theme *ui.Theme) {
	if r.W <= 0 || r.H <= 8 {
		return
	}
	t := *theme
	t.Text = t.Text.Alpha(.72)
	scale := min(14/mathSize, (r.H-8)/max(1, box.Ascent+box.Descent))
	y := r.Y + (r.H-(box.Ascent+box.Descent)*scale)/2
	clip := r
	var ellipsis *ui.TextLayout
	if box.W*scale > r.W {
		ellipsis = ui.ShapeText("…", ui.Font{Family: "system-ui", Size: 14}, 0)
		w, _ := ellipsis.Size()
		clip.W = max(0, clip.W-w-2)
	}
	p.Clip(clip, 0, func() {
		p.Transform(ui.Scaling(scale, scale), r.X, y, func() {
			e.paintFormula(p, box, r.X, y+box.Ascent, &t)
		})
	})
	if ellipsis != nil {
		w, h := ellipsis.Size()
		p.Clip(r, 0, func() { p.TextLayout(ellipsis, r.X+r.W-w, r.Y+(r.H-h)/2, t.Text) })
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
