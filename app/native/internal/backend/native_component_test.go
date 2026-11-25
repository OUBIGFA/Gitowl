package backend

import (
	"context"
	"encoding/json"
	copilot "github.com/github/copilot-sdk/go"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type componentTransport func(*http.Request) (*http.Response, error)

func (fn componentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNativeCopilotComponentAndSDK(t *testing.T) {
	archive := os.Getenv("GITOWL_COPILOT_ARCHIVE")
	manifest := os.Getenv("GITOWL_COPILOT_MANIFEST")
	if archive == "" || manifest == "" {
		t.Skip("set the pinned official component archive and manifest to verify the real SDK")
	}
	root := t.TempDir()
	requests := 0
	c := nativeComponent{manifestPath: manifest, cache: filepath.Join(root, "cache"), recycle: func(path string) error { return os.Rename(path, filepath.Join(root, "retired")) }, client: &http.Client{Transport: componentTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		body, err := os.Open(archive)
		if err != nil {
			return nil, err
		}
		info, err := body.Stat()
		if err != nil {
			body.Close()
			return nil, err
		}
		return &http.Response{StatusCode: 200, Body: body, ContentLength: info.Size(), Header: make(http.Header)}, nil
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry, err := c.prepare(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("component was not acquired exactly once")
	}
	client := copilot.NewClient(&copilot.ClientOptions{Connection: copilot.StdioConnection{Path: entry}, BaseDirectory: filepath.Join(root, "sdk-home"), UseLoggedInUser: boolPointer(false), Mode: copilot.ModeEmpty})
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer client.Stop()
	pong, err := client.Ping(ctx, "native-go-runtime")
	if err != nil || pong.Message != "pong: native-go-runtime" {
		t.Fatalf("SDK handshake %+v %v", pong, err)
	}
	c.verified = ""
	c.client = &http.Client{Transport: componentTransport(func(*http.Request) (*http.Response, error) {
		t.Error("verified cache attempted another download")
		return nil, io.EOF
	})}
	if again, err := c.prepare(ctx); err != nil || again != entry {
		t.Fatalf("offline cache %q %v", again, err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var descriptor componentManifest
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Files) == 0 {
		t.Fatal("missing runtime verification inventory")
	}
}
