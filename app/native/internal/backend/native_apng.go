package backend

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/kettek/apng"
	"image"
	"image/draw"
)

func animatedPNG(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	for rest := data[8:]; len(rest) >= 12; {
		size := uint64(binary.BigEndian.Uint32(rest)) + 12
		if size > uint64(len(rest)) {
			return false
		}
		if string(rest[4:8]) == "acTL" {
			return true
		}
		rest = rest[int(size):]
	}
	return false
}

// kettek/apng decodes and validates PNG records. Compositing follows PNG third
// edition's SOURCE/OVER and NONE/BACKGROUND/PREVIOUS operations using Go image/draw.
func decodeAPNGFrame(data []byte, index int) (*image.NRGBA, KernelMediaChunk, error) {
	if len(data) < 24 {
		return nil, KernelMediaChunk{}, errors.New("truncated PNG")
	}
	w, h := int(binary.BigEndian.Uint32(data[16:20])), int(binary.BigEndian.Uint32(data[20:24]))
	if w <= 0 || h <= 0 || w > 32768 || h > 32768 {
		return nil, KernelMediaChunk{}, errors.New("APNG dimensions exceed preview limits")
	}
	count := uint64(0)
	for rest := data[8:]; len(rest) >= 12; {
		size := uint64(binary.BigEndian.Uint32(rest)) + 12
		if size > uint64(len(rest)) {
			break
		}
		if string(rest[4:8]) == "acTL" && size >= 20 {
			count = uint64(binary.BigEndian.Uint32(rest[8:12]))
			break
		}
		rest = rest[int(size):]
	}
	if count == 0 || count > 10000 || uint64(w)*uint64(h)*4*(count+3) > 256<<20 {
		return nil, KernelMediaChunk{}, errors.New("APNG animation exceeds preview memory limit")
	}
	animation, err := apng.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, KernelMediaChunk{}, err
	}
	frames := animation.Frames
	if len(frames) > 0 && frames[0].IsDefault {
		frames = frames[1:]
	}
	if index < 0 || index >= len(frames) {
		return nil, KernelMediaChunk{}, errors.New("APNG frame is outside the animation")
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	var previous *image.NRGBA
	for i, frame := range frames {
		bounds := frame.Image.Bounds()
		rect := image.Rect(frame.XOffset, frame.YOffset, frame.XOffset+bounds.Dx(), frame.YOffset+bounds.Dy())
		if !rect.In(canvas.Rect) {
			return nil, KernelMediaChunk{}, errors.New("APNG frame leaves its canvas")
		}
		if frame.DisposeOp == apng.DISPOSE_OP_PREVIOUS {
			previous = image.NewNRGBA(canvas.Rect)
			copy(previous.Pix, canvas.Pix)
		}
		op := draw.Src
		if frame.BlendOp == apng.BLEND_OP_OVER {
			op = draw.Over
		} else if frame.BlendOp != apng.BLEND_OP_SOURCE {
			return nil, KernelMediaChunk{}, errors.New("invalid APNG blend operation")
		}
		draw.Draw(canvas, rect, frame.Image, bounds.Min, op)
		if i == index {
			duration := int64(frame.GetDelay() * 1000000)
			repetitions := int(animation.LoopCount) - 1
			return canvas, KernelMediaChunk{Total: len(canvas.Pix), Width: w, Height: h, Duration: max(1, duration), Frames: len(frames), Repetitions: repetitions}, nil
		}
		switch frame.DisposeOp {
		case apng.DISPOSE_OP_NONE:
		case apng.DISPOSE_OP_BACKGROUND:
			draw.Draw(canvas, rect, image.Transparent, image.Point{}, draw.Src)
		case apng.DISPOSE_OP_PREVIOUS:
			copy(canvas.Pix, previous.Pix)
		default:
			return nil, KernelMediaChunk{}, errors.New("invalid APNG disposal operation")
		}
	}
	return nil, KernelMediaChunk{}, errors.New("APNG frame is unavailable")
}
