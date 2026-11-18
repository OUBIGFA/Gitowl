package editor

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// ImageProblem distinguishes recovery actions from an opaque decode failure.
type ImageProblem uint8

const (
	ImageInvalid ImageProblem = iota
	ImageMissing
	ImageUnsupported
	ImageUnreadable
	ImageRemote
	ImageTooLarge
)

// ImageResource is a decoded picture, a picture whose header dimensions are
// already known while its pixels decode in the background, or the reason it
// could not be loaded. Local means the host can reveal its file or the nearest
// existing folder.
type ImageResource struct {
	Bitmap        *ui.Bitmap
	Width, Height int
	Loading       bool
	LoadedAt      time.Time
	Problem       ImageProblem
	Local         bool
	// Frames and Delays animate a GIF: Bitmap is its first frame, which
	// prints, and shows when the system reduces motion.
	Frames []*ui.Bitmap
	Delays []time.Duration
}

// InvalidateImage drops a cached image entry (such as a placeholder whose
// background decode just finished) so the next frame picks up the new resource.
func (e *Editor) InvalidateImage(src string) {
	if e.images != nil {
		delete(e.images, src)
	}
	e.ready = false
}

type imageEnterState struct {
	id      string
	start   time.Time
	pending bool
}

// FocusInsertedImage focuses a freshly inserted picture's caption line (with
// its default filename alt selected so typing immediately writes a caption)
// and triggers its Polaroid-to-page spring expansion.
func (e *Editor) FocusInsertedImage(id string) {
	e.sync()
	leaf, ok := e.Engine.LeafByID(id)
	if !ok {
		e.RequestFocus = true
		return
	}
	e.SetSelection(leaf.Start, leaf.End)
	e.imageCaptionID = id
	e.imageEnter = imageEnterState{id: id, pending: true}
	e.ready = false
	e.RequestFocus = true
	e.reveal = true
}

func (e *Editor) captionVisible(b *document.Block) bool {
	if e.printing || b == nil {
		return false
	}
	if e.imageCaptionID == b.ID {
		return true
	}
	return e.captionedImages[b.ID] && strings.TrimSpace(b.Text()) != ""
}

// frameAt is the frame of an animated picture at now, and how long until
// the next; a still picture is its bitmap, for ever.
func (r ImageResource) frameAt(now time.Time) (*ui.Bitmap, time.Duration) {
	if len(r.Frames) < 2 || len(r.Delays) != len(r.Frames) {
		return r.Bitmap, 0
	}
	var total time.Duration
	for _, d := range r.Delays {
		total += d
	}
	if total <= 0 {
		return r.Bitmap, 0
	}
	at := time.Duration(now.UnixNano()) % total
	for i, d := range r.Delays {
		if at < d {
			return r.Frames[i], d - at
		}
		at -= d
	}
	return r.Frames[0], r.Delays[0]
}

// imageFigure owns the geometry shared by drawing, hit targets and printing.
// Like Textus, figures are centered at their natural size, constrained only by
// the column; failed pictures become a compact, actionable dashed card.
type imageFigure struct {
	resource           ImageResource
	bounds             ui.Rect
	name               string
	title, alt, reason *ui.TextLayout
	altY, reasonY      float32
	actions            []imageAction
}

type imageAction struct {
	key, label string
	bounds     ui.Rect
}

type imageViewer struct {
	open, actual bool
	zoom         float32
	pan          ui.Point
	shownScale   float32
	shownPan     ui.Point
	zoomAtTime   time.Time
	openedAt     time.Time
	origin       ui.Rect
	name         string
	bitmap       *ui.Bitmap
	scroll       ui.ScrollState
}

// ReplaceImage preserves the current composition and makes the replacement a
// separate undo step. Invalidating both URLs also clears a cached load failure.
func (e *Editor) ReplaceImage(id, expected, next string) bool {
	e.UnmarkText()
	if !e.Engine.ReplaceImage(id, expected, next) {
		return false
	}
	delete(e.images, expected)
	delete(e.images, next)
	e.ready = false
	e.sync()
	return true
}

