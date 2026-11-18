package editor

import (
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// The page's header, as Notion's: an optional cover across the top of the
// card and an optional icon, an emoji, above the title. Pointing at the top
// of the page offers to add whichever is missing.

const (
	// coverHeight is a gradient cover's height, and the least of a picture
	// cover's, which follows the picture's proportions up to coverMax.
	coverHeight = 200
	coverMax    = 360
	iconSize    = 64
)

// pageEmoji are the icons offered for a page.
var pageEmoji = []string{
	"📄", "📝", "📒", "📓", "📔", "📕", "📗", "📘", "📙", "📚",
	"💡", "🚀", "🎯", "📌", "📍", "⭐", "🌟", "✨", "🔥", "🌈",
	"🌱", "🌿", "🍀", "🌸", "🌻", "🍎", "☕", "🍵", "🎨", "🎵",
	"🎬", "📷", "🧭", "🗺️", "🏠", "🏔️", "🌊", "☀️", "🌙", "⛅",
	"💼", "📊", "📈", "🗂️", "🧪", "🔬", "🛠️", "⚙️", "🔒", "❤️",
	"✅", "❓", "💬", "🎉", "🎁", "🏆", "🧠", "👋", "🙂", "🐱",
}

// headerState is where the header was laid out and which of its pickers
// is open.
type headerState struct {
	// iconRect is the icon's box and coverRect the cover's, empty when
	// there is none; titleTop is where the first block starts.
	iconRect, coverRect ui.Rect
	titleTop            float32
	coverImage          *ui.Bitmap
	iconOpen, coverOpen bool
	pickCover           func(done func(url string))
	// moving is set while the picture cover is being repositioned, movingY
	// the part shown so far (Document.CoverY), saved or dropped at the end.
	moving  bool
	movingY float64
}

// layoutHeader lays the cover and the icon out and returns where the
// blocks start.
func (e *Editor) layoutHeader(o Options) float32 {
	h := &e.header
	h.iconRect, h.coverRect, h.coverImage = ui.Rect{}, ui.Rect{}, nil
	y := float32(pageTop)
	if cover := e.Doc.Cover; cover != "" {
		h.coverRect = ui.Rect{X: 0, Y: 0, W: o.Width, H: coverHeight}
		if !strings.HasPrefix(cover, "gradient:") {
			bitmap := e.imageResource(cover, o).Bitmap
			h.coverImage = bitmap
			if bitmap != nil {
				// As much of the picture as its proportions ask for, within
				// bounds: a wide one whole, a tall one a band of it.
				if iw, ih := bitmap.Size(); iw > 0 && ih > 0 {
					h.coverRect.H = min(max(o.Width*float32(ih)/float32(iw), coverHeight), coverMax)
				}
			}
		}
		y = h.coverRect.H + 40
	}
	if !h.coverMovable() {
		h.moving = false
	}
	if e.Doc.Icon != "" {
		top := y
		if e.Doc.Cover != "" {
			top = h.coverRect.H - iconSize/2
		}
		h.iconRect = ui.Rect{X: e.padding - 4, Y: top, W: iconSize + 8, H: iconSize + 8}
		y = top + iconSize + 24
	}
	h.titleTop = y
	return y
}

// coverPlace is where the picture cover is drawn for r, the band: scaled to
// fill it, centred across, and moved up or down by y (Document.CoverY) as
// far as the picture reaches past it.
func coverPlace(b *ui.Bitmap, r ui.Rect, y float64) ui.Rect {
	iw, ih := b.Size()
	if iw <= 0 || ih <= 0 {
		return r
	}
	s := max(r.W/float32(iw), r.H/float32(ih))
	w, h := float32(iw)*s, float32(ih)*s
	return ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)*float32(y+1)/2, W: w, H: h}
}

// coverOverflow is how much taller than its band the picture cover is
// drawn, which repositioning moves through.
func (h *headerState) coverOverflow() float32 {
	if h.coverImage == nil {
		return 0
	}
	return coverPlace(h.coverImage, h.coverRect, 0).H - h.coverRect.H
}

