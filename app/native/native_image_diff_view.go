package main

import (
	"encoding/base64"
	"fmt"
	"image"
	"math"

	"github.com/egoist/mygo/ui"
)

// The original checkboard-background.scss's 20px bitmap, not a drawn icon or
// SVG path. Native Images tile it only across the visible part of the canvas.
var nativeImageChecker = func() ui.ImageSource {
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAABQAAAAUAQMAAAC3R49OAAAABlBMVEX5+fn///8pDrwNAAAAFElEQVQI12NgsP/AQAz+f4CBGAwAJIIdTTn0+w0AAAAASUVORK5CYII=")
	if err != nil {
		panic(err)
	}
	source, err := ui.DecodeImage(data)
	if err != nil {
		panic(err)
	}
	return source
}()

// The parent wires onMode to an original-kernel command; the chosen segment is
// read only from the acknowledged IAppState snapshot. A failed persistence must
// not paint a native-only preference as if it were saved.
func (a *app) nativeImageDiffView(c *ui.Context, s *nativeImageDiffState, onMode func(nativeImageDiffType)) {
	label := func(key string) string { return nativeImageLabel(c.Locale(), key) }
	ui.Column(c.Key("native-image-diff")).Fill().MinHeight(0).MinWidth(0).Gap(8).Children(func() {
		if s.imageLoading {
			ui.Text(c, label("loading")).Role(ui.RoleStatus)
			return
		}
		if s.imageError != nil {
			ui.Text(c, label("failed")).Role(ui.RoleAlert)
			ui.Text(c, s.imageError.Error()).TextColor(c.Theme().TextMuted)
			return
		}
		p := s.imagePrepared
		if p == nil {
			ui.Text(c, label("empty")).Role(ui.RoleStatus)
			return
		}
		both := p.previous != nil && p.current != nil
		ui.Row(c).Gap(12).Wrap().Children(func() {
			if both {
				selected := int(s.imageMode)
				ui.Segmented(c.Key("image-mode"), &selected, label("twoUp"), label("swipe"), label("onion"), label("difference")).Label(label("mode")).MaxWidthPercent(100).Disabled(onMode == nil).OnChange(func() {
					if onMode != nil {
						onMode(nativeImageDiffType(selected))
					}
				})
			}
			ui.Icon(c, icons["image"]).FontSize(18).TextColor(c.Theme().TextMuted)
			ui.Checkbox(c.Key("image-natural"), &s.imageNatural, label("natural")).OnChange(func() { s.imageScroll = ui.ScrollState{}; s.clearNativeImageDifference() })
		})
		if both && (s.imageMode == nativeImageSwipe || s.imageMode == nativeImageOnionSkin) {
			value, name := &s.imageSwipe, label("swipePosition")
			if s.imageMode == nativeImageOnionSkin {
				value, name = &s.imageOpacity, label("opacity")
			}
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, name).TextColor(c.Theme().TextMuted)
				ui.Slider(c.Key("image-slider"), value, 0, 100).Step(0.1).Label(name).Grow(1).MinWidth(80)
				ui.Text(c, fmt.Sprintf("%.1f%%", *value)).Width(64).FontFeatures("tnum")
			})
		}
		ui.Row(c).Gap(16).Children(func() {
			for i, m := range []*nativeImageDiffMedia{p.previous, p.current} {
				if m == nil {
					continue
				}
				key := "previous"
				if i == 1 {
					key = "current"
				}
				ui.Column(c.Key(key + "-metadata")).Grow(1).Basis(0).MinWidth(0).Children(func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.Text(c, label(key)).FontWeight(500).Shrink(0)
						if both && i == 1 {
							delta, text := p.current.bytes-p.previous.bytes, label("sameBytes")
							if delta != 0 {
								text = fmt.Sprintf("%+d B", delta)
								if p.previous.bytes > 0 {
									text += fmt.Sprintf(" (%d%%)", int(math.Floor(float64(p.current.bytes)/float64(p.previous.bytes)*100+0.5)))
								}
							}
							ui.Text(c, text).SingleLine().Grow(1).MinWidth(0).TextColor(c.Theme().TextMuted).FontFeatures("tnum").Tooltip(label("byteChange") + ": " + text)
						}
					})
					ui.Text(c, fmt.Sprintf("%d × %d px · %s", m.width, m.height, nativeImageBytes(m.bytes))).TextColor(c.Theme().TextMuted).FontFeatures("tnum")
				})
			}
		})
		stage := ui.ScrollBoth(c.Key("image-stage")).Grow(1).MinHeight(0).FillWidth().TrackScroll(&s.imageScroll).KeepScrollbars(true).Label(label("preview"))
		bounds := stage.Bounds()
		if bounds.W <= 0 || bounds.H <= 0 {
			c.After(0)
			return
		}
		stage.Children(func() {
			availableW, availableH := max(1, bounds.W-16), max(1, bounds.H-16)
			mode := s.imageMode
			if !both {
				mode = nativeImageTwoUp
			}
			cw, ch := p.size(availableW, availableH, s.imageNatural)
			if mode == nativeImageTwoUp && both {
				cw, ch = p.size(max(1, (availableW-16)/2), availableH, s.imageNatural)
				cw = cw*2 + 16
			}
			if mode == nativeImageDifference {
				// Resize invalidates only the worker's composite. Status text belongs
				// to the readable preview, not a canvas that may be only one pixel wide.
				a.requestNativeImageDifference(s, image.Pt(int(math.Ceil(float64(cw))), int(math.Ceil(float64(ch)))))
				statusErr := s.imageDifferenceError
				if statusErr == nil && s.imageDifference != nil {
					statusErr = s.imageDifference.Err()
				}
				if statusErr != nil || s.imageDifference == nil {
					ui.Column(c.Key("image-difference-status")).Width(availableW).MinHeight(availableH).Padding(16).Gap(8).Children(func() {
						if statusErr != nil {
							ui.Text(c, label("failed")).FillWidth().Role(ui.RoleAlert)
							ui.Text(c, statusErr.Error()).FillWidth().TextColor(c.Theme().TextMuted)
						} else {
							ui.Text(c, label("computing")).FillWidth().Role(ui.RoleStatus)
						}
					})
					return
				}
			}
			ui.Box(c).Size(max(bounds.W, cw+16), max(bounds.H, ch+16)).Shrink(0).Center().Children(func() {
				canvas := ui.Box(c.Key("image-canvas")).Size(cw, ch).Shrink(0)
				canvas.Children(func() {
					switch mode {
					case nativeImageTwoUp:
						if both {
							ui.Row(c).Fill().Gap(16).Children(func() {
								for i, m := range []*nativeImageDiffMedia{p.previous, p.current} {
									side := "previous"
									if i == 1 {
										side = "current"
									}
									ui.Box(c.Key(side)).Size((cw-16)/2, ch).Center().Children(func() { nativeImageMediaView(c, m, (cw-16)/2, ch, 1, label(side), true) })
								}
							})
						} else {
							m, name := p.current, label("current")
							if m == nil {
								m, name = p.previous, label("previous")
							}
							nativeImageMediaView(c, m, cw, ch, 1, name, true)
						}
					case nativeImageSwipe:
						nativeImageCheckerView(c, cw, ch)
						// Exactly the upstream floor(inset) boundaries, including the possible
						// one-pixel overlap at fractional slider positions. Current paints last.
						left := float32(math.Floor(float64(cw) * s.imageSwipe / 100))
						right := cw - float32(math.Floor(float64(cw)*(1-s.imageSwipe/100)))
						ui.Box(c.Key("swipe-previous-clip")).Absolute().Left(0).Top(0).Size(right, ch).Clip().Children(func() {
							ui.Box(c).Absolute().Left(0).Top(0).Size(cw, ch).Children(func() { nativeImageMediaView(c, p.previous, cw, ch, 1, label("previous"), false) })
						})
						ui.Box(c.Key("swipe-current-clip")).Absolute().Left(left).Top(0).Size(cw-left, ch).Clip().Children(func() {
							ui.Box(c).Absolute().Left(-left).Top(0).Size(cw, ch).Children(func() { nativeImageMediaView(c, p.current, cw, ch, 1, label("current"), false) })
						})
					case nativeImageOnionSkin:
						nativeImageCheckerView(c, cw, ch)
						nativeImageMediaView(c, p.previous, cw, ch, 1, label("previous"), false)
						nativeImageMediaView(c, p.current, cw, ch, float32(s.imageOpacity/100), label("current"), false)
					case nativeImageDifference:
						ui.Video(c, s.imageDifference).Fill().Fit(ui.Contain).Label(label("difference"))
					}
				})
			})
		})
	})
}