func imageName(src string) string {
	if u, err := url.Parse(src); err == nil && u.Host != "" {
		src = u.EscapedPath()
		if src == "" || src == "/" {
			return u.Host
		}
	}
	if decoded, err := url.PathUnescape(src); err == nil {
		src = decoded
	}
	if name := path.Base(src); name != "." && name != "/" && name != "" {
		return name
	}
	return i18n.T("未命名图片")
}

func (p ImageProblem) text() string {
	switch p {
	case ImageMissing:
		return i18n.T("这个路径下没有文件。")
	case ImageUnsupported:
		return i18n.T("文件存在，但格式不受支持或图片已损坏。")
	case ImageUnreadable:
		return i18n.T("无法读取图片，请检查文件及访问权限。")
	case ImageRemote:
		return i18n.T("未能直接连接。可使用当前网络重新加载。")
	case ImageTooLarge:
		return i18n.T("图片过大，无法预览。")
	default:
		return i18n.T("图片路径无效，请重新选择图片。")
	}
}

func (e *Editor) imageResource(src string, o Options) ImageResource {
	if resource, ok := e.images[src]; ok && !resource.Loading {
		return resource
	}
	resource := ImageResource{}
	if o.LoadImage != nil {
		resource = o.LoadImage(src)
		e.images[src] = resource
	}
	return resource
}

func (e *Editor) layoutImage(r *row, o Options) float32 {
	b := r.leaf.Block
	f := &imageFigure{resource: e.imageResource(b.URL, o), name: imageName(b.URL)}
	r.picture = f
	x, y, w := r.rect.X, r.rect.Y+16, r.rect.W
	iw, ih := 0, 0
	if bitmap := f.resource.Bitmap; bitmap != nil {
		iw, ih = bitmap.Size()
	} else if f.resource.Loading && f.resource.Width > 0 && f.resource.Height > 0 {
		iw, ih = f.resource.Width, f.resource.Height
	}
	if iw > 0 && ih > 0 {
		scale := min(float32(1), w/float32(max(1, iw)))
		preferred := b.ImageWidth
		if e.imageResize.id == b.ID {
			preferred = e.imageResize.width
		}
		if preferred > 0 {
			scale = min(w, preferred) / float32(max(1, iw))
		}
		if e.printing {
			// A whole picture fits an A4 page; the interactive view has no
			// fixed height limit, so tall screenshots remain readable.
			scale = min(scale, 850/float32(max(1, ih)))
		}
		pw, ph := float32(iw)*scale, float32(ih)*scale
		f.bounds = ui.Rect{X: x + (w-pw)/2, Y: y, W: pw, H: ph}
		r.x, r.y = x, y+ph+8
		bottom := y + ph
		if e.captionVisible(b) {
			_, h := r.layout.Size()
			bottom = r.y + max(20, h)
		}
		r.rect.H = bottom - r.rect.Y + 18
		return r.rect.H
	}
	if f.resource.Loading {
		ph := min(float32(220), max(96, w*0.42))
		f.bounds = ui.Rect{X: x, Y: y, W: w, H: ph}
		r.x, r.y = x, y+ph+8
		r.rect.H = y + ph - r.rect.Y + 18
		return r.rect.H
	}
	title := i18n.T("无法显示图片：") + f.name
	if f.resource.Problem == ImageMissing {
		title = i18n.T("找不到图片：") + f.name
	}
	alt := strings.TrimSpace(b.Text())
	if alt == f.name {
		alt = ""
	}
	headFont := ui.Font{Family: "system-ui", Size: 14, Weight: 600, LineHeight: 1.45}
	altFont := ui.Font{Family: "system-ui", Size: 14, Italic: true, LineHeight: 1.45}
	reasonFont := ui.Font{Family: "system-ui", Size: 13, LineHeight: 1.45}
	natural := func(text string, font ui.Font) float32 {
		l := ui.ShapeText(text, font, 0)
		width, _ := l.Size()
		return width
	}
	cardW := min(w, max(natural(title, headFont)+22, natural(alt, altFont), natural(f.resource.Problem.text(), reasonFont), 250)+28)
	cardW = min(cardW, 640)
	f.bounds = ui.Rect{X: x + (w-cardW)/2, Y: y, W: cardW}
	inner := max(1, cardW-28)
	f.title = ui.ShapeText(title, headFont, max(1, inner-22))
	_, headH := f.title.Size()
	cursor := float32(10) + max(18, headH)
	if alt != "" {
		cursor += 2
		f.altY = cursor
		f.alt = ui.ShapeText(alt, altFont, inner)
		_, h := f.alt.Size()
		cursor += h
	}
	f.reasonY = cursor + 2
	f.reason = ui.ShapeText(f.resource.Problem.text(), reasonFont, inner)
	_, reasonH := f.reason.Size()
	cursor = f.reasonY + reasonH
	if !e.printing {
		var actions []imageAction
		if o.RetryImage != nil && f.resource.Problem == ImageRemote {
			actions = append(actions, imageAction{key: "retry", label: i18n.T("重新加载")})
		}
		if o.ReplaceImage != nil {
			actions = append(actions, imageAction{key: "choose", label: i18n.T("重新选择图片…")})
		}
		if o.RevealImage != nil && f.resource.Local {
			actions = append(actions, imageAction{key: "reveal", label: i18n.T("在文件夹中显示")})
		}
		if len(actions) > 0 {
			cursor += 8
			x := float32(14)
			for _, action := range actions {
				bw := min(inner, natural(action.label, ui.Font{Family: "system-ui", Size: 13})+20)
				if x > 14 && x+bw > cardW-14 {
					x = 14
					cursor += 34
				}
				action.bounds = ui.Rect{X: f.bounds.X + x, Y: y + cursor, W: bw, H: 28}
				f.actions = append(f.actions, action)
				x += bw + 6
			}
			cursor += 28
		}
	}
	f.bounds.H = cursor + 10
	r.x, r.y = f.bounds.X+14, y+f.altY
	if e.captionVisible(b) {
		// Captions are editable through the block's text input. Keep the
		// editing line outside the recovery card and its action targets.
		r.x, r.y = x, y+f.bounds.H+8
		_, h := r.layout.Size()
		r.rect.H = r.y + max(20, h) - r.rect.Y + 18
	} else {
		r.rect.H = y + f.bounds.H - r.rect.Y + 18
	}
	return r.rect.H
}