// coverMovable reports whether the cover is a picture with more of it than
// its band shows.
func (h *headerState) coverMovable() bool { return h.coverOverflow() > 1 }

// coverY is the part of the picture cover shown: the document's, or the
// one being chosen.
func (e *Editor) coverY() float64 {
	if e.header.moving {
		return e.header.movingY
	}
	return e.Doc.CoverY
}

// paintHeader draws the cover and the icon.
func (e *Editor) paintHeader(p *ui.Painter, bounds ui.Rect) {
	h := &e.header
	if h.coverRect.H > 0 && h.coverRect.Y+h.coverRect.H >= e.Scroll.Y {
		r := h.coverRect
		r.X += bounds.X
		r.Y += bounds.Y
		switch {
		case h.coverImage != nil:
			place := coverPlace(h.coverImage, r, e.coverY())
			p.Clip(r, 0, func() { p.Image(h.coverImage, place, ui.Cover) })
		default:
			colors, ok := e.page.Covers[strings.TrimPrefix(e.Doc.Cover, "gradient:")]
			if !ok {
				colors = [2]ui.Color{e.page.Hover, e.page.Hover}
			}
			p.FillGradient(r, ui.LinearGradient{Angle: 120, From: colors[0], To: colors[1], Oklab: true}, 0)
		}
	}
	if h.iconRect.H > 0 {
		r := h.iconRect
		r.X += bounds.X
		r.Y += bounds.Y
		if !e.printing && (h.iconOpen || e.hovered(h.iconRect, 0)) {
			p.Fill(r, e.page.Hover, 8)
		}
		span := ui.Span{Text: e.Doc.Icon, Size: iconSize * .82}
		w, th := p.MeasureText(0, span)
		p.RichText(r.X+(r.W-w)/2, r.Y+(r.H-th)/2, 0, span)
	}
}

// headerShown reports whether the pointer is at the top of the page, where
// the header offers its buttons.
func (e *Editor) headerShown() bool {
	return e.header.iconOpen || e.header.coverOpen || e.header.moving ||
		e.pointerInside && e.pointer.Y < e.header.titleTop+56 && e.pointer.Y >= e.Scroll.Y-1
}

// headerButton is a quiet text button of the header.
func (e *Editor) headerButton(c *ui.Context, key string, icon *ui.SVG, label string, solid bool) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Height(28).PaddingX(8).Gap(6).Radius(6).AlignItems(ui.Center).
		Label(label).KeepFocus().Cursor(ui.CursorPointer)
	if solid {
		b.Background(t.Background.Alpha(.86))
	}
	if b.Hovered() {
		b.Background(e.page.Hover)
		if solid {
			b.Background(t.Background)
		}
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(e.page.Caption)
		ui.Text(c, label).FontSize(13).TextColor(e.page.Caption)
	})
	return b
}

