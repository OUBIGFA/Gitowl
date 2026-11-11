package main

import (
	"image"
	"image/color"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

type finiteDialogDecoder struct{ index, rewinds atomic.Int32 }

func (d *finiteDialogDecoder) Next() (ui.VideoFrame, error) {
	i := d.index.Add(1) - 1
	if i >= 2 {
		return ui.VideoFrame{}, io.EOF
	}
	p := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	p.Set(0, 0, color.NRGBA{R: uint8(255 * (1 - i)), B: uint8(255 * i), A: 255})
	return ui.VideoFrame{Image: p, Duration: 20 * time.Millisecond}, nil
}
func (d *finiteDialogDecoder) Rewind() error { d.rewinds.Add(1); d.index.Store(0); return nil }

func TestNativeDialogFiniteAnimationDoesNotRestartOnUnrelatedRedraw(t *testing.T) {
	decoder := &finiteDialogDecoder{}
	player, err := ui.NewVideoPlayer(decoder)
	if err != nil {
		t.Fatal(err)
	}
	defer player.Close()
	form := &nativeDialogForm{values: map[string]*nativeDialogValue{"image": {imageMedia: &nativeImageDiffMedia{player: player}}}}
	field := backend.KernelDialogField{ID: "image", Kind: "image", LabelKey: "aux.currentImage", ReadOnly: true}
	tt := ui.NewTester(func(c *ui.Context) { renderNativeDialogField(c, field, form, func() {}) }, 900, 620)
	for i := 0; i < 100 && player.Playing(); i++ {
		tt.Advance(50 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	if player.Playing() {
		t.Fatal("finite original playback did not end")
	}
	for i := 0; i < 10; i++ {
		tt.Advance(time.Second)
	}
	if player.Playing() || decoder.rewinds.Load() != 0 {
		t.Fatal("unrelated UI redraw restarted a finite animation")
	}
}