func (e *Editor) paintImage(p *ui.Painter, bounds ui.Rect, row row, t *ui.Theme) {
	f := row.picture
	dx, dy := e.visualOffset(row)
	r := f.bounds
	r.X, r.Y = r.X+bounds.X+dx, r.Y+bounds.Y+dy

	drawBody := func() {
		if f.resource.Bitmap != nil {
			bitmap := f.resource.Bitmap
			if !e.printing && !e.reduceMotion && r.Y+r.H > bounds.Y+e.Scroll.Y && r.Y < bounds.Y+e.Scroll.Y+e.viewport {
				// An animated GIF in view plays.
				frame, next := f.resource.frameAt(p.Now())
				if next > 0 {
					bitmap = frame
					p.After(next)
				}
			}
			p.Shadow(r, 8, 0, 2, 10, 0, e.page.ImageShadow)
			p.Clip(r, 8, func() {
				p.Image(bitmap, r, ui.Contain)
				if !e.printing && !e.reduceMotion && !f.resource.LoadedAt.IsZero() {
					const fadeDur = 120 * time.Millisecond
					if elapsed := p.Now().Sub(f.resource.LoadedAt); elapsed >= 0 && elapsed < fadeDur {
						u := float32(elapsed) / float32(fadeDur)
						veil := (1 - u) * (1 - u)
						p.Fill(r, t.Background.Alpha(veil), 0)
						p.AnimationFrame()
					}
				}
			})
			if !e.printing && e.imageResize.id == row.leaf.Block.ID && row.rect.W > 0 {
				pct := max(1, min(100, int(math.Round(float64(f.bounds.W/row.rect.W*100)))))
				txt := ui.ShapeText(fmt.Sprintf("%d%%", pct), ui.Font{Family: "system-ui", Size: 12, Weight: 600}, 0)
				tw, th := txt.Size()
				pill := ui.Rect{X: r.X + (r.W-tw-20)/2, Y: r.Y + 10, W: tw + 20, H: max(24, th+8)}
				p.Fill(pill, e.page.Scrim.Alpha(0.75), pill.H/2)
				p.TextLayout(txt, pill.X+10, pill.Y+(pill.H-th)/2, e.page.OnScrim)
			}
		} else if f.resource.Loading {
			p.Fill(r, e.page.CodeFill, 8)
			p.Stroke(r, e.page.CodeBorder, 8, 1)
			ic := min(float32(20), min(r.W, r.H)/2)
			if ic >= 12 {
				p.Icon(iconImage, ui.Rect{X: r.X + (r.W-ic)/2, Y: r.Y + (r.H-ic)/2, W: ic, H: ic}, e.page.Placeholder)
			}
		} else {
			p.StrokeDashed(r, e.page.Caption, 8, 1)
			p.Icon(iconImage, ui.Rect{X: r.X + 14, Y: r.Y + 12, W: 16, H: 16}, e.page.Caption)
			p.TextLayout(f.title, r.X+36, r.Y+10, t.Text)
			if f.alt != nil {
				p.TextLayout(f.alt, r.X+14, r.Y+f.altY, e.page.Caption)
			}
			p.TextLayout(f.reason, r.X+14, r.Y+f.reasonY, e.page.Caption)
		}
		selection := e.Selection().Range
		if !e.printing && e.focused && (selection.Start == selection.End && e.Engine.LeafAt(e.Caret).Block.ID == row.leaf.Block.ID || selection.End > row.leaf.Start && selection.Start <= row.leaf.End) {
			p.Stroke(ui.Rect{X: r.X - 3, Y: r.Y - 3, W: r.W + 6, H: r.H + 6}, t.Accent, 10, 2)
		}
	}

	if !e.printing && !e.reduceMotion && e.imageEnter.id == row.leaf.Block.ID {
		if e.imageEnter.pending {
			e.imageEnter.start = p.Now()
			e.imageEnter.pending = false
		}
		const dur = 220 * time.Millisecond
		elapsed := p.Now().Sub(e.imageEnter.start)
		if elapsed >= 0 && elapsed < dur {
			u := float32(elapsed) / float32(dur)
			inv := 1 - u
			ease := 1 - inv*inv*inv
			scale := 0.82 + 0.18*ease
			deg := -4.0 * (1 - ease)
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			p.Transform(ui.Rotation(deg).Then(ui.Scaling(scale, scale)), cx, cy, drawBody)
			p.AnimationFrame()
		} else {
			e.imageEnter = imageEnterState{}
			drawBody()
		}
	} else {
		if e.imageEnter.id == row.leaf.Block.ID && e.reduceMotion {
			e.imageEnter = imageEnterState{}
		}
		drawBody()
	}

	if !e.printing && e.imageCaptionID == row.leaf.Block.ID && row.leaf.Block.Text() == "" {
		ph := ui.ShapeText(i18n.T("添加图片说明…"), ui.Font{Family: "system-ui", Size: 13}, 0)
		pw, _ := ph.Size()
		p.TextLayout(ph, bounds.X+row.rect.X+dx+(row.rect.W-pw)/2, bounds.Y+row.y+dy, e.page.Placeholder)
	}
}