// headerControls are the header's buttons, in the surface's coordinates:
// add an icon or a cover above the title, change or remove the cover on
// it, and the icon itself, which opens its picker.
func (e *Editor) headerControls(c *ui.Context) {
	if e.printing {
		return
	}
	h := &e.header
	if h.iconRect.H > 0 {
		b := ui.ButtonBase(c.Key("page-icon")).Absolute().Left(h.iconRect.X).Top(h.iconRect.Y).Size(h.iconRect.W, h.iconRect.H).
			Radius(8).Label(i18n.T("更换图标")).Tooltip(i18n.T("更换图标")).KeepFocus().Cursor(ui.CursorPointer)
		if b.Clicked() {
			h.iconOpen, h.coverOpen = !h.iconOpen, false
		}
	}
	if !e.headerShown() {
		return
	}
	if h.moving {
		e.coverMover(c)
		return
	}
	if h.coverRect.H > 0 && (e.hovered(h.coverRect, 0) || h.coverOpen) {
		ui.Row(c.Key("cover-buttons")).Absolute().Right(e.padding).Top(h.coverRect.H - 40).Gap(6).Children(func() {
			if h.coverMovable() && e.headerButton(c, "cover-move", iconMoveVertical, i18n.T("调整位置"), true).Clicked() {
				h.moving, h.movingY, h.coverOpen, h.iconOpen = true, e.Doc.CoverY, false, false
			}
			if e.headerButton(c, "cover-change", iconImage, i18n.T("更换封面"), true).Clicked() {
				h.coverOpen, h.iconOpen = !h.coverOpen, false
			}
			if e.headerButton(c, "cover-remove", iconTrash, i18n.T("移除封面"), true).Clicked() {
				e.Engine.SetCover("")
				h.coverOpen = false
			}
		})
	}
	if e.Doc.Icon != "" && e.Doc.Cover != "" {
		return
	}
	top := float32(12)
	switch {
	case e.Doc.Cover != "":
		top = h.coverRect.H + 8
	case e.Doc.Icon != "":
		top = h.iconRect.Y - 36
	}
	ui.Row(c.Key("header-add")).Absolute().Left(e.padding - 8).Top(max(4, top)).Gap(4).Children(func() {
		if e.Doc.Icon == "" && e.headerButton(c, "icon-add", iconSmile, i18n.T("添加图标"), false).Clicked() {
			e.Engine.SetPageIcon(pageEmoji[0])
			h.iconOpen, h.coverOpen = true, false
		}
		if e.Doc.Cover == "" && e.headerButton(c, "cover-add", iconImage, i18n.T("添加封面"), false).Clicked() {
			e.Engine.SetCover("gradient:" + document.CoverGradients[0])
			h.coverOpen, h.iconOpen = true, false
		}
	})
}

// headerPopups are the icon's and the cover's pickers.
func (e *Editor) headerPopups(c *ui.Context, surface ui.Element) {
	h := &e.header
	if e.Doc.Icon == "" {
		h.iconOpen = false
	}
	if e.Doc.Cover == "" {
		h.coverOpen = false
	}
	if !h.iconOpen && !h.coverOpen || e.printing {
		return
	}
	t := c.Theme()
	bounds := surface.Bounds()
	ui.Overlay(c, func() {
		if h.iconOpen {
			x := bounds.X + h.iconRect.X
			y := bounds.Y + h.iconRect.Y + h.iconRect.H + 6
			panel := ui.Column(c.Key("page-icons")).Absolute().Left(x).Top(y).Width(318).Padding(10).Gap(8)
			e.popover(c, panel).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					ui.Text(c, i18n.T("页面图标")).FontSize(12).TextColor(t.Text).Grow(1)
					if e.headerButton(c, "icon-remove", iconTrash, i18n.T("移除图标"), false).Clicked() {
						e.Engine.SetPageIcon("")
						h.iconOpen = false
					}
				})
				ui.Column(c).Gap(2).Children(func() {
					for row := 0; row < len(pageEmoji); row += 10 {
						ui.Row(c.Key(row)).Gap(2).Children(func() {
							for _, emoji := range pageEmoji[row:min(row+10, len(pageEmoji))] {
								cell := ui.ButtonBase(c.Key("page-emoji:"+emoji)).Size(28, 28).Radius(4).Center().
									Label(i18n.T("图标：") + emoji).KeepFocus().Cursor(ui.CursorPointer)
								if e.Doc.Icon == emoji {
									cell.Background(e.page.TurnActive)
								} else if cell.Hovered() {
									cell.Background(e.page.Hover)
								}
								cell.Children(func() { ui.Text(c, emoji).FontSize(18) })
								if cell.Clicked() {
									e.Engine.SetPageIcon(emoji)
									h.iconOpen = false
								}
							}
						})
					}
				})
			})
		}
		if h.coverOpen {
			x := bounds.X + bounds.W - e.padding - 300
			y := bounds.Y + h.coverRect.H - e.Scroll.Y + 2
			panel := ui.Column(c.Key("page-covers")).Absolute().Left(max(8, x)).Top(y).Width(300).Padding(10).Gap(10)
			e.popover(c, panel).Children(func() {
				ui.Text(c, i18n.T("封面")).FontSize(12).TextColor(t.Text)
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, name := range document.CoverGradients {
						colors := e.page.Covers[name]
						value := "gradient:" + name
						sw := ui.ButtonBase(c.Key("cover:"+name)).Size(82, 44).Radius(6).Label(i18n.T("封面：") + coverName(name)).
							Tooltip(coverName(name)).KeepFocus().Cursor(ui.CursorPointer)
						on := e.Doc.Cover == value
						hovered := sw.Hovered()
						sw.Draw(func(p *ui.Painter, r ui.Rect) {
							p.FillGradient(r, ui.LinearGradient{Angle: 120, From: colors[0], To: colors[1], Oklab: true}, 6)
							if on {
								p.Stroke(r, t.Accent, 6, 2)
							} else if hovered {
								p.Stroke(r, t.Text.Alpha(.25), 6, 1)
							}
						})
						if sw.Clicked() {
							e.Engine.SetCover(value)
						}
					}
				})
				if h.pickCover != nil && e.headerButton(c, "cover-upload", iconImage, i18n.T("使用图片…"), false).Clicked() {
					h.coverOpen = false
					e.header.pickCover(func(url string) {
						if url != "" {
							e.Engine.SetCover(url)
						}
					})
				}
			})
		}
	})
}

