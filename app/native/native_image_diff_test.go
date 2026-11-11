package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func nativeImageTestBlob(t *testing.T, w, h int, c color.Color) *backend.Blob {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, c)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, m); err != nil {
		t.Fatal(err)
	}
	return &backend.Blob{Contents: data.Bytes(), MediaType: "image/png", Bytes: data.Len()}
}
func nativeImageTestDiff(t *testing.T, w, h int) backend.Diff {
	return backend.Diff{Kind: "image", Previous: nativeImageTestBlob(t, w, h, color.NRGBA{40, 80, 160, 255}), Current: nativeImageTestBlob(t, w, h, color.NRGBA{200, 100, 40, 255})}
}

func TestNativeImageDecodeAlphaFitAndSingleSides(t *testing.T) {
	for _, both := range []bool{false, true} {
		diff := nativeImageTestDiff(t, 3, 2)
		diff.Current = nativeImageTestBlob(t, 3, 2, color.NRGBA{240, 120, 60, 128})
		if !both {
			diff.Previous = nil
		}
		p, err := prepareNativeImageDiff(context.Background(), diff)
		if err != nil {
			t.Fatal(err)
		}
		defer p.close()
		expected := color.RGBAModel.Convert(color.NRGBA{240, 120, 60, 128}).(color.RGBA)
		if got := p.current.pixels.RGBAAt(1, 1); got != expected {
			t.Fatalf("oriented native raster changed alpha: %v want %v", got, expected)
		}
		if w, h := p.size(200, 200, false); w != 3 || h != 2 {
			t.Fatalf("thumbnail upscaled %v %v", w, h)
		}
	}
	for _, test := range []struct{ w, h, cw, ch, rw, rh float32 }{{400, 200, 100, 100, 100, 50}, {200, 400, 100, 100, 50, 100}, {20, 10, 100, 100, 20, 10}} {
		w, h := nativeImageFit(test.w, test.h, test.cw, test.ch)
		if w != test.rw || h != test.rh {
			t.Errorf("fit %v: %v %v", test, w, h)
		}
	}
	p := &nativeImageDiffPrepared{previous: &nativeImageDiffMedia{width: 400, height: 100}, current: &nativeImageDiffMedia{width: 100, height: 400}}
	if w, h := p.size(200, 200, false); w != 200 || h != 200 {
		t.Fatal("must fit independently then union")
	}
	if w, h := p.size(200, 200, true); w != 400 || h != 400 {
		t.Fatal("natural size was fitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareNativeImageDiff(ctx, nativeImageTestDiff(t, 1, 1)); err != context.Canceled {
		t.Fatalf("canceled decode: %v", err)
	}
	for _, diff := range []backend.Diff{{Kind: "text"}, {Kind: "image"}, {Kind: "image", Current: &backend.Blob{Contents: []byte("not an image")}}} {
		if _, err := prepareNativeImageDiff(context.Background(), diff); err == nil {
			t.Fatal("invalid image silently accepted")
		}
	}
}

func TestNativeImageDifferenceAlphaAndCentering(t *testing.T) {
	cases := []struct {
		name string
		b, s color.NRGBA
		want color.RGBA
	}{
		{"opaque", color.NRGBA{40, 80, 160, 255}, color.NRGBA{200, 100, 40, 255}, color.RGBA{160, 20, 120, 255}},
		{"source transparent", color.NRGBA{40, 80, 160, 255}, color.NRGBA{255, 0, 0, 0}, color.RGBA{40, 80, 160, 255}},
		{"backdrop transparent", color.NRGBA{}, color.NRGBA{200, 100, 40, 255}, color.RGBA{200, 100, 40, 255}},
		{"both transparent", color.NRGBA{}, color.NRGBA{}, color.RGBA{}},
		// With both alpha 128, identical white does not turn transparent or black:
		// the non-overlapping coverage remains (CSS source-over after blending).
		{"same half white", color.NRGBA{255, 255, 255, 128}, color.NRGBA{255, 255, 255, 128}, color.RGBA{127, 127, 127, 192}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, s := image.NewRGBA(image.Rect(0, 0, 1, 1)), image.NewRGBA(image.Rect(0, 0, 1, 1))
			b.Set(0, 0, tc.b)
			s.Set(0, 0, tc.s)
			got, err := nativeImageDifferencePixels(context.Background(), b, s)
			if err != nil {
				t.Fatal(err)
			}
			if got.RGBAAt(0, 0) != tc.want {
				t.Fatalf("%v want %v", got.RGBAAt(0, 0), tc.want)
			}
		})
	}
	diff := nativeImageTestDiff(t, 4, 4)
	diff.Current = nativeImageTestBlob(t, 2, 2, color.NRGBA{200, 100, 40, 255})
	p, err := prepareNativeImageDiff(context.Background(), diff)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	d, err := newNativeImageDifferenceDecoder(context.Background(), p, image.Pt(4, 4))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if got := frame.Image.At(0, 0); got != (color.RGBA{40, 80, 160, 255}) {
		t.Fatalf("nonoverlap not source-over: %v", got)
	}
	if got := frame.Image.At(1, 1); got != (color.RGBA{160, 20, 120, 255}) {
		t.Fatalf("different sizes weren't centered: %v", got)
	}
	if _, err := d.Next(); err != io.EOF {
		t.Fatalf("static image asks for infinite frames: %v", err)
	}
}

