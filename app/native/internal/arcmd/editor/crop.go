package editor

// Cropping a picture: its context menu opens the picture in a dialog with
// a frame to drag — its corners resize it, its inside moves it — and 裁剪
// hands the frame, in the picture's pixels, to the host, which keeps the
// cut as a new file beside the document and points the picture at it as
// one step of the history. The original file stays.

import (
	"fmt"
	"image"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/i18n"
	"github.com/egoist/mygo/ui"
)

// imageCropper is the open crop dialog: the picture, and the frame in its
// pixels.
type imageCropper struct {
	open    bool
	id, url string
	bitmap  *ui.Bitmap
	x0, y0  float32
	x1, y1  float32
}

// cropMin is the smallest frame, in the picture's pixels.
const cropMin = 8

// openCropper opens the dialog for picture id at url, framing all of it.
func (e *Editor) openCropper(id, url string, b *ui.Bitmap) {
	w, h := b.Size()
	e.cropper = imageCropper{open: true, id: id, url: url, bitmap: b, x1: float32(w), y1: float32(h)}
}

// cropRect is the frame as whole pixels.
func (k *imageCropper) cropRect() image.Rectangle {
	return image.Rect(int(k.x0+.5), int(k.y0+.5), int(k.x1+.5), int(k.y1+.5))
}

// move drags the frame by dx, dy pixels, kept on the picture.
func (k *imageCropper) move(dx, dy float32) {
	w, h := k.bitmap.Size()
	dx = max(-k.x0, min(dx, float32(w)-k.x1))
	dy = max(-k.y0, min(dy, float32(h)-k.y1))
	k.x0, k.x1, k.y0, k.y1 = k.x0+dx, k.x1+dx, k.y0+dy, k.y1+dy
}

// resize drags corner (0 top left, 1 top right, 2 bottom right, 3 bottom
// left) by dx, dy pixels, kept on the picture and no smaller than cropMin.
func (k *imageCropper) resize(corner int, dx, dy float32) {
	w, h := k.bitmap.Size()
	if corner == 0 || corner == 3 {
		k.x0 = max(0, min(k.x0+dx, k.x1-cropMin))
	} else {
		k.x1 = min(float32(w), max(k.x1+dx, k.x0+cropMin))
	}
	if corner == 0 || corner == 1 {
		k.y0 = max(0, min(k.y0+dy, k.y1-cropMin))
	} else {
		k.y1 = min(float32(h), max(k.y1+dy, k.y0+cropMin))
	}
}