// paintFileDropPreview draws the dashed drop slot and a tilted Polaroid card
// beside the pointer while files are dragged over the document.
func (e *Editor) paintFileDropPreview(p *ui.Painter, bounds ui.Rect, t *ui.Theme) {
	if e.printing || e.fileOver < 0 {
		return
	}
	y := bounds.Y + e.fileOver - 4
	slot := ui.Rect{X: bounds.X + e.padding, Y: y - 14, W: max(48, e.width-2*e.padding), H: 28}
	p.Fill(slot, t.Accent.Alpha(0.06), 7)
	p.StrokeDashed(slot, t.Accent.Alpha(0.65), 7, 1.25)
	hint := ui.ShapeText(i18n.T("松手插入图片"), ui.Font{Family: "system-ui", Size: 12, Weight: 500}, 0)
	hw, hh := hint.Size()
	if slot.W > hw+16 {
		p.TextLayout(hint, slot.X+(slot.W-hw)/2, slot.Y+(slot.H-hh)/2, t.Accent.Alpha(0.85))
	}

	if !e.pointerInside {
		return
	}
	card := ui.Rect{
		X: bounds.X + e.pointer.X + 14,
		Y: bounds.Y + e.pointer.Y - 46,
		W: 96,
		H: 78,
	}
	deg := float32(-4.5)
	if e.reduceMotion {
		deg = 0
	}
	cx, cy := card.X+card.W/2, card.Y+card.H/2
	p.Transform(ui.Rotation(deg), cx, cy, func() {
		p.Shadow(card, 8, 0, 6, 18, 0, e.page.MathShadow)
		p.Fill(card, e.page.Popover, 8)
		p.Stroke(card, e.page.MathBorder, 8, 1)
		photo := ui.Rect{X: card.X + 7, Y: card.Y + 7, W: card.W - 14, H: card.H - 24}
		p.Fill(photo, t.Accent.Alpha(0.11), 4)
		p.Icon(iconImage, ui.Rect{X: photo.X + (photo.W-20)/2, Y: photo.Y + (photo.H-20)/2, W: 20, H: 20}, t.Accent)
		bar := ui.Rect{X: card.X + 20, Y: card.Y + card.H - 11, W: card.W - 40, H: 4}
		p.Fill(bar, e.page.Placeholder.Alpha(0.45), 2)
	})
}

