package main

import (
	"fmt"
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

func dangerContrast(a, b color.RGBA) float64 {
	luminance := func(c color.RGBA) float64 {
		linear := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
	}
	x, y := luminance(a), luminance(b)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

// Inspect the actual button pixels: the old theme's ordinary danger text was
// also used on the opposite primary fill, making both appearances unreadable.
func TestDestructiveButtonTextContrastsWithItsActualSurface(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, primary := range []bool{false, true} {
			for _, active := range []bool{false, true} {
				t.Run(fmt.Sprintf("dark=%t/primary=%t/active=%t", dark, primary, active), func(t *testing.T) {
					action := backend.KernelDialogAction{ID: "confirm", LabelKey: "repositoryDialog.removeRepository", Primary: primary, Destructive: true}
					tt := ui.NewTester(func(c *ui.Context) {
						setWorkspaceTheme(c)
						ui.Column(c).Fill().Padding(24).Background(c.Theme().Background).Children(func() {
							renderNativeDialogButton(c, action, false, func(backend.KernelDialogAction) {})
						})
					}, 400, 140)
					tt.SetDark(dark)
					tt.SetWindowActive(active)
					tt.Advance(time.Second)
					info, ok := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", action.LabelKey))
					if !ok {
						t.Fatal("destructive action is not accessible")
					}
					r := info.Bounds
					for _, state := range []string{"normal", "hover", "pressed"} {
						switch state {
						case "hover":
							tt.Move(r.X+r.W/2, r.Y+r.H/2)
						case "pressed":
							tt.Press(r.X+r.W/2, r.Y+r.H/2)
						}
						tt.Advance(time.Second)
						image := tt.Image()
						background := image.RGBAAt(int(r.X+r.W/2), int(r.Y+5))
						contrast := 1.0
						// Exclude the border/focus ring; the strongest interior pixels
						// are the glyphs, including their antialiased edges.
						for y := int(r.Y + 8); y < int(r.Y+r.H-8); y++ {
							for x := int(r.X + 12); x < int(r.X+r.W-12); x++ {
								contrast = max(contrast, dangerContrast(image.RGBAAt(x, y), background))
							}
						}
						if contrast < 4.5 {
							t.Errorf("%s destructive label contrast is %.2f:1, want at least 4.5:1", state, contrast)
						}
					}
				})
			}
		}
	}
}
