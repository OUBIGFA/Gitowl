package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

// Values are the original models/diff/image-diff.ts enum, not a native preference.
type nativeImageDiffType int

const (
	nativeImageTwoUp nativeImageDiffType = iota
	nativeImageSwipe
	nativeImageOnionSkin
	nativeImageDifference
)

// Embed in app. All methods except prepareNativeImageDiff run on the window
// thread. The parent supplies original IAppState.imageDiffType and persists
// requests through Dispatcher.changeImageDiffType, never a native settings file.
type nativeImageDiffState struct {
	imageMode                nativeImageDiffType
	imageSwipe, imageOpacity float64
	imageNatural             bool
	imagePrepared            *nativeImageDiffPrepared
	imageError               error
	imageLoading             bool
	imageIdentity            string
	imageEpoch               uint64
	imageCancel              context.CancelFunc
	imageScroll              ui.ScrollState
	imageDifference          *ui.VideoPlayer
	imageDifferenceSize      image.Point
	imageDifferenceEpoch     uint64
	imageDifferenceCancel    context.CancelFunc
	imageDifferenceError     error
}

type nativeImageDiffMedia struct {
	source               ui.ImageSource
	pixels               *image.RGBA // immutable, already oriented by MyGo's decoder
	player               *ui.VideoPlayer
	data                 []byte // originalKernel.Diff owns these immutable bytes
	width, height, bytes int
	makeDecoder          func() (ui.VideoDecoder, error)
	animated             bool
	loops                int // GIF: -1 once, 0 forever, n repetitions after the first play
}
type nativeImageDiffPrepared struct{ previous, current *nativeImageDiffMedia }

func (p *nativeImageDiffPrepared) close() {
	if p == nil {
		return
	}
	for _, m := range []*nativeImageDiffMedia{p.previous, p.current} {
		if m != nil && m.player != nil {
			m.player.Close()
		}
	}
}

// clearNativeImageDiff must be called by clearDiff, repository replacement and
// close. Epoch checks also reject a finished worker whose UI callback was queued
// before cancellation. No Context/Element crosses the worker boundary.
func (s *nativeImageDiffState) clearNativeImageDiff() {
	s.imageEpoch++
	if s.imageCancel != nil {
		s.imageCancel()
		s.imageCancel = nil
	}
	s.clearNativeImageDifference()
	s.imagePrepared.close()
	s.imagePrepared, s.imageError, s.imageLoading = nil, nil, false
	s.imageScroll = ui.ScrollState{}
	s.imageIdentity = ""
}
func (s *nativeImageDiffState) clearNativeImageDifference() {
	s.imageDifferenceEpoch++
	if s.imageDifferenceCancel != nil {
		s.imageDifferenceCancel()
		s.imageDifferenceCancel = nil
	}
	if s.imageDifference != nil {
		s.imageDifference.Close()
	}
	s.imageDifference, s.imageDifferenceSize, s.imageDifferenceError = nil, image.Point{}, nil
}
func (s *nativeImageDiffState) setNativeImageDiffType(mode nativeImageDiffType) {
	if mode < nativeImageTwoUp || mode > nativeImageDifference {
		return
	}
	if s.imageMode != mode {
		s.imageMode, s.imageSwipe, s.imageOpacity = mode, 0, 1
		s.imageScroll = ui.ScrollState{}
		s.clearNativeImageDifference()
	}
}

// loadNativeImageDiff is the parent hook after clearDiff and before the ordinary
// preparePreview worker. It consumes only projected originalKernel.Diff bytes.
func (a *app) loadNativeImageDiff(s *nativeImageDiffState, diff backend.Diff, identity string) {
	s.clearNativeImageDiff()
	s.imageIdentity, s.imageLoading = identity, true
	s.imageSwipe, s.imageOpacity = 0, 1
	epoch, diffSeq, repoSeq := s.imageEpoch, a.diffSeq, a.repoSeq
	ctx, cancel := context.WithCancel(a.ctx)
	s.imageCancel = cancel
	update, core := a.update, a.core
	go func() {
		p, err := prepareNativeImageDiffWithKernel(ctx, diff, core)
		update(func() {
			if a.closed || ctx.Err() != nil || epoch != s.imageEpoch || diffSeq != a.diffSeq || repoSeq != a.repoSeq {
				p.close()
				return
			}
			s.imagePrepared, s.imageError, s.imageLoading = p, err, false
		})
	}()
}