func (e *Editor) imageControls(c *ui.Context, o Options) {
	if e.printing {
		return
	}
	for i := range e.visibleRows(0) {
		// The row is copied only once it is a picture in view: the copy
		// escapes into the controls' callbacks, and copying every row of a
		// long page made each frame allocate megabytes.
		if r := &e.rows[i]; r.picture == nil || r.rect.Y+r.rect.H < e.Scroll.Y || r.rect.Y > e.Scroll.Y+e.viewport {
			continue
		}
		row := *e.indexedRow(i)
		f := row.picture
		id, src := row.leaf.Block.ID, row.leaf.Block.URL
		key := c.Key("image:" + id)
		if f.resource.Loading {
			r := f.bounds
			ui.Box(key.Key("loading")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
				Role(ui.RoleStatus).Label(i18n.T("正在加载图片…") + " " + f.name)
			continue
		}
		if f.resource.Bitmap == nil {
			r := f.bounds
			ui.Box(key.Key("status")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
				Role(ui.RoleImage).Label(i18n.T("无法显示图片：") + f.name + "。" + row.leaf.Block.Text() + "。" + f.resource.Problem.text()).Tooltip(src)
		}
		for _, action := range f.actions {
			b := e.imageButton(key.Key(action.key), action.label, action.bounds)
			if b.Clicked() {
				if action.key == "retry" {
					o.RetryImage(src)
				} else if action.key == "choose" {
					o.ReplaceImage(id, src)
				} else {
					o.RevealImage(src)
				}
			}
		}
		if f.resource.Bitmap == nil {
			continue
		}
		r := f.bounds
		picture := ui.Box(key).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
			Focusable().Role(ui.RoleButton).Label(i18n.T("查看图片：") + f.name).Tooltip(row.leaf.Block.Text()).Cursor(ui.CursorPointer)
		if picture.Clicked() {
			e.FocusBlock(id)
			if e.imageCaptionID != "" {
				e.imageCaptionID, e.ready = "", false
			}
		}
		picture.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch ev.Key {
			case ui.KeyBackspace, ui.KeyDelete:
				e.DeleteBlock(id)
			case ui.KeyLeft, ui.KeyUp:
				e.SetSelection(max(0, row.leaf.Start-1), max(0, row.leaf.Start-1))
			case ui.KeyRight, ui.KeyDown:
				e.SetSelection(min(e.Len(), row.leaf.End+1), min(e.Len(), row.leaf.End+1))
			default:
				return false
			}
			e.RequestFocus = true
			return true
		})
		open := func() {
			e.imageViewer = imageViewer{
				open:     true,
				name:     f.name,
				bitmap:   f.resource.Bitmap,
				origin:   picture.Bounds(),
				openedAt: c.Now(),
			}
		}
		if picture.DoubleClicked() || picture.Focused() && (picture.Shortcut(0, ui.KeyEnter) || picture.Shortcut(0, ui.KeySpace)) {
			open()
		}
		if picture.Focused() {
			picture.Border(2, c.Theme().Accent).Radius(8)
		}
		picture.ContextMenu(func(m *ui.Menu) {
			menuAction(m, i18n.T("放大查看"), open)
			if !e.ReadOnly {
				menuAction(m, i18n.T("编辑图片说明…"), func() {
					e.FocusBlock(id)
					e.imageCaptionID, e.ready, e.RequestFocus = id, false, true
				})
			}
			if o.ReplaceImage != nil {
				menuAction(m, i18n.T("重新选择图片…"), func() { o.ReplaceImage(id, src) })
			}
			if o.CropImage != nil && f.resource.Local && len(f.resource.Frames) == 0 {
				menuAction(m, i18n.T("裁剪图片…"), func() { e.openCropper(id, src, f.resource.Bitmap) })
			}
			if o.RevealImage != nil && f.resource.Local {
				menuAction(m, i18n.T("在文件夹中显示"), func() { o.RevealImage(src) })
			}
		})
		e.imageHandles(c, row)
		if (picture.Hovered() || picture.Focused() || e.hovered(r, 0)) && r.W >= 100 && r.H >= 56 {
			button := e.imageButton(key.Key("zoom"), i18n.T("放大查看"), ui.Rect{X: r.X + r.W - 82, Y: r.Y + 8, W: 74, H: 28})
			button.Background(e.page.Popover)
			if button.Clicked() {
				open()
			}
		}
	}
}