func nativeImageTestGIF(t *testing.T, delays []int, loops int) *backend.Blob {
	t.Helper()
	palette := color.Palette{color.NRGBA{40, 80, 160, 255}, color.NRGBA{200, 100, 40, 255}, color.Transparent}
	g := &gif.GIF{Delay: delays, LoopCount: loops, Config: image.Config{ColorModel: palette, Width: 8, Height: 8}}
	for i := range delays {
		m := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
		for j := range m.Pix {
			m.Pix[j] = uint8(i % 2)
		}
		g.Image = append(g.Image, m)
		g.Disposal = append(g.Disposal, gif.DisposalBackground)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, g); err != nil {
		t.Fatal(err)
	}
	return &backend.Blob{Contents: out.Bytes(), Bytes: out.Len(), MediaType: "image/gif"}
}
func TestNativeImageGIFRealTimelinesAndFiniteLoops(t *testing.T) {
	for _, loops := range []int{-1, 0, 2} {
		blob := nativeImageTestGIF(t, []int{1, 20}, loops)
		d, err := ui.NewGIFDecoder(blob.Contents)
		if err != nil {
			t.Fatal(err)
		}
		limited := &nativeImageLoopDecoder{decoder: d, loops: loops}
		count := 0
		for ; count < 10; count++ {
			frame, err := limited.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if count%2 == 0 && frame.Duration != 100*time.Millisecond {
				t.Fatal("browser minimum GIF delay lost")
			}
		}
		want := 2
		if loops == 0 {
			want = 10
		} else if loops > 0 {
			want = 2 * (loops + 1)
		}
		if count != want {
			t.Fatalf("loops %d: frames %d want %d", loops, count, want)
		}
	}
	diff := backend.Diff{Kind: "image", Previous: nativeImageTestGIF(t, []int{10, 20}, -1), Current: nativeImageTestGIF(t, []int{20, 20}, -1)}
	p, err := prepareNativeImageDiff(context.Background(), diff)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if p.previous.player == nil || p.current.player == nil {
		t.Fatal("animated GIF flattened to first frame")
	}
	d, err := newNativeImageDifferenceDecoder(context.Background(), p, image.Pt(8, 8))
	if err != nil {
		t.Fatal(err)
	}
	var durations []time.Duration
	var red []uint32
	for {
		frame, err := d.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		durations = append(durations, frame.Duration)
		r, _, _, _ := frame.Image.At(4, 4).RGBA()
		red = append(red, r>>8)
	}
	if !reflect.DeepEqual(durations, []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond}) {
		t.Fatalf("different GIF timelines paired by frame index: %v", durations)
	}
	if !reflect.DeepEqual(red, []uint32{0, 160, 0, 0}) {
		t.Fatalf("GIF difference isn't animated: %v", red)
	}
}

type nativeImageHarness struct {
	a       *app
	s       *nativeImageDiffState
	tt      *ui.Tester
	updates chan func()
}

