package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeImageMagickPreservesCodecFrames(t *testing.T) {
	exe := os.Getenv("GITOWL_MAGICK")
	if exe == "" {
		t.Skip("set GITOWL_MAGICK to the pinned official native decoder")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "references", "native-image-codecs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures map[string]struct {
		MediaType, Contents   string
		Width, Height, Frames int
		Opaque                bool
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			bytes, err := base64.StdEncoding.DecodeString(fixture.Contents)
			if err != nil {
				t.Fatal(err)
			}
			media := nativeMedia{executable: exe}
			blob := media.retain(bytes, fixture.MediaType)
			for index := 0; index < fixture.Frames; index++ {
				pixels, metadata, err := media.frame(context.Background(), blob.MediaID, index)
				if err != nil {
					t.Fatal(err)
				}
				if metadata.Width != fixture.Width || metadata.Height != fixture.Height || metadata.Frames != fixture.Frames || len(pixels.Pix) != fixture.Width*fixture.Height*4 {
					t.Fatalf("invalid frame %+v", metadata)
				}
				pixel := pixels.Pix[:4]
				if index == 0 && (pixel[0] < 245 || pixel[1] > 10 || pixel[2] > 10) {
					t.Fatalf("red frame pixels %v", pixel)
				}
				if index == 1 && (pixel[2] < 245 || pixel[0] > 10) {
					t.Fatalf("blue frame pixels %v", pixel)
				}
				if fixture.Opaque && pixel[3] != 255 || !fixture.Opaque && (pixel[3] < 120 || pixel[3] > 140) {
					t.Fatalf("alpha %v", pixel)
				}
				if fixture.Frames > 1 {
					expected := int64(70000)
					if index == 1 {
						expected = 110000
					}
					if metadata.Duration != expected || metadata.Repetitions != 1 {
						t.Fatalf("animation timing/loops %+v", metadata)
					}
				}
			}
		})
	}
}

func TestNativeImagePolicyRejectsExternalSVGImages(t *testing.T) {
	exe := os.Getenv("GITOWL_MAGICK")
	if exe == "" {
		t.Skip("requires packaged ImageMagick policy")
	}
	root := t.TempDir()
	path := filepath.Join(root, "private.svg")
	if err := os.WriteFile(path, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{filepath.ToSlash(path), "file:///" + filepath.ToSlash(path), "https://example.invalid/private.png"} {
		media := nativeMedia{executable: exe}
		blob := media.retain([]byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="2" height="2"><image xlink:href="%s" width="2" height="2"/></svg>`, target)), "image/svg+xml")
		if _, _, err := media.frame(context.Background(), blob.MediaID, 0); err == nil {
			t.Fatalf("external image accepted: %s", target)
		}
	}
}