func (e *Editor) imageButton(c *ui.Context, label string, r ui.Rect) ui.Element {
	b := ui.ButtonBase(c).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).
		Radius(14).Border(1, e.page.TableLine).Label(label).Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(e.page.Hover)
	}
	if b.FocusVisible() {
		b.Border(2, c.Theme().Accent)
	}
	b.Children(func() { ui.Text(c, label).FontSize(13).MinWidth(0).MaxLines(1).TextColor(c.Theme().Text) })
	return b
}

// Explicit typing reveals the editable description; reading and pasting keep
// filenames and alt text out of a successfully loaded picture's layout.
func (e *Editor) editImageCaption() {
	if b := e.Engine.LeafAt(e.Caret).Block; b.Kind == "image" {
		if e.captionedImages == nil {
			e.captionedImages = map[string]bool{}
		}
		e.captionedImages[b.ID] = true
		if e.imageCaptionID != b.ID {
			e.imageCaptionID, e.ready = b.ID, false
		}
	}
}

func (e *Editor) imagePreview(c *ui.Context) {
	v := &e.imageViewer
	if !v.open || v.bitmap == nil {
		return
	}
	w, h := c.Size()
	w, h = max(200, w-48), max(160, h-48)
	ui.DialogBase(c.Key("image-preview"), &v.open, func(backdrop, panel ui.Element) {
		backdrop.Background(e.page.Scrim).Transition(ui.ElementTransition{
			Duration: 150 * time.Millisecond,
			Enter:    &ui.Motion{Opacity: 0},
		})
		panel.Size(w, h).Padding(12).Gap(10).Radius(12).Background(c.Theme().Background).Label(i18n.T("图片预览")).
			Transition(ui.ElementTransition{
				Duration: 165 * time.Millisecond,
				Enter:    &ui.Motion{Opacity: 0, Scale: 0.97},
			})
		ui.Row(c).Height(32).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, v.name).Grow(1).MinWidth(0).MaxLines(1).FontSize(14).FontWeight(600)
			label := i18n.T("原始大小")
			if v.actual {
				label = i18n.T("适应窗口")
			}
			if ui.Button(c, label).Clicked() {
				v.actual = !v.actual
				v.scroll = ui.ScrollState{}
				v.zoom, v.pan = 0, ui.Point{}
			}
			vw, vh := w-24, h-66
			if ui.Button(c, "−").Label(i18n.T("缩小图片")).Clicked() {
				v.zoomAt(v.scaleFor(vw, vh)/1.15, vw/2, vh/2, vw, vh)
			}
			ui.Text(c, fmt.Sprintf("%.0f%%", v.scaleFor(vw, vh)*100)).FontSize(12).Width(42)
			if ui.Button(c, "+").Label(i18n.T("放大图片")).Clicked() {
				v.zoomAt(v.scaleFor(vw, vh)*1.15, vw/2, vh/2, vw, vh)
			}
			if ui.Button(c, i18n.T("关闭")).Tooltip(i18n.T("关闭 (Esc)")).Clicked() {
				v.open = false
			}
		})
		e.imageCanvas(c, w-24, h-66)
	})
}