// cropView is the crop dialog.
func (e *Editor) cropView(c *ui.Context) {
	k := &e.cropper
	if !k.open || k.bitmap == nil {
		return
	}
	t := c.Theme()
	ww, wh := c.Size()
	iw, ih := k.bitmap.Size()
	// The picture fits the dialog, never enlarged.
	areaW, areaH := min(720, ww-80), min(460, wh-180)
	scale := min(float32(1), areaW/float32(max(1, iw)), areaH/float32(max(1, ih)))
	pw, ph := float32(iw)*scale, float32(ih)*scale
	ui.DialogBase(c.Key("image-crop"), &k.open, func(backdrop, panel ui.Element) {
		backdrop.Background(e.page.Scrim)
		panel.Padding(16).Gap(12).Radius(12).Background(t.Background).Label(i18n.T("裁剪图片"))
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("裁剪图片")).FontSize(15).FontWeight(600).Grow(1)
			r := k.cropRect()
			ui.Text(c, fmt.Sprintf("%d × %d", r.Dx(), r.Dy())).FontSize(12).TextColor(t.TextMuted)
		})
		stage := ui.Box(c.Key("crop-stage")).Size(pw, ph).AlignSelf(ui.Center)
		stage.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Image(k.bitmap, r, ui.Contain)
			f := ui.Rect{X: r.X + k.x0*scale, Y: r.Y + k.y0*scale, W: (k.x1 - k.x0) * scale, H: (k.y1 - k.y0) * scale}
			shade := ui.RGB(0, 0, 0).Alpha(.5)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: f.Y - r.Y}, shade, 0)
			p.Fill(ui.Rect{X: r.X, Y: f.Y + f.H, W: r.W, H: r.Y + r.H - f.Y - f.H}, shade, 0)
			p.Fill(ui.Rect{X: r.X, Y: f.Y, W: f.X - r.X, H: f.H}, shade, 0)
			p.Fill(ui.Rect{X: f.X + f.W, Y: f.Y, W: r.X + r.W - f.X - f.W, H: f.H}, shade, 0)
			p.Stroke(f, cropWhite, 0, 1.5)
			// Thirds, as cameras show them.
			for i := float32(1); i < 3; i++ {
				p.Line(f.X+f.W*i/3, f.Y, f.X+f.W*i/3, f.Y+f.H, 1, cropWhite.Alpha(.4))
				p.Line(f.X, f.Y+f.H*i/3, f.X+f.W, f.Y+f.H*i/3, 1, cropWhite.Alpha(.4))
			}
		})
		stage.Children(func() {
			fx, fy, fw, fh := k.x0*scale, k.y0*scale, (k.x1-k.x0)*scale, (k.y1-k.y0)*scale
			inside := ui.Box(c.Key("crop-move")).Absolute().Left(fx).Top(fy).Size(fw, fh).Cursor(ui.CursorMove).
				Label(i18n.T("移动裁剪框")).Focusable()
			if dx, dy, held := inside.Dragged(); held {
				k.move(dx/scale, dy/scale)
			}
			inside.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputKeyDown {
					return false
				}
				step := float32(10)
				switch ev.Key {
				case ui.KeyLeft:
					k.move(-step, 0)
				case ui.KeyRight:
					k.move(step, 0)
				case ui.KeyUp:
					k.move(0, -step)
				case ui.KeyDown:
					k.move(0, step)
				default:
					return false
				}
				return true
			})
			for i, at := range [][2]float32{{fx, fy}, {fx + fw, fy}, {fx + fw, fy + fh}, {fx, fy + fh}} {
				cursor := ui.CursorResizeNWSE
				if i == 1 || i == 3 {
					cursor = ui.CursorResizeNESW
				}
				h := ui.Box(c.Key(fmt.Sprintf("crop-corner-%d", i))).Absolute().Left(at[0]-7).Top(at[1]-7).Size(14, 14).
					Radius(3).Background(cropWhite).Border(1, t.Accent).Cursor(cursor).Label(i18n.T("调整裁剪框"))
				if dx, dy, held := h.Dragged(); held {
					k.resize(i, dx/scale, dy/scale)
				}
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("crop-reset"), i18n.T("重置")).Clicked() {
				k.x0, k.y0, k.x1, k.y1 = 0, 0, float32(iw), float32(ih)
			}
			ui.Box(c).Grow(1)
			if ui.Button(c.Key("crop-cancel"), i18n.T("取消")).Tooltip(i18n.T("取消 (Esc)")).Clicked() {
				k.open = false
			}
			whole := k.cropRect() == image.Rect(0, 0, iw, ih)
			if ui.PrimaryButton(c.Key("crop-apply"), i18n.T("裁剪")).Disabled(whole).Clicked() {
				k.open = false
				if e.cropImage != nil {
					e.cropImage(k.id, k.url, k.cropRect())
				}
			}
		})
	})
}

var cropWhite = ui.RGB(255, 255, 255)

// OpenCropper opens the crop dialog for the picture id, when it is shown
// and still.
func (e *Editor) OpenCropper(id string) bool {
	for _, r := range e.rows {
		if r.picture != nil && r.leaf.Block.ID == id && r.picture.resource.Bitmap != nil && len(r.picture.resource.Frames) == 0 {
			e.openCropper(id, r.leaf.Block.URL, r.picture.resource.Bitmap)
			return true
		}
	}
	return false
}
