package main

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

// Adapts retained Chromium full/composited RGBA frames to MyGo's native player.
// The original image's code never runs in the UI document. Requests are made on
// VideoPlayer's decoder goroutine and are canceled with this presentation owner.
type kernelImageDecoder struct {
	ctx      context.Context
	cancel   context.CancelFunc
	kernel   *backend.Kernel
	id       string
	index    int
	metadata backend.KernelMediaChunk
}

func newKernelImageDecoder(ctx context.Context, kernel *backend.Kernel, id string) *kernelImageDecoder {
	ctx, cancel := context.WithCancel(ctx)
	return &kernelImageDecoder{ctx: ctx, cancel: cancel, kernel: kernel, id: id}
}
func (d *kernelImageDecoder) Next() (ui.VideoFrame, error) {
	if d.metadata.Frames != 0 && d.index >= d.metadata.Frames {
		return ui.VideoFrame{}, io.EOF
	}
	pixels, metadata, err := d.kernel.ReadMediaFrame(d.ctx, d.id, d.index)
	if err != nil {
		return ui.VideoFrame{}, err
	}
	d.metadata = metadata
	d.index++
	duration := time.Duration(metadata.Duration) * time.Microsecond
	if duration <= 0 {
		duration = 100 * time.Millisecond
	}
	return ui.VideoFrame{Image: pixels, Duration: duration}, nil
}
func (d *kernelImageDecoder) Rewind() error { d.index = 0; return d.ctx.Err() }
func (d *kernelImageDecoder) Close() error  { d.cancel(); return nil }

func prepareNativeKernelImageMedia(ctx context.Context, kernel *backend.Kernel, blob *backend.Blob) (*nativeImageDiffMedia, error) {
	if kernel == nil {
		return nil, errors.New("original image decoder is not connected")
	}
	decoder := newKernelImageDecoder(ctx, kernel, blob.MediaID)
	defer decoder.Close()
	first, err := decoder.Next()
	if err != nil {
		return nil, err
	}
	m := &nativeImageDiffMedia{bytes: blob.Bytes, width: decoder.metadata.Width, height: decoder.metadata.Height, pixels: nativeImageCopy(first.Image), animated: decoder.metadata.Frames > 1, loops: decoder.metadata.Repetitions}
	if m.loops == -1 {
		m.loops = 0
	} else if m.loops == 0 {
		m.loops = -1
	}
	m.makeDecoder = func() (ui.VideoDecoder, error) { return newKernelImageDecoder(ctx, kernel, blob.MediaID), nil }
	m.source = ui.NewBitmap(m.pixels)
	if m.animated {
		dec, _ := m.makeDecoder()
		m.player, err = ui.NewVideoPlayer(&nativeImageLoopDecoder{decoder: dec, loops: m.loops})
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}
