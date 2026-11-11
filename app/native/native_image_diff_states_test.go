package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func TestNativeImageShortReviewAreaAndTextScaling(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark%v", locale, dark), func(t *testing.T) {
				h := newNativeImageHarness(t, nativeImageTestDiff(t, 600, 400), 900, 620, locale, dark)
				// A bounded 220-DIP preview leaves room for the real parent title/composer;
				// this is not a test that gives an image component the whole 620-DIP window.
				h.tt = ui.NewTester(func(c *ui.Context) {
					setWorkspaceTheme(c)
					ui.Box(c).Width(620).Height(220).Margin(120, 0, 0, 256).Children(func() {
						h.a.nativeImageDiffView(c, h.s, func(mode nativeImageDiffType) { h.s.setNativeImageDiffType(mode) })
					})
				}, 900, 620)
				h.tt.SetLocale(locale)
				h.tt.SetDark(dark)
				h.tt.SetPreferences(ui.Preferences{TextScale: 1.25})
				for _, mode := range []nativeImageDiffType{nativeImageTwoUp, nativeImageSwipe, nativeImageOnionSkin, nativeImageDifference} {
					h.s.setNativeImageDiffType(mode)
					h.tt.Frame()
					h.settle(t)
					r, ok := h.tt.Find(nativeImageLabel(locale, "preview"))
					if !ok || r.H < 16 || r.Y+r.H > 341 {
						t.Fatalf("short review area escaped parent: %+v %v", r, ok)
					}
					for _, key := range []string{"twoUp", "swipe", "onion", "difference", "natural"} {
						visibleControl(t, h.tt, nativeImageLabel(locale, key), 900, 620)
					}
					nativeImageScreenshot(t, h.tt, fmt.Sprintf("short-220-%s-dark%v-mode%d", locale, dark, mode))
				}
			})
		}
	}
}

func TestNativeImageDifferenceStatusUsesPreviewWidth(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, natural := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/natural%v", locale, natural), func(t *testing.T) {
				h := newNativeImageHarness(t, nativeImageTestDiff(t, 17, 13), 900, 620, locale, false)
				h.s.imageNatural = natural
				h.s.setNativeImageDiffType(nativeImageDifference)
				// Keep the real worker's update queued so this frame deterministically
				// exercises pending presentation instead of racing a fast tiny decode.
				h.tt.Frame()
				preview, ok := h.tt.Find(nativeImageLabel(locale, "preview"))
				if !ok {
					t.Fatal("missing image preview")
				}
				check := func(role ui.Role, label string) {
					t.Helper()
					if _, ok := h.tt.AccessByRole(role, label); !ok {
						t.Fatalf("missing accessible status %q", label)
					}
					r, ok := h.tt.Find(label)
					if !ok || r.W < 100 || r.H > 48 || r.X < preview.X || r.X+r.W > preview.X+preview.W || r.Y < preview.Y || r.Y+r.H > preview.Y+preview.H {
						t.Fatalf("status squeezed into tiny image instead of readable preview: status=%+v preview=%+v", r, preview)
					}
				}
				check(ui.RoleStatus, nativeImageLabel(locale, "computing"))
				h.s.imageDifferenceError = errors.New("Difference decoder could not read this image")
				h.tt.Frame()
				check(ui.RoleAlert, nativeImageLabel(locale, "failed"))
				details, ok := h.tt.Find(h.s.imageDifferenceError.Error())
				if !ok || details.W < 100 || details.Y+details.H > preview.Y+preview.H {
					t.Fatalf("difference error details are clipped: %+v", details)
				}
			})
		}
	}
}