func newNativeImageHarness(t *testing.T, diff backend.Diff, width, height int, locale string, dark bool) *nativeImageHarness {
	t.Helper()
	a := newApp(nil, nil)
	h := &nativeImageHarness{a: a, s: &nativeImageDiffState{imageOpacity: 1}, updates: make(chan func(), 100)}
	a.update = func(fn func()) { h.updates <- fn }
	p, err := prepareNativeImageDiff(a.ctx, diff)
	if err != nil {
		t.Fatal(err)
	}
	h.s.imagePrepared = p
	h.tt = ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		// The production image view receives the same 232-DIP rail and 24-DIP
		// content margins as the minimum workspace; no duplicate preview widgets.
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(projectRailWidth).FillHeight().Background(c.Theme().Surface)
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Padding(24).Children(func() { h.a.nativeImageDiffView(c, h.s, func(m nativeImageDiffType) { h.s.setNativeImageDiffType(m) }) })
		})
	}, width, height)
	h.tt.SetLocale(locale)
	h.tt.SetDark(dark)
	t.Cleanup(func() { h.s.clearNativeImageDiff(); a.close() })
	return h
}
func (h *nativeImageHarness) settle(t *testing.T) {
	t.Helper()
	for h.s.imageMode == nativeImageDifference && h.s.imageDifference == nil && h.s.imageDifferenceError == nil {
		select {
		case fn := <-h.updates:
			fn()
			h.tt.Frame()
		case <-time.After(10 * time.Second):
			t.Fatal("difference worker did not finish")
		}
	}
	if h.s.imageDifferenceError != nil {
		t.Fatal(h.s.imageDifferenceError)
	}
	h.tt.Frame()
}
func nativeImageScreenshot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if os.Getenv("GITOWL_IMAGE_SCREENSHOTS") != "1" {
		return
	}
	dir := filepath.Join("..", "..", "_temp", "native-full-ui", "image-diff")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeImageTesterMatrix(t *testing.T) {
	for _, size := range []image.Point{{900, 620}, {1440, 900}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, dark := range []bool{false, true} {
				name := fmt.Sprintf("%dx%d-%s-dark%v", size.X, size.Y, locale, dark)
				t.Run(name, func(t *testing.T) {
					h := newNativeImageHarness(t, nativeImageTestDiff(t, 480, 300), size.X, size.Y, locale, dark)
					label := func(key string) string { return nativeImageLabel(locale, key) }
					for i, key := range []string{"twoUp", "swipe", "onion", "difference"} {
						if err := h.tt.Click(label(key)); err != nil {
							t.Fatal(err)
						}
						h.settle(t)
						if h.s.imageMode != nativeImageDiffType(i) {
							t.Fatal("segment did not request mode")
						}
						for _, k := range []string{"twoUp", "swipe", "onion", "difference", "natural", "preview"} {
							visibleControl(t, h.tt, label(k), size.X, size.Y)
						}
						if access, ok := h.tt.AccessByRole(ui.RoleRadio, label(key)); !ok || !access.Checked {
							t.Fatalf("selected mode not accessible: %+v %v", access, ok)
						}
						if i == 1 || i == 2 {
							slider := "swipePosition"
							value := &h.s.imageSwipe
							initial := float64(0)
							if i == 2 {
								slider = "opacity"
								value = &h.s.imageOpacity
								initial = 1
							}
							if *value != initial {
								t.Fatalf("upstream initial %s: %v", slider, *value)
							}
							sliderControl, found := h.tt.AccessByRole(ui.RoleSlider, label(slider))
							if !found {
								t.Fatal("native slider missing")
							}
							r := sliderControl.Bounds
							h.tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
							h.tt.Key(0, ui.KeyHome)
							if *value != 0 {
								t.Fatal("Home")
							}
							h.tt.Key(0, ui.KeyRight)
							if *value != 0.1 {
								t.Fatalf("native slider step: %v", *value)
							}
							h.tt.Key(0, ui.KeyEnd)
							if *value != 100 {
								t.Fatal("End")
							}
							access, ok := h.tt.AccessByRole(ui.RoleSlider, label(slider))
							if !ok || access.Now != 100 || access.Min != 0 || access.Max != 100 {
								t.Fatalf("slider a11y range: %+v", access)
							}
							*value = 50
							h.tt.Frame()
						}
						nativeImageScreenshot(t, h.tt, fmt.Sprintf("%s-mode%d", name, i))
					}
					if err := h.tt.Click(label("natural")); err != nil {
						t.Fatal(err)
					}
					h.settle(t)
					if !h.s.imageNatural {
						t.Fatal("natural pixels toggle")
					}
				})
			}
		}
	}
}

