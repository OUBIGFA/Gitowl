package backend

import (
	"context"
	"errors"
	"image"
)

// One bounded wire chunk of a retained Chromium decoded frame. RGBA is straight
// sRGB (image.NRGBA), never Go's premultiplied image.RGBA storage.
type KernelMediaChunk struct {
	Contents    []byte `json:"contents"`
	Offset      int    `json:"offset"`
	Total       int    `json:"total"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Duration    int64  `json:"duration"`
	Frames      int    `json:"frames"`
	Repetitions int    `json:"repetitions"`
}

// ReadMediaFrame never expands the line scanner limit or blocks unrelated Git
// replies. Frame chunks use the ordinary multiplexed request IDs and context.
func (k *Kernel) ReadMediaFrame(ctx context.Context, id string, index int) (*image.NRGBA, KernelMediaChunk, error) {
	var first KernelMediaChunk
	var pixels []byte
	for offset := 0; ; {
		result, err := k.commandKind(ctx, struct {
			Name    string `json:"name"`
			MediaID string `json:"mediaID"`
			Frame   int    `json:"frame"`
			Offset  int    `json:"offset"`
		}{"media.frame.read", id, index, offset}, true)
		if err != nil {
			return nil, first, err
		}
		chunk := result.MediaChunk
		if chunk == nil || chunk.Width <= 0 || chunk.Height <= 0 || chunk.Width > 32768 || chunk.Height > 32768 ||
			chunk.Total != chunk.Width*chunk.Height*4 || chunk.Offset != offset || len(chunk.Contents) == 0 || len(chunk.Contents) > 1024*1024 ||
			chunk.Total > 256*1024*1024 || chunk.Frames < 1 || chunk.Duration < 0 || chunk.Repetitions < -1 || offset+len(chunk.Contents) > chunk.Total {
			return nil, first, errors.New("invalid or oversized image frame")
		}
		if offset == 0 {
			first = *chunk
			first.Contents = nil
			pixels = make([]byte, chunk.Total)
		} else if chunk.Total != first.Total || chunk.Width != first.Width || chunk.Height != first.Height || chunk.Duration != first.Duration || chunk.Frames != first.Frames || chunk.Repetitions != first.Repetitions {
			return nil, first, errors.New("image changed during frame transfer")
		}
		copy(pixels[offset:], chunk.Contents)
		offset += len(chunk.Contents)
		if offset == len(pixels) {
			return &image.NRGBA{Pix: pixels, Stride: first.Width * 4, Rect: image.Rect(0, 0, first.Width, first.Height)}, first, nil
		}
	}
}
