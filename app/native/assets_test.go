package main

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayUsesPixelSizedAssetsAtTaskbarScales(t *testing.T) {
	for _, sample := range []struct {
		scale float64
		size  int
	}{{1, 16}, {1.25, 20}, {1.5, 24}, {1.75, 28}, {2, 32}, {2.5, 40}, {3, 48}, {4, 64}} {
		data, err := loadTrayPNG("resources", sample.scale)
		if err != nil {
			t.Fatal(err)
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != sample.size || config.Height != sample.size {
			t.Fatalf("scale=%v: expected %dx%d tray image, got %+v, %v", sample.scale, sample.size, sample.size, config, err)
		}
	}
}
