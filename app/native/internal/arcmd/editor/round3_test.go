package editor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/document"
	"github.com/egoist/mygo/ui"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: 59, G: 130, B: 246, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAsyncImageZeroLayoutShiftAndLightboxZoomDamping(t *testing.T) {
	pngBytes := makeTestPNG(t, 320, 180)
	bmp, err := ui.DecodeBitmap(pngBytes)
	if err != nil {
		t.Fatal(err)
	}

	loading := true
	var loadedAt time.Time
	e := New("Before\n\n![hero](assets/hero.png)\n\nAfter")
	tt := ui.NewTester(func(c *ui.Context) {
		View(c, e, Options{
			Theme:  c.Theme(),
			Width:  820,
			Column: 680,
			LoadImage: func(src string) ImageResource {
				if loading {
					return ImageResource{Width: 320, Height: 180, Loading: true, Local: true}
				}
				return ImageResource{Bitmap: bmp, Width: 320, Height: 180, LoadedAt: loadedAt, Local: true}
			},
		})
	}, 900, 700)
	tt.Frame()

	var placeholderBounds ui.Rect
	afterYBefore := e.rows[2].rect.Y
	if e.rows[1].picture == nil {
		t.Fatal("missing image figure during loading")
	}
	placeholderBounds = e.rows[1].picture.bounds
	if placeholderBounds.W != 320 || placeholderBounds.H != 180 {
		t.Fatalf("unexpected placeholder bounds: %+v", placeholderBounds)
	}

	// Complete background decode and invalidate: zero layout shift!
	loading = false
	loadedAt = time.Now()
	e.InvalidateImage("assets/hero.png")
	tt.Frame()
	decodedBounds := e.rows[1].picture.bounds
	afterYAfter := e.rows[2].rect.Y
	if decodedBounds != placeholderBounds || afterYAfter != afterYBefore {
		t.Fatalf("layout shifted after async image decode: bounds %+v -> %+v, afterY %v -> %v",
			placeholderBounds, decodedBounds, afterYBefore, afterYAfter)
	}
	tt.Advance(150 * time.Millisecond)
	tt.Frame()

	// Double-click the image to open the lightbox viewer and test smooth zoom damping.
	docBounds, _ := tt.Find("文档")
	cx := docBounds.X + decodedBounds.X + decodedBounds.W/2
	cy := docBounds.Y + decodedBounds.Y + decodedBounds.H/2
	tt.ClickAt(cx, cy)
	tt.ClickAt(cx, cy)
	tt.Frame()
	if !e.imageViewer.open {
		t.Fatal("double-click did not open image lightbox")
	}
	// Advance past initial hero open animation so shownScale settles at fit scale.
	tt.Advance(220 * time.Millisecond)
	tt.Frame()
	startShown := e.imageViewer.shownScale
	if err := tt.Click("放大图片"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(24 * time.Millisecond)
	midShown := e.imageViewer.shownScale
	if midShown <= startShown {
		t.Fatalf("lightbox zoom did not smoothly advance: start=%v mid=%v", startShown, midShown)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if abs32(e.imageViewer.shownScale-midShown) < 0.001 {
		t.Fatalf("lightbox zoom snapped on first frame instead of easing: mid=%v final=%v", midShown, e.imageViewer.shownScale)
	}
}

func TestColumnsMiddleSeamResizeSnapAndRoundTrip(t *testing.T) {
	md := "<div class=\"arcmd-columns\">\n<div class=\"arcmd-column\">\n\nLeft column\n\n</div>\n<div class=\"arcmd-column\">\n\nRight column\n\n</div>\n</div>"
	e, tt := newTester(md)
	focus(e, tt)
	tt.Frame()

	if len(e.Doc.Blocks) != 1 || e.Doc.Blocks[0].Kind != "columns" {
		t.Fatalf("expected columns block, got %+v", e.Doc.Blocks)
	}
	if len(e.resizers) != 1 {
		t.Fatalf("expected 1 middle-seam resizer for 2-column block, got %d", len(e.resizers))
	}

	// Default equal-width columns omit data-widths in Markdown export.
	if out := ExportMarkdown(e.Doc); strings.Contains(out, "data-widths") {
		t.Fatalf("default equal-width columns polluted Markdown with data-widths: %q", out)
	}

	// Drag the middle seam right by 90 DIP to create asymmetric columns.
	res := e.resizers[0]
	docBounds, _ := tt.Find("文档")
	sx, sy := docBounds.X+res.x, docBounds.Y+res.y+12
	tt.Move(sx, sy)
	tt.Frame()
	tt.Press(sx, sy)
	tt.Move(sx+90, sy)
	tt.Frame()
	tt.Release(sx+90, sy)
	tt.Frame()

	w := e.Doc.Blocks[0].Widths
	if len(w) != 2 || w[0] <= w[1]+40 {
		t.Fatalf("middle-seam drag did not resize columns asymmetrically: %v", w)
	}

	// Export & re-import Markdown and HTML preserve custom column proportions.
	ratio := w[0] / (w[0] + w[1])
	exportedMD := ExportMarkdown(e.Doc)
	if !strings.Contains(exportedMD, "data-widths=") {
		t.Fatalf("expected data-widths in asymmetric Markdown export: %q", exportedMD)
	}
	reimportedMD := ImportMarkdown(exportedMD)
	if err := reimportedMD.Validate(); err != nil {
		t.Fatalf("reimported Markdown failed validation: %v", err)
	}
	rw := reimportedMD.Blocks[0].Widths
	if len(rw) != 2 || abs32(rw[0]/(rw[0]+rw[1])-ratio) > 0.01 {
		t.Fatalf("Markdown round-trip lost column ratio: got %v, want ratio %.3f", rw, ratio)
	}

	exportedHTML := ExportHTML(e.Doc)
	reimportedHTML := ImportHTML(exportedHTML)
	if len(reimportedHTML.Blocks) != 1 || reimportedHTML.Blocks[0].Kind != "columns" || len(reimportedHTML.Blocks[0].Widths) != 2 {
		t.Fatalf("HTML round-trip failed on columns: %+v", reimportedHTML.Blocks)
	}
	hw := reimportedHTML.Blocks[0].Widths
	if abs32(hw[0]/(hw[0]+hw[1])-ratio) > 0.01 {
		t.Fatalf("HTML round-trip lost column ratio: got %v, want ratio %.3f", hw, ratio)
	}

	// Drag back within 6 DIP of center: magnetic snap commits 50% / 50%.
	res = e.resizers[0]
	center := res.x - (res.widths[0]-res.widths[1])*0.5
	tt.Press(docBounds.X+res.x, sy)
	tt.Move(docBounds.X+center+3, sy)
	tt.Frame()
	tt.Release(docBounds.X+center+3, sy)
	tt.Frame()
	if got := e.Doc.Blocks[0].Widths; len(got) != 2 || abs32(got[0]-got[1]) > 0.01 {
		t.Fatalf("columns middle seam did not magnetically snap to 50/50: %v", got)
	}

	// Undo restores the asymmetric widths; double-clicking the seam equalizes cleanly.
	tt.Key(ui.Cmd, ui.KeyZ)
	tt.Frame()
	if got := e.Doc.Blocks[0].Widths; len(got) != 2 || abs32(got[0]-got[1]) < 20 {
		t.Fatalf("undo did not restore asymmetric widths: %v", got)
	}
	res = e.resizers[0]
	tt.ClickAt(docBounds.X+res.x, sy)
	tt.ClickAt(docBounds.X+res.x, sy)
	tt.Frame()
	if got := e.Doc.Blocks[0].Widths; len(got) != 2 || abs32(got[0]-got[1]) > 0.01 || strings.Contains(ExportMarkdown(e.Doc), "data-widths") {
		t.Fatalf("double-clicking middle seam did not equalize widths: %v", got)
	}
}

func TestCalloutColorCrossFadeAndFindMatchPulse(t *testing.T) {
	e, tt := newTester("> [!NOTE]\n> Watch the tone fade smoothly.\n\nTarget needle one and needle two.")
	focus(e, tt)
	tt.Frame()

	calloutID := e.Doc.Blocks[0].ID
	initialIcon := e.motion.callouts[calloutID].icon
	e.Engine.SetColor(calloutID, "red")
	e.sync()
	tt.Frame()
	tt.Advance(20 * time.Millisecond)
	midIcon := e.motion.callouts[calloutID].icon
	targetIcon := e.page.Callouts["red"].Icon
	if midIcon == initialIcon || midIcon == targetIcon {
		t.Fatalf("callout tone did not cross-fade smoothly: initial=%+v mid=%+v target=%+v", initialIcon, midIcon, targetIcon)
	}
	tt.Advance(400 * time.Millisecond)
	if e.motion.callouts[calloutID].icon != targetIcon {
		t.Fatalf("callout tone did not settle at target: got=%+v want=%+v", e.motion.callouts[calloutID].icon, targetIcon)
	}

	// ⌘F match navigation triggers active match pulse and settles cleanly.
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("needle")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if e.find.pulseAt.IsZero() && !e.find.pulsePending {
		t.Fatal("stepping to next match did not trigger match pulse")
	}
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	if !e.find.pulseAt.IsZero() {
		t.Fatal("match pulse did not clean up after completing")
	}
	_ = document.CurrentVersion
}