// coverName names a cover gradient for people.
func coverName(name string) string {
	switch name {
	case "dawn":
		return i18n.T("晨光")
	case "sea":
		return i18n.T("海")
	case "forest":
		return i18n.T("森林")
	case "dusk":
		return i18n.T("暮色")
	case "sand":
		return i18n.T("沙")
	case "ink":
		return i18n.T("墨")
	}
	return name
}

// closeHeader puts the header's pickers away.
func (e *Editor) closeHeader() bool {
	open := e.header.iconOpen || e.header.coverOpen || e.header.moving
	e.header.iconOpen, e.header.coverOpen, e.header.moving = false, false, false
	return open
}

// coverMover repositions the picture cover, as Notion's: the picture
// follows the pointer dragging it up or down, and is kept with 保存位置 or
// dropped with 取消 or Escape.
func (e *Editor) coverMover(c *ui.Context) {
	h := &e.header
	r := h.coverRect
	pad := ui.Box(c.Key("cover-mover")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
		Role(ui.RoleSlider).Label(i18n.T("封面位置")).KeepFocus().Cursor(ui.CursorGrab)
	if _, dy, held := pad.Dragged(); held {
		pad.Cursor(ui.CursorGrabbing)
		if over := h.coverOverflow(); over > 0 {
			// The picture moves with the pointer: dragging it down shows
			// more of its top.
			h.movingY = min(max(h.movingY-2*float64(dy/over), -1), 1)
		}
	}
	pad.Children(func() {
		ui.Box(c.Key("cover-mover-hint")).Absolute().Left(r.W/2-90).Top(r.H/2-16).Size(180, 32).Radius(16).
			Center().Background(e.page.Scrim).Children(func() {
			ui.Text(c, i18n.T("拖动图片以调整位置")).FontSize(13).TextColor(e.page.OnScrim)
		})
	})
	ui.Row(c.Key("cover-buttons")).Absolute().Right(e.padding).Top(r.H - 40).Gap(6).Children(func() {
		if e.headerButton(c, "cover-save", iconCheck, i18n.T("保存位置"), true).Clicked() {
			h.moving = false
			e.Engine.SetCoverY(h.movingY)
		}
		if e.headerButton(c, "cover-cancel", iconClose, i18n.T("取消"), true).Clicked() {
			h.moving = false
		}
	})
}
