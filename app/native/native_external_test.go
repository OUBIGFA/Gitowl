package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

func TestKernelBrowserRequestUsesVisibleHostAndReportsOpenResult(t *testing.T) {
	for _, openError := range []error{nil, errors.New("system browser unavailable")} {
		t.Run(map[bool]string{true: "opened", false: "failed"}[openError == nil], func(t *testing.T) {
			stdout, output := io.Pipe()
			input, stdin := io.Pipe()
			a := newApp(nil, nil)
			// Match the real host's update dispatcher. A reply cancelled during
			// teardown may still enqueue its final (closed-app) error callback.
			updates := make(chan func(), 1)
			a.update = func(fn func()) { updates <- fn }
			defer func() { a.close(); input.Close(); stdin.Close(); stdout.Close(); output.Close() }()
			a.core = backend.NewKernel(stdout, stdin, a.receiveCoreEvent)
			opened := make(chan string, 1)
			a.openExternal = func(url string) error { opened <- url; return openError }
			const url = "https://github.com/login/oauth/authorize?client_id=fixture"
			data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "external.open", "params": backend.KernelExternalRequest{ID: 42, URL: url}})
			if _, err := output.Write(append(data, '\n')); err != nil {
				t.Fatal(err)
			}
			select {
			case actual := <-opened:
				if actual != url {
					t.Fatalf("OAuth URL was changed: %s", actual)
				}
			case <-time.After(time.Second):
				t.Fatal("the visible native host never opened the browser")
			}
			requests := make(chan []byte, 1)
			go func() { data, _ := bufio.NewReader(input).ReadBytes('\n'); requests <- data }()
			var request struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
				Params struct {
					ID     int  `json:"id"`
					Opened bool `json:"opened"`
				} `json:"params"`
			}
			select {
			case data := <-requests:
				if err := json.Unmarshal(data, &request); err != nil || request.Method != "kernel.externalResult" || request.Params.ID != 42 || request.Params.Opened != (openError == nil) {
					t.Fatalf("native browser result was lost: %s / %v", data, err)
				}
			case <-time.After(time.Second):
				t.Fatal("the original login flow did not receive its browser result")
			}
			data, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]bool{"received": true}})
			if _, err := output.Write(append(data, '\n')); err != nil {
				t.Fatal(err)
			}
		})
	}
}