func TestNativeImageSingleSidedLoadingAndErrors(t *testing.T) {
	// A real two-frame, one-pixel APNG generated with Pillow, not a first-frame
	//-only fixture. Until MyGo supports its timeline it must fail explicitly.
	apng, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAACGFjVEwAAAACAAAAAPONk3AAAAAaZmNUTAAAAAAAAAABAAAAAQAAAAAAAAAAAAEACgAAWn8w0AAAAA1JREFUeJxj+M/A8B8ABQAB/4mZPR0AAAAaZmNUTAAAAAEAAAABAAAAAQAAAAAAAAAAAAEACgAAwQzaBAAAABFmZEFUAAAAAnicY2Bg+P8fAAMCAf/1e6XXAAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareNativeImageDiff(context.Background(), backend.Diff{Kind: "image", Current: &backend.Blob{Contents: apng, MediaType: "image/png"}}); err == nil {
		t.Fatal("APNG silently displayed only its default image")
	}
	for _, current := range []bool{false, true} {
		diff := nativeImageTestDiff(t, 32, 24)
		if current {
			diff.Previous = nil
		} else {
			diff.Current = nil
		}
		h := newNativeImageHarness(t, diff, 900, 620, "en", false)
		h.s.setNativeImageDiffType(nativeImageDifference)
		h.tt.Frame()
		if _, ok := h.tt.AccessByRole(ui.RoleRadio, "Difference"); ok {
			t.Fatal("single image offers an impossible comparison")
		}
		h.s.imageLoading = true
		h.tt.Frame()
		if _, ok := h.tt.AccessByRole(ui.RoleStatus, "Loading image…"); !ok {
			t.Fatal("loading not announced")
		}
		h.s.imageLoading = false
		h.s.imageError = errors.New("unsupported image format")
		h.tt.Frame()
		if _, ok := h.tt.AccessByRole(ui.RoleAlert, "Image preview unavailable"); !ok {
			t.Fatal("decode error is not an alert")
		}
		nativeImageScreenshot(t, h.tt, fmt.Sprintf("error-current%v", current))
	}
	for _, element := range []string{"text", "image", "pattern", "marker", "filter", "foreignObject", "animate"} {
		blob := &backend.Blob{MediaType: "image/svg+xml", Contents: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"><` + element + ` /></svg>`)}
		if _, err := prepareNativeImageDiff(context.Background(), backend.Diff{Kind: "image", Current: blob}); err == nil {
			t.Fatalf("unsupported SVG <%s> silently omitted", element)
		}
	}
}

func TestNativeImageNativeModeKeyboardAndNoOptimisticPersistence(t *testing.T) {
	h := newNativeImageHarness(t, nativeImageTestDiff(t, 32, 32), 900, 620, "en", false)
	if err := h.tt.Click("2-up"); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyRight)
	if h.s.imageMode != nativeImageSwipe {
		t.Fatal("native radio-group arrow navigation did not reach Swipe")
	}
	h.tt.Key(0, ui.KeyRight)
	if h.s.imageMode != nativeImageOnionSkin || h.s.imageOpacity != 1 {
		t.Fatal("Onion Skin lost upstream initial opacity")
	}
	// The real parent acknowledges via snapshot. A request that has not received
	// that snapshot (or failed) must not turn into a native persisted preference.
	var requested nativeImageDiffType
	h.tt = ui.NewTester(func(c *ui.Context) { h.a.nativeImageDiffView(c, h.s, func(m nativeImageDiffType) { requested = m }) }, 620, 320)
	if err := h.tt.Click("2-up"); err != nil {
		t.Fatal(err)
	}
	if requested != nativeImageTwoUp || h.s.imageMode != nativeImageOnionSkin {
		t.Fatal("mode mutated before original kernel acknowledged")
	}
}

func TestNativeImageRealPhotoAndTransparencyFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "test", "fixtures", "repo-with-image-changes", "modified-image.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	// Existing upstream licensed fixture, no new downloaded/shipping artwork.
	photo := &backend.Blob{Contents: data, Bytes: len(data), MediaType: "image/jpeg"}
	diff := backend.Diff{Kind: "image", Previous: photo, Current: photo}
	h := newNativeImageHarness(t, diff, 900, 620, "en", true)
	for _, mode := range []nativeImageDiffType{nativeImageTwoUp, nativeImageSwipe, nativeImageOnionSkin, nativeImageDifference} {
		h.s.setNativeImageDiffType(mode)
		h.s.imageSwipe, h.s.imageOpacity = 50, 50
		h.tt.Frame()
		h.settle(t)
		nativeImageScreenshot(t, h.tt, fmt.Sprintf("upstream-photo-mode%d", mode))
	}
	h.tt.SetScale(2)
	h.tt.Frame()
	h.settle(t)
	nativeImageScreenshot(t, h.tt, "photo-difference-scale2")
	transparent := newNativeImageHarness(t, backend.Diff{Kind: "image", Previous: nativeImageTestBlob(t, 160, 160, color.Transparent), Current: nativeImageTestBlob(t, 96, 96, color.NRGBA{160, 80, 40, 128})}, 900, 620, "zh-Hans", true)
	for _, mode := range []nativeImageDiffType{nativeImageTwoUp, nativeImageSwipe, nativeImageOnionSkin, nativeImageDifference} {
		transparent.s.setNativeImageDiffType(mode)
		transparent.s.imageSwipe, transparent.s.imageOpacity = 50, 50
		transparent.tt.Frame()
		transparent.settle(t)
		nativeImageScreenshot(t, transparent.tt, fmt.Sprintf("transparency-unequal-mode%d", mode))
	}
}

func TestNativeImageTallNaturalPixels(t *testing.T) {
	h := newNativeImageHarness(t, nativeImageTestDiff(t, 128, 4096), 900, 620, "zh-Hans", true)
	if err := h.tt.Click(nativeImageLabel("zh-Hans", "natural")); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.s.imageScroll.MaxY <= 0 {
		t.Fatal("natural tall image cannot scroll vertically")
	}
	r, _ := h.tt.Find(nativeImageLabel("zh-Hans", "preview"))
	h.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 600)
	if h.s.imageScroll.Y <= 0 {
		t.Fatal("vertical native scroll did not move")
	}
	nativeImageScreenshot(t, h.tt, "tall-natural-scroll")
}

func TestNativeImageNoDecodeOnUnchangedBuildAndStaleResize(t *testing.T) {
	h := newNativeImageHarness(t, nativeImageTestDiff(t, 100, 100), 900, 620, "en", false)
	original := h.s.imagePrepared
	source := original.current.source
	for i := 0; i < 10; i++ {
		h.tt.Frame()
	}
	if h.s.imagePrepared != original || h.s.imagePrepared.current.source != source {
		t.Fatal("build re-decoded source")
	}
	h.s.setNativeImageDiffType(nativeImageDifference)
	h.tt.Frame()
	h.settle(t)
	composite, epoch := h.s.imageDifference, h.s.imageDifferenceEpoch
	for i := 0; i < 10; i++ {
		h.tt.Frame()
	}
	if h.s.imageDifference != composite || h.s.imageDifferenceEpoch != epoch {
		t.Fatal("unchanged frame rebuilt difference")
	}
	// Resizing may finish before an obsolete result's UI callback executes.
	a := newApp(nil, nil)
	defer a.close()
	s := &nativeImageDiffState{}
	defer s.clearNativeImageDiff()
	updates := make(chan func(), 10)
	a.update = func(fn func()) { updates <- fn }
	s.imagePrepared = original
	h.s.imagePrepared = nil // ownership transfers only within this test
	a.requestNativeImageDifference(s, image.Pt(80, 80))
	var first func()
	select {
	case first = <-updates:
	case <-time.After(10 * time.Second):
		t.Fatal("first resize timeout")
	}
	a.requestNativeImageDifference(s, image.Pt(40, 40))
	var second func()
	select {
	case second = <-updates:
	case <-time.After(10 * time.Second):
		t.Fatal("second resize timeout")
	}
	second()
	current := s.imageDifference
	first()
	if s.imageDifference != current || s.imageDifferenceSize != image.Pt(40, 40) {
		t.Fatal("obsolete resize replaced current composite")
	}
}