func TestNativeImageSwipeOpacityPixelsAndNaturalScroll(t *testing.T) {
	h := newNativeImageHarness(t, nativeImageTestDiff(t, 200, 120), 900, 620, "en", false)
	sample := func() color.RGBA {
		r, _ := h.tt.Find("Image preview")
		return h.tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
	}
	for _, mode := range []nativeImageDiffType{nativeImageSwipe, nativeImageOnionSkin} {
		h.s.setNativeImageDiffType(mode)
		h.s.imageSwipe, h.s.imageOpacity = 0, 0
		h.tt.Frame()
		want := color.RGBA{200, 100, 40, 255}
		if mode == nativeImageOnionSkin {
			want = color.RGBA{40, 80, 160, 255}
		}
		if got := sample(); got != want {
			t.Fatalf("mode %v at 0: %v want %v", mode, got, want)
		}
		h.s.imageSwipe, h.s.imageOpacity = 100, 100
		h.tt.Frame()
		want = color.RGBA{40, 80, 160, 255}
		if mode == nativeImageOnionSkin {
			want = color.RGBA{200, 100, 40, 255}
		}
		if got := sample(); got != want {
			t.Fatalf("mode %v at 100: %v want %v", mode, got, want)
		}
	}
	long := newNativeImageHarness(t, nativeImageTestDiff(t, 4096, 128), 900, 620, "en", true)
	if err := long.tt.Click("Natural pixels"); err != nil {
		t.Fatal(err)
	}
	long.tt.Frame()
	if long.s.imageScroll.MaxX <= 0 {
		t.Fatal("natural panorama cannot scroll horizontally")
	}
	r, _ := long.tt.Find("Image preview")
	long.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	long.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 600, 0)
	if long.s.imageScroll.X <= 0 {
		t.Fatal("panorama scroll did not move")
	}
	nativeImageScreenshot(t, long.tt, "panorama-natural-scroll")
	thumb := newNativeImageHarness(t, nativeImageTestDiff(t, 16, 16), 900, 620, "en", false)
	nativeImageScreenshot(t, thumb.tt, "thumbnail-no-upscale")
}

func TestNativeImageWorkersRejectStaleRepositoryDiffAndClose(t *testing.T) {
	for _, kind := range []string{"repository", "diff", "clear", "close"} {
		t.Run(kind, func(t *testing.T) {
			a := newApp(nil, nil)
			defer a.close()
			s := &nativeImageDiffState{}
			defer s.clearNativeImageDiff()
			updates := make(chan func(), 10)
			a.update = func(fn func()) { updates <- fn }
			a.loadNativeImageDiff(s, nativeImageTestDiff(t, 8, 8), "old-repo:old-diff")
			var old func()
			select {
			case old = <-updates:
			case <-time.After(10 * time.Second):
				t.Fatal("decode timed out")
			}
			switch kind {
			case "repository":
				a.repoSeq++
			case "diff":
				a.diffSeq++
			case "clear":
				s.clearNativeImageDiff()
			case "close":
				a.close()
			}
			old()
			if s.imagePrepared != nil {
				t.Fatal("stale decoded image polluted new view")
			}
		})
	}
	a := newApp(nil, nil)
	defer a.close()
	s := &nativeImageDiffState{}
	defer s.clearNativeImageDiff()
	updates := make(chan func(), 10)
	a.update = func(fn func()) { updates <- fn }
	p, err := prepareNativeImageDiff(a.ctx, nativeImageTestDiff(t, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	s.imagePrepared = p
	a.requestNativeImageDifference(s, image.Pt(8, 8))
	var stale func()
	select {
	case stale = <-updates:
	case <-time.After(10 * time.Second):
		t.Fatal("composite timed out")
	}
	s.clearNativeImageDiff()
	stale()
	if s.imageDifference != nil {
		t.Fatal("stale composite repopulated cleared diff")
	}
}

func TestNativeImageGIFTesterPlaybackAndReducedMotion(t *testing.T) {
	diff := backend.Diff{Kind: "image", Current: nativeImageTestGIF(t, []int{10, 20}, -1)}
	h := newNativeImageHarness(t, diff, 900, 620, "en", false)
	r, _ := h.tt.Find("Image preview")
	sample := func() color.RGBA { return h.tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)) }
	first := sample()
	// Video decode-ahead has a real goroutine. Yield only for its work, while
	// animation uses Tester time rather than sleeps or real wall-clock timing.
	for i := 0; i < 100 && sample() == first; i++ {
		runtime.Gosched()
		h.tt.Advance(20 * time.Millisecond)
		h.tt.Frame()
	}
	if sample() == first {
		t.Fatal("real native GIF playback never changed pixels")
	}
	h.tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if sample() != first {
		t.Fatal("reduced motion should display first native GIF frame")
	}
	nativeImageScreenshot(t, h.tt, "gif-reduced-motion")
}

func TestNativeImageLabelsMatchParentGeneratorInput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "references", "native-ui-image-labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var labels map[string]map[string]string
	if err = json.Unmarshal(data, &labels); err != nil {
		t.Fatal(err)
	}
	for key, values := range labels {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			if nativeImageLabel(locale, strings.TrimPrefix(key, "image.")) != values[locale] {
				t.Fatalf("label %s / %s drifted", key, locale)
			}
		}
	}
}
