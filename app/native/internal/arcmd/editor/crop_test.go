package editor

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCropperFrameStaysOnThePicture(t *testing.T) {
	k := imageCropper{bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 100, 50))), x1: 100, y1: 50}
	k.resize(0, 30, 10)
	if k.cropRect() != image.Rect(30, 10, 100, 50) {
		t.Fatal(k.cropRect())
	}
	k.move(-100, 100)
	if k.cropRect() != image.Rect(0, 10, 70, 50) {
		t.Fatal("moving stays on the picture", k.cropRect())
	}
	k.resize(2, -200, -200)
	if r := k.cropRect(); r.Dx() != cropMin || r.Dy() != cropMin {
		t.Fatal("no smaller than the least", r)
	}
}

func TestCropDialogHandsTheFrameToTheHost(t *testing.T) {
	e := New("x")
	var got image.Rectangle
	tt := ui.NewTester(func(c *ui.Context) {
		w, _ := c.Size()
		View(c, e, Options{Width: w, Theme: c.Theme(), CropImage: func(id, url string, r image.Rectangle) { got = r }})
	}, 1000, 760)
	tt.Frame()
	e.openCropper("id", "a.png", ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 200, 100))))
	tt.Frame()
	if !tt.HasText("200 × 100") {
		t.Fatal(tt.Texts())
	}
	r, ok := tt.Find("调整裁剪框")
	if !ok {
		t.Fatal("no corner")
	}
	tt.Press(r.X+7, r.Y+7)
	tt.Move(r.X+27, r.Y+17)
	tt.Release(r.X+27, r.Y+17)
	if err := tt.Click("裁剪"); err != nil {
		t.Fatal(err)
	}
	if got.Min != image.Pt(20, 10) || got.Max != image.Pt(200, 100) || e.cropper.open {
		t.Fatal("the frame", got)
	}
}
