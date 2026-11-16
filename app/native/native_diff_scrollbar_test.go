package main

import (
	"fmt"
	"image/color"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestDiffScrollbarSeparatesChangeMarkersFromThumb(t *testing.T) {
	var patch strings.Builder
	patch.WriteString("diff --git a/review.txt b/review.txt\n--- a/review.txt\n+++ b/review.txt\n@@ -1,80 +1,80 @@\n")
	for _, prefix := range []string{"-before", "+after"} {
		for line := range 80 {
			fmt.Fprintf(&patch, "%s %02d %s\n", prefix, line, strings.Repeat("review 中文 ", 12))
		}
	}
	for _, viewport := range []struct {
		width, height int
		scale         float32
		locale        string
	}{
		{900, 620, 1, "en"},
		{900, 620, 1.25, "zh-Hans"},
		{1920, 1080, 1, "en"},
		{1920, 1080, 1.25, "zh-Hans"},
	} {
		for _, dark := range []bool{false, true} {
			for _, wrap := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/dark=%t/wrap=%t", viewport.locale, viewport.width, dark, wrap), func(t *testing.T) {
					application := designEvidenceApp(t, time.Now(), "review")
					stdout, output := io.Pipe()
					input, stdin := io.Pipe()
					application.core = backend.NewKernel(stdout, stdin, nil)
					updates := make(chan func(), 32)
					application.update = func(update func()) { updates <- update }
					go io.Copy(io.Discard, input)
					t.Cleanup(func() { application.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() })
					text := patch.String()
					preview, err := preparePreview(backend.Diff{Kind: "text", Text: &text}, "review.txt")
					if err != nil {
						t.Fatal(err)
					}
					application.preview, application.wrap = preview, wrap
					application.coreSnapshot.Workspace.SideBySide = true
					var theme ui.Theme
					tester := ui.NewTester(func(context *ui.Context) {
						application.view(context)
						theme = *context.Theme()
					}, viewport.width, viewport.height)
					tester.SetLocale(viewport.locale)
					tester.SetDark(dark)
					tester.SetScale(viewport.scale)
					state := preview.patches[0].state
					state.SetScrollY(1e9)
					tester.Frame()
					tester.Advance(time.Second)
					if state.ScrollY() <= 0 {
						t.Fatal("the review fixture did not need a vertical scrollbar")
					}
					bounds := visibleControl(t, tester, locales.In(viewport.locale).DiffTitle(), viewport.width, viewport.height)
					frame := tester.Image()
					modified := editor.DefaultTheme(&theme).Info
					markerPixels := 0
					for row := int(bounds.Y * viewport.scale); row < int((bounds.Y+bounds.H)*viewport.scale); row++ {
						for column := int((bounds.X + bounds.W - 18) * viewport.scale); column < int((bounds.X+bounds.W)*viewport.scale); column++ {
							if frame.RGBAAt(column, row) == (color.RGBA{R: modified.R, G: modified.G, B: modified.B, A: 255}) {
								markerPixels++
							}
						}
					}
					if markerPixels == 0 {
						t.Error("modified ranges must remain visible as semantic change markers, not a dark text-colored edge")
					}
					thumb := theme.Scrollbar
					background := theme.Background
					blend := func(foreground, behind uint8) uint8 {
						return uint8((int(foreground)*int(thumb.A) + int(behind)*(255-int(thumb.A)) + 127) / 255)
					}
					want := color.RGBA{R: blend(thumb.R, background.R), G: blend(thumb.G, background.G), B: blend(thumb.B, background.B), A: 255}
					pointerX, pointerY := bounds.X+bounds.W-7.5, bounds.Y+float32(state.ViewportHeight())-18
					checkThumb := func(pointerY float32) {
						t.Helper()
						column, row := int(pointerX*viewport.scale), int(pointerY*viewport.scale)
						got := tester.Image().RGBAAt(column, row)
						if got.A != want.A || max(got.R, want.R)-min(got.R, want.R) > 1 || max(got.G, want.G)-min(got.G, want.G) > 1 || max(got.B, want.B)-min(got.B, want.B) > 1 {
							t.Errorf("the scrollbar thumb mixes with a change marker at (%d,%d): got %v, want %v", column, row, got, want)
						}
					}
					checkThumb(pointerY)
					saveDesignEvidence(t, tester, fmt.Sprintf("diff-scrollbar-%s-%d-dark-%t-wrap-%t.png", viewport.locale, viewport.width, dark, wrap))
					previousOffset := state.ScrollY()
					tester.Press(pointerX, pointerY)
					tester.Move(pointerX, pointerY-40)
					checkThumb(pointerY - 40)
					tester.Release(pointerX, pointerY-40)
					if state.ScrollY() >= previousOffset {
						t.Fatal("dragging the native scrollbar did not move the diff")
					}
				})
			}
		}
	}
}
