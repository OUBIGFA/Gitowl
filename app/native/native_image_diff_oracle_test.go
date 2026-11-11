// CSS oracle inputs are generated from real production preparation/compositing.
// The optional files let an external Chromium run compare the upstream actual
// mix-blend-mode styling, without embedding a browser in the shipped view.
package main

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

func TestNativeImageExportCSSOracle(t *testing.T) {
	if os.Getenv("GITOWL_IMAGE_SCREENSHOTS") != "1" {
		t.Skip("set GITOWL_IMAGE_SCREENSHOTS=1 to export Chromium oracle inputs")
	}
	type sample struct {
		Name          string `json:"name"`
		Before, After *backend.Blob
		Width, Height int
	}
	cases := []struct {
		name                 string
		b, s                 color.NRGBA
		bw, bh, sw, sh, w, h int
	}{
		{"opaque", color.NRGBA{40, 80, 160, 255}, color.NRGBA{200, 100, 40, 255}, 8, 8, 8, 8, 8, 8},
		{"alpha", color.NRGBA{200, 100, 50, 128}, color.NRGBA{80, 160, 240, 128}, 8, 8, 8, 8, 8, 8},
		{"nonoverlap", color.NRGBA{200, 100, 50, 128}, color.NRGBA{80, 160, 240, 128}, 8, 8, 4, 4, 8, 8},
		{"fractional", color.NRGBA{200, 100, 50, 128}, color.NRGBA{80, 160, 240, 128}, 14, 5, 5, 14, 9, 9},
		{"samewhite", color.NRGBA{255, 255, 255, 128}, color.NRGBA{255, 255, 255, 128}, 8, 8, 8, 8, 8, 8},
	}
	dir := filepath.Join("..", "..", "_temp", "native-full-ui", "image-diff")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var inputs []sample
	for _, tc := range cases {
		b, s := nativeImageTestBlob(t, tc.bw, tc.bh, tc.b), nativeImageTestBlob(t, tc.sw, tc.sh, tc.s)
		p, err := prepareNativeImageDiff(context.Background(), backend.Diff{Kind: "image", Previous: b, Current: s})
		if err != nil {
			t.Fatal(err)
		}
		d, err := newNativeImageDifferenceDecoder(context.Background(), p, image.Pt(tc.w, tc.h))
		if err != nil {
			t.Fatal(err)
		}
		frame, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "native-oracle-"+tc.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, frame.Image); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		p.close()
		inputs = append(inputs, sample{tc.name, b, s, tc.w, tc.h})
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "oracle-input.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