func nativeImageMediaView(c *ui.Context, m *nativeImageDiffMedia, w, h, opacity float32, label string, checker bool) {
	iw, ih := nativeImageFit(float32(m.width), float32(m.height), w, h)
	ui.Box(c).Absolute().Left((w-iw)/2).Top((h-ih)/2).Size(iw, ih).Opacity(opacity).Children(func() {
		if checker {
			nativeImageCheckerView(c, iw, ih)
		}
		if m.player != nil {
			ui.Video(c, m.player).Absolute().Fill().Fit(ui.ScaleDown).Label(label)
			if m.player.Err() != nil {
				ui.Text(c, m.player.Err().Error()).Role(ui.RoleAlert)
			}
		} else {
			ui.Image(c, m.source).Absolute().Fill().Fit(ui.ScaleDown).Label(label)
		}
	})
}

func nativeImageCheckerView(c *ui.Context, w, h float32) {
	box := ui.Box(c.Key("transparency")).Absolute().Left(0).Top(0).Size(w, h).Clip()
	b := box.Bounds()
	if b.W <= 0 {
		c.After(0)
		return
	}
	// Bounds are window coordinates even inside a scroller. Don't build tens of
	// thousands of offscreen tiles for natural-pixel panoramas.
	viewW, viewH := c.Size()
	x0, y0 := max(0, int(-b.X/20)*20), max(0, int(-b.Y/20)*20)
	x1, y1 := min(int(math.Ceil(float64(w))), int(viewW-b.X)+20), min(int(math.Ceil(float64(h))), int(viewH-b.Y)+20)
	box.Children(func() {
		for y := y0; y < y1; y += 20 {
			for x := x0; x < x1; x += 20 {
				ui.Image(c, nativeImageChecker).Absolute().Left(float32(x)).Top(float32(y)).Size(20, 20)
			}
		}
	})
}
func nativeImageBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.2f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MiB", float64(n)/(1024*1024))
}

// Image controls use the same generated, system-language-aware catalog as all
// other native UI. The source labels stay in the reviewable references input.
func nativeImageLabel(locale, key string) string {
	return nativeDialogLabel(locale, "image."+key)
}