// prepareNativeImageDiff performs decoding once, outside any UI build pass.
// ui.DecodeImage supplies MyGo's EXIF orientation and registered bitmap/SVG
// support; GIF uses its real compositing/disposal decoder and VideoPlayer.
func prepareNativeImageDiff(ctx context.Context, diff backend.Diff) (*nativeImageDiffPrepared, error) {
	return prepareNativeImageDiffWithKernel(ctx, diff, nil)
}

func prepareNativeImageDiffWithKernel(ctx context.Context, diff backend.Diff, kernel *backend.Kernel) (*nativeImageDiffPrepared, error) {
	if diff.Kind != "image" || (diff.Previous == nil && diff.Current == nil) {
		return nil, errors.New("image diff has no image contents")
	}
	p := &nativeImageDiffPrepared{}
	for _, side := range []struct {
		blob *backend.Blob
		dest **nativeImageDiffMedia
		name string
	}{{diff.Previous, &p.previous, "previous"}, {diff.Current, &p.current, "current"}} {
		if side.blob == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			p.close()
			return nil, err
		}
		var m *nativeImageDiffMedia
		var err error
		if side.blob.MediaID != "" {
			m, err = prepareNativeKernelImageMedia(ctx, kernel, side.blob)
		} else {
			m, err = prepareNativeImageDiffMedia(ctx, side.blob)
		}
		if err != nil {
			p.close()
			return nil, fmt.Errorf("%s image: %w", side.name, err)
		}
		*side.dest = m
	}
	if err := ctx.Err(); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

func prepareNativeImageDiffMedia(ctx context.Context, blob *backend.Blob) (*nativeImageDiffMedia, error) {
	if blob.PreviewError != "" {
		return nil, errors.New(blob.PreviewError)
	}
	if err := nativeImageCheckFormat(blob); err != nil {
		return nil, err
	}
	m := &nativeImageDiffMedia{data: blob.Contents, bytes: blob.Bytes, loops: -1}
	if bytes.HasPrefix(blob.Contents, []byte("GIF87a")) || bytes.HasPrefix(blob.Contents, []byte("GIF89a")) {
		// Go validates metadata; don't search arbitrary compressed data for a loop
		// extension. MyGo's GIF decoder deliberately leaves loop policy to its caller.
		g, err := gif.DecodeAll(bytes.NewReader(blob.Contents))
		if err != nil {
			return nil, err
		}
		m.animated, m.loops = len(g.Image) > 1, g.LoopCount
		m.makeDecoder = func() (ui.VideoDecoder, error) { return ui.NewGIFDecoder(m.data) }
		dec, err := ui.NewGIFDecoder(blob.Contents)
		if err != nil {
			return nil, err
		}
		first, err := dec.Next()
		if err != nil {
			return nil, err
		}
		m.pixels = nativeImageCopy(first.Image)
		m.width, m.height = m.pixels.Rect.Dx(), m.pixels.Rect.Dy()
		if m.animated {
			if err := dec.Rewind(); err != nil {
				return nil, err
			}
			m.player, err = ui.NewVideoPlayer(&nativeImageLoopDecoder{decoder: dec, loops: m.loops})
			if err != nil {
				return nil, err
			}
		}
	} else {
		src, err := ui.DecodeImage(blob.Contents)
		if err != nil {
			return nil, err
		}
		w, h := ui.ImageSize(src)
		if w <= 0 || h <= 0 {
			return nil, errors.New("image has no intrinsic size")
		}
		m.width, m.height = int(math.Ceil(float64(w))), int(math.Ceil(float64(h)))
		// A native offscreen Image raster gives access to oriented pixels, including
		// supported SVG, without copying MyGo's private EXIF/SVG implementation or
		// using an app Canvas. This source is worker-private, not drawn concurrently.
		m.pixels = nativeImageRaster(src, m.width, m.height, float32(m.width), float32(m.height))
	}
	if err := ctx.Err(); err != nil {
		if m.player != nil {
			m.player.Close()
		}
		return nil, err
	}
	m.source = ui.NewBitmap(m.pixels)
	return m, nil
}

