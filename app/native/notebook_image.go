package main

import (
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"

	rich "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/editor"
	"github.com/egoist/mygo/ui"
)

// Resolve Markdown images relative to the document, under the same project
// boundary as the file tree. Opening a note never contacts a remote host.
func (d *notebookDocument) loadImage(src string) rich.ImageResource {
	u, err := url.Parse(src)
	if err != nil {
		return rich.ImageResource{Problem: rich.ImageInvalid}
	}
	if u.Scheme != "" || u.Host != "" {
		return rich.ImageResource{Problem: rich.ImageRemote}
	}
	name := filepath.Join(filepath.Dir(d.name), filepath.FromSlash(u.Path))
	root, err := notebookFileRoot(d.root, name)
	if err != nil {
		p := rich.ImageUnreadable
		if errors.Is(err, os.ErrNotExist) {
			p = rich.ImageMissing
		}
		return rich.ImageResource{Problem: p}
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return rich.ImageResource{Problem: rich.ImageUnreadable}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return rich.ImageResource{Problem: rich.ImageUnreadable}
	}
	size, _, err := image.DecodeConfig(f)
	if err != nil || size.Width <= 0 || size.Height <= 0 || int64(size.Width)*int64(size.Height) > 32_000_000 {
		return rich.ImageResource{Problem: rich.ImageUnsupported}
	}
	if _, err := f.Seek(0, 0); err != nil {
		return rich.ImageResource{Problem: rich.ImageUnreadable}
	}
	pixels, _, err := image.Decode(f)
	if err != nil {
		return rich.ImageResource{Problem: rich.ImageInvalid}
	}
	return rich.ImageResource{Bitmap: ui.NewBitmap(pixels), Width: size.Width, Height: size.Height, Local: true}
}
