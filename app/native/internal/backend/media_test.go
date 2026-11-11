package backend

import (
	"context"
	"encoding/json"
	"testing"
)

func TestKernelMediaChunksDoNotBreakConcurrentOriginalResults(t *testing.T) {
	k, p := kernelPipes(t, nil)
	// An RGBA frame this size would exceed the 64 MiB scanner after base64.
	const width, height = 4096, 4096
	done := make(chan error, 1)
	go func() {
		pixels, metadata, err := k.ReadMediaFrame(context.Background(), "media-fixture", 0)
		if err == nil && (len(pixels.Pix) != width*height*4 || metadata.Width != width || pixels.Pix[len(pixels.Pix)-1] != 128) {
			t.Error("assembled original frame changed")
		}
		done <- err
	}()
	state := kernelCall(func() (KernelResult, error) { return k.State(context.Background()) })
	read, states := 0, 0
	for read < width*height*4 || states == 0 {
		request := p.request(t)
		var command struct {
			Name   string
			Offset int
		}
		if err := json.Unmarshal(request.Params, &command); err != nil {
			t.Fatal(err)
		}
		if command.Name == "state" {
			p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(2)})
			states++
			continue
		}
		if command.Name != "media.frame.read" || command.Offset != read {
			t.Fatalf("unexpected media command %s", request.Params)
		}
		chunk := make([]byte, 1024*1024)
		for i := 3; i < len(chunk); i += 4 {
			chunk[i] = 128
		}
		p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(3), MediaChunk: &KernelMediaChunk{Contents: chunk, Offset: read, Total: width * height * 4, Width: width, Height: height, Duration: 100000, Frames: 1}})
		read += len(chunk)
	}
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	if result := receive(t, state); result.err != nil {
		t.Fatal(result.err)
	}
	if k.Err() != nil {
		t.Fatalf("media disconnected original kernel: %v", k.Err())
	}
}