// MyGo intentionally omits these SVG constructs. Failing visibly is preferable
// to an apparently valid diff with text, filters or embedded pixels missing.
func nativeImageCheckFormat(blob *backend.Blob) error {
	// DecodeBitmap's PNG decoder has no APNG timeline. The PNG acTL chunk
	// declares animation; don't present the default image as animated support.
	if bytes.HasPrefix(blob.Contents, []byte("\x89PNG\r\n\x1a\n")) {
		for rest := blob.Contents[8:]; len(rest) >= 12; {
			size := uint64(binary.BigEndian.Uint32(rest[:4])) + 12
			if size > uint64(len(rest)) {
				break
			} // normal decoder reports truncation
			if string(rest[4:8]) == "acTL" {
				return errors.New("MyGo 0.8.1 has no animated PNG decoder")
			}
			rest = rest[int(size):]
		}
	}
	if blob.MediaType == "image/svg+xml" || bytes.HasPrefix(bytes.TrimSpace(blob.Contents), []byte("<")) {
		decoder := xml.NewDecoder(bytes.NewReader(blob.Contents))
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if tag, ok := token.(xml.StartElement); ok {
				switch tag.Name.Local {
				case "text", "image", "pattern", "marker", "filter", "foreignObject", "animate", "animateTransform", "animateMotion", "set":
					return fmt.Errorf("MyGo 0.8.1 cannot faithfully render SVG <%s>", tag.Name.Local)
				}
			}
		}
	}
	return nil
}

func nativeImageCopy(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Rect, src, b.Min, draw.Src)
	return dst
}

// nativeImageFit ports image-diffs/sizing.ts: each image shrinks separately and
// never grows; the comparison canvas is the union of the two fitted extents.
func nativeImageFit(w, h, availableW, availableH float32) (float32, float32) {
	if w <= 0 || h <= 0 || availableW <= 0 || availableH <= 0 {
		return 0, 0
	}
	ratio := max(float32(1), w/availableW, h/availableH)
	return w / ratio, h / ratio
}
func (p *nativeImageDiffPrepared) size(w, h float32, natural bool) (float32, float32) {
	var width, height float32
	for _, m := range []*nativeImageDiffMedia{p.previous, p.current} {
		if m == nil {
			continue
		}
		mw, mh := float32(m.width), float32(m.height)
		if !natural {
			mw, mh = nativeImageFit(mw, mh, w, h)
		}
		width, height = max(width, mw), max(height, mh)
	}
	return width, height
}

func nativeImageRaster(src ui.ImageSource, w, h int, fitW, fitH float32) *image.RGBA {
	return ui.Render(func(c *ui.Context) {
		theme := *c.Theme()
		theme.Background = ui.Transparent
		c.SetTheme(&theme)
		ui.Box(c).Fill().Background(ui.Transparent).Center().Children(func() {
			ui.Image(c, src).Size(fitW, fitH).Fit(ui.ScaleDown).Shrink(0)
		})
	}, w, h, 1)
}

// W3C Compositing and Blending Level 1 (10.1.11 + source-over). The original
// DifferenceBlend uses mix-blend-mode:difference in a transform-created isolated
// stacking context. Blend *straight* sRGB channels, then source-over alpha, not
// abs(premultiplied channels), an XOR, or subtraction against the theme/checker.
// Different image sizes are centered before this operation. Returned RGBA is
// premultiplied so native Image composites transparency onto the real theme.
func nativeImageDifferencePixels(ctx context.Context, previous, current *image.RGBA) (*image.RGBA, error) {
	if previous.Bounds() != current.Bounds() {
		return nil, errors.New("difference layers must share the fitted canvas")
	}
	b := previous.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := b.Min.X; x < b.Max.X; x++ {
			pi, ci, oi := previous.PixOffset(x, y), current.PixOffset(x, y), out.PixOffset(x, y)
			ab, as := float64(previous.Pix[pi+3])/255, float64(current.Pix[ci+3])/255
			for k := 0; k < 3; k++ {
				pb, ps := float64(previous.Pix[pi+k])/255, float64(current.Pix[ci+k])/255
				cb, cs := float64(0), float64(0)
				if ab > 0 {
					cb = pb / ab
				}
				if as > 0 {
					cs = ps / as
				}
				co := ps*(1-ab) + pb*(1-as) + math.Abs(cb-cs)*ab*as
				out.Pix[oi+k] = uint8(math.Round(min(1, max(0, co)) * 255))
			}
			out.Pix[oi+3] = uint8(math.Round((as + ab - as*ab) * 255))
		}
	}
	return out, nil
}

// Preserve the GIF's actual loop count. SetLoop(true) would silently make an
// intentionally one-shot GIF repeat forever; MyGo's default is one playback.
type nativeImageLoopDecoder struct {
	decoder         ui.VideoDecoder
	loops, repeated int
}

