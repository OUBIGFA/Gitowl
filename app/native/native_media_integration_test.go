package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestNativeMediaUsesOriginalDesktopKernel(t *testing.T) { exerciseOriginalKernelView(t, "media") }

// Only image bytes are fixtures. Routing, codecs, bounded pipe chunks, AppStore
// settings, MyGo controls, frame timing/alpha and player ownership are real.
func exerciseOriginalKernelMedia(t *testing.T, v originalGenerationView) {
	t.Helper()
	data, err := os.ReadFile("../../references/native-image-codecs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]struct {
		Contents, Previous    []byte
		Frames, Width, Height int
		Opaque                bool
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	filename := func(name string) string {
		if name == "apng" {
			return "animation.png"
		}
		if name == "jpeg-exif" {
			return "oriented.jpg"
		}
		return "image." + name
	}
	for name, f := range fixtures {
		if err = os.WriteFile(filepath.Join(v.repo, filename(name)), f.Previous, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	add := []string{"-C", v.repo, "add"}
	for name := range fixtures {
		add = append(add, filename(name))
	}
	v.git(add...)
	v.git("-C", v.repo, "commit", "-m", "Add codec baselines")
	for name, f := range fixtures {
		if err = os.WriteFile(filepath.Join(v.repo, filename(name)), f.Contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v.app.refreshCore()
	v.await(func() bool {
		return v.app.corePending == 0 && v.app.status != nil && len(v.app.status.Files) == len(fixtures)+2
	})
	for _, name := range []string{"apng", "webp", "ico", "avif", "jpeg-exif", "dds"} {
		t.Logf("codec %s", name)
		// Native file lists virtualize their rows. Scroll the real list instead
		// of treating an offscreen accessibility node as a clickable control.
		list, ok := v.tester.Find("Changed files")
		if !ok {
			t.Fatal("changed-file list missing")
		}
		v.tester.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, -10000)
		for n := 0; n < 6; n++ {
			if r, visible := v.tester.Find("Review " + filename(name)); visible && r.H > 0 {
				break
			}
			v.tester.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, 96)
		}
		v.click("Review " + filename(name))
		v.await(func() bool {
			file, _, selected := v.app.currentFile()
			return selected && file.Path == filename(name) && v.app.corePending == 0 && !v.app.loadingDiff && !v.app.imageLoading && (v.app.imagePrepared != nil || v.app.imageError != nil || name == "dds" && v.app.preview != nil && v.app.preview.kind == "binary")
		})
		if name == "dds" && v.app.preview.kind == "binary" {
			t.Log("DDS is disabled by the original production feature flag; no extra format entry invented")
			v.click("Collapse diff")
			continue
		}
		if v.app.imageError != nil {
			t.Fatalf("%s original Chromium decode: %v", name, v.app.imageError)
		}
		m := v.app.imagePrepared.current
		if m.width != fixtures[name].Width || m.height != fixtures[name].Height || m.animated != (fixtures[name].Frames > 1) {
			t.Fatalf("%s metadata %dx%d animated=%v", name, m.width, m.height, m.animated)
		}
		r, g, b, a := m.pixels.At(0, 0).RGBA()
		alphaWrong := a < 30000 || a > 36000
		if fixtures[name].Opaque {
			alphaWrong = a != 65535
		}
		if alphaWrong || r < a*9/10 || g > a/10 || b > a/10 {
			t.Fatalf("%s straight/premultiplied red alpha changed: %d %d %d %d", name, r, g, b, a)
		}
		if m.animated {
			dec, err := m.makeDecoder()
			if err != nil {
				t.Fatal(err)
			}
			first, err := dec.Next()
			if err != nil {
				t.Fatalf("%s first frame: %v", name, err)
			}
			second, err := dec.Next()
			if err != nil {
				t.Fatalf("%s second frame: %v", name, err)
			}
			if first.Duration != 70*time.Millisecond || second.Duration != 110*time.Millisecond || m.loops != 1 {
				t.Fatalf("%s timing/finite loops lost: %v %v %v", name, first.Duration, second.Duration, m.loops)
			}
			r, _, b, a := second.Image.At(0, 0).RGBA()
			if b < a*9/10 || r > a/10 {
				t.Fatal("animation second frame did not come from Chromium")
			}
			if closer, ok := dec.(interface{ Close() error }); ok {
				_ = closer.Close()
			}
		}
		if popup := v.app.coreSnapshot.CurrentPopup; popup != nil {
			t.Fatalf("unexpected original popup during %s media: %+v", name, popup.Content)
		}
		for _, mode := range []struct {
			label string
			mode  nativeImageDiffType
		}{{"Swipe", nativeImageSwipe}, {"Onion skin", nativeImageOnionSkin}, {"Difference", nativeImageDifference}, {"2-up", nativeImageTwoUp}} {
			v.click(mode.label)
			v.await(func() bool { return v.app.corePending == 0 && v.app.imageMode == mode.mode })
			if v.app.coreSnapshot.ImageDiffType != int(mode.mode) {
				t.Fatal("native mode did not round-trip through original AppStore")
			}
		}
		v.click("Collapse diff")
		if v.app.reviewingFile || !v.tester.HasText("Changed files") {
			t.Fatal("collapsing image review did not retain the file list")
		}
	}
	// Real menus/controls remain usable after the media traffic.
	v.tester.Key(ui.Cmd, ui.KeyA)
}
