package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	rich "github.com/OUBIGFA/Gitowl/app/native/internal/arcmd/editor"
	"github.com/egoist/mygo/ui"
)

const notebookImageLimit = 16 << 20

var notebookImageWorkers = make(chan struct{}, 4)

// Remote images are document content. Use an anonymous request and check
// the actual destination on each connection, including after redirects.
var notebookPublicTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 10 * time.Second, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		endpoint, err := netip.ParseAddrPort(address)
		if err != nil {
			return err
		}
		ip := endpoint.Addr().Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return errors.New("image destination is not public")
		}
		return nil
	}}
	transport.DialContext = dialer.DialContext
	return transport
}()

// An explicit load may use the user's network proxy or an intranet host.
func notebookImageHTTPClient(explicit bool) *http.Client {
	var transport http.RoundTripper = notebookPublicTransport
	if explicit {
		transport = http.DefaultTransport
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 || !notebookWebImage(req.URL) {
			return errors.New("invalid image redirect")
		}
		req.Header.Del("Referer")
		return nil
	}}
}

func notebookWebImage(u *url.URL) bool {
	return (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func (a *app) notebookImage(d *notebookDocument, src string, explicit bool) rich.ImageResource {
	u, err := url.Parse(src)
	if err != nil {
		return rich.ImageResource{Problem: rich.ImageInvalid}
	}
	if u.Host != "" && u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Scheme == "" && u.Host == "" {
		return d.localImage(u)
	}
	if !notebookWebImage(u) {
		return rich.ImageResource{Problem: rich.ImageInvalid}
	}
	if resource, ok := d.webImages[src]; ok {
		return resource
	}
	if d.imageContext == nil {
		d.imageContext, d.cancelImages = context.WithCancel(a.ctx)
		d.webImages = make(map[string]rich.ImageResource)
	}
	loading := rich.ImageResource{Loading: true}
	d.webImages[src] = loading
	ctx := d.imageContext
	go func() {
		select {
		case notebookImageWorkers <- struct{}{}:
		case <-ctx.Done():
			return
		}
		resource := fetchNotebookImage(ctx, notebookImageHTTPClient(explicit), u.String())
		<-notebookImageWorkers
		if ctx.Err() != nil {
			return
		}
		a.update(func() {
			if ctx.Err() != nil {
				return
			}
			d.webImages[src] = resource
			if d.rich != nil {
				d.rich.InvalidateImage(src)
			}
		})
	}()
	return loading
}

func fetchNotebookImage(ctx context.Context, client *http.Client, src string) rich.ImageResource {
	failed := rich.ImageResource{Problem: rich.ImageRemote}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return failed
	}
	req.Header.Set("Accept", "image/*")
	resp, err := client.Do(req)
	if err != nil {
		return failed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return failed
	}
	if resp.ContentLength > notebookImageLimit {
		return rich.ImageResource{Problem: rich.ImageTooLarge}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, notebookImageLimit+1))
	if err != nil {
		return failed
	}
	if len(data) > notebookImageLimit {
		return rich.ImageResource{Problem: rich.ImageTooLarge}
	}
	resource := decodeNotebookImage(bytes.NewReader(data))
	if resource.Bitmap != nil && len(data) >= 13 && bytes.HasPrefix(data, []byte("GIF8")) && notebookGIFAnimation(data, resource.Width, resource.Height) {
		// MyGo composites partial frames and honors transparency and disposal.
		if dec, err := ui.NewGIFDecoder(data); err == nil {
			for frame, err := dec.Next(); err == nil; frame, err = dec.Next() {
				resource.Frames = append(resource.Frames, ui.NewBitmap(frame.Image))
				resource.Delays = append(resource.Delays, frame.Duration)
			}
			resource.Bitmap = resource.Frames[0]
		}
	}
	return resource
}

// The native GIF decoder expands all frames. Inspect block lengths before
// decoding so a tiny download cannot allocate an unbounded animation.
func notebookGIFAnimation(data []byte, w, h int) bool {
	const pixelBudget = 16_000_000
	pos := 13
	tableSize := func(flags byte) int {
		if flags&0x80 == 0 {
			return 0
		}
		return 3 << ((flags & 7) + 1)
	}
	pos += tableSize(data[10])
	frames := 0
	for pos < len(data) {
		switch data[pos] {
		case 0x3b:
			return frames > 1
		case 0x21:
			pos += 2 // extension introducer and label
		case 0x2c:
			if len(data)-pos < 10 {
				return false
			}
			frames++
			if frames > 240 || w*h > pixelBudget/frames {
				return false
			}
			pos += 11 + tableSize(data[pos+9]) // descriptor, palette and LZW code size
		default:
			return false
		}
		for pos < len(data) {
			n := int(data[pos])
			pos += 1 + n
			if n == 0 {
				break
			}
		}
	}
	return false
}

func (d *notebookDocument) localImage(u *url.URL) rich.ImageResource {
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
	resource := decodeNotebookImage(f)
	resource.Local = true
	return resource
}

func decodeNotebookImage(f io.ReadSeeker) rich.ImageResource {
	size, _, err := image.DecodeConfig(f)
	if err != nil || size.Width <= 0 || size.Height <= 0 {
		return rich.ImageResource{Problem: rich.ImageUnsupported}
	}
	if size.Width > 32_000_000/size.Height {
		return rich.ImageResource{Problem: rich.ImageTooLarge}
	}
	if _, err := f.Seek(0, 0); err != nil {
		return rich.ImageResource{Problem: rich.ImageUnreadable}
	}
	pixels, _, err := image.Decode(f)
	if err != nil {
		return rich.ImageResource{Problem: rich.ImageInvalid}
	}
	return rich.ImageResource{Bitmap: ui.NewBitmap(pixels), Width: size.Width, Height: size.Height}
}