func (d *nativeImageLoopDecoder) Next() (ui.VideoFrame, error) {
	f, err := d.decoder.Next()
	if err == io.EOF && (d.loops == 0 || d.repeated < d.loops) {
		d.repeated++
		if err = d.decoder.Rewind(); err == nil {
			f, err = d.decoder.Next()
		}
	}
	return f, err
}
func (d *nativeImageLoopDecoder) Rewind() error { d.repeated = 0; return d.decoder.Rewind() }
func (d *nativeImageLoopDecoder) Close() error {
	if closer, ok := d.decoder.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type nativeImageDifferenceTrack struct {
	decoder   ui.VideoDecoder
	pixels    *image.RGBA
	remaining time.Duration
	done      bool
}

// Difference's decoder combines both timelines at the next frame boundary,
// instead of pairing frame numbers (which breaks different delays/counts).
// VideoPlayer owns the background decode loop and publishes via native Update.
type nativeImageDifferenceDecoder struct {
	ctx          context.Context
	tracks       [2]nativeImageDifferenceTrack
	size         image.Point
	initialized  bool
	lastDuration time.Duration
}

func newNativeImageDifferenceDecoder(ctx context.Context, p *nativeImageDiffPrepared, size image.Point) (*nativeImageDifferenceDecoder, error) {
	d := &nativeImageDifferenceDecoder{ctx: ctx, size: size}
	for i, m := range []*nativeImageDiffMedia{p.previous, p.current} {
		d.tracks[i].pixels = m.pixels
		d.tracks[i].done = !m.animated
		if m.animated {
			decoder, err := m.makeDecoder()
			if err != nil {
				return nil, err
			}
			d.tracks[i].decoder = &nativeImageLoopDecoder{decoder: decoder, loops: m.loops}
		}
	}
	return d, nil
}
func (d *nativeImageDifferenceDecoder) Next() (ui.VideoFrame, error) {
	if err := d.ctx.Err(); err != nil {
		return ui.VideoFrame{}, err
	}
	for i := range d.tracks {
		t := &d.tracks[i]
		if t.done {
			continue
		}
		if d.initialized {
			t.remaining -= d.lastDuration
		}
		if !d.initialized || t.remaining <= 0 {
			f, err := t.decoder.Next()
			if err == io.EOF {
				t.done = true
				continue
			}
			if err != nil {
				return ui.VideoFrame{}, err
			}
			t.pixels = nativeImageCopy(f.Image)
			t.remaining = f.Duration
		}
	}
	if d.initialized && d.tracks[0].done && d.tracks[1].done {
		return ui.VideoFrame{}, io.EOF
	}
	duration := time.Duration(math.MaxInt64)
	for _, t := range d.tracks {
		if !t.done {
			duration = min(duration, t.remaining)
		}
	}
	if duration == time.Duration(math.MaxInt64) {
		duration = time.Second
	}
	layers := [2]*image.RGBA{}
	for i, t := range d.tracks {
		w, h := nativeImageFit(float32(t.pixels.Rect.Dx()), float32(t.pixels.Rect.Dy()), float32(d.size.X), float32(d.size.Y))
		layers[i] = nativeImageRaster(ui.NewBitmap(t.pixels), d.size.X, d.size.Y, w, h)
	}
	pixels, err := nativeImageDifferencePixels(d.ctx, layers[0], layers[1])
	d.initialized, d.lastDuration = true, duration
	return ui.VideoFrame{Image: pixels, Duration: duration}, err
}
func (d *nativeImageDifferenceDecoder) Close() error {
	for _, track := range d.tracks {
		if closer, ok := track.decoder.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	return nil
}

func (d *nativeImageDifferenceDecoder) Rewind() error {
	d.initialized = false
	for i := range d.tracks {
		t := &d.tracks[i]
		if t.decoder != nil {
			if err := t.decoder.Rewind(); err != nil {
				return err
			}
			t.done = false
		}
	}
	return nil
}

func (a *app) requestNativeImageDifference(s *nativeImageDiffState, size image.Point) {
	if size.X <= 0 || size.Y <= 0 || size == s.imageDifferenceSize {
		return
	}
	s.clearNativeImageDifference()
	s.imageDifferenceSize = size
	epoch, p := s.imageDifferenceEpoch, s.imagePrepared
	ctx, cancel := context.WithCancel(a.ctx)
	s.imageDifferenceCancel = cancel
	diffSeq, repoSeq, update := a.diffSeq, a.repoSeq, a.update
	go func() {
		d, err := newNativeImageDifferenceDecoder(ctx, p, size)
		var player *ui.VideoPlayer
		if err == nil {
			player, err = ui.NewVideoPlayer(d)
		}
		update(func() {
			if a.closed || ctx.Err() != nil || epoch != s.imageDifferenceEpoch || diffSeq != a.diffSeq || repoSeq != a.repoSeq {
				if player != nil {
					player.Close()
				}
				return
			}
			s.imageDifference, s.imageDifferenceError = player, err
		})
	}()
}
