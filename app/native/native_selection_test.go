package main

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

type selectionRequest struct {
	ID     int `json:"id"`
	Params struct {
		Name       string                            `json:"name"`
		Repository backend.KernelRepositoryReference `json:"repository"`
	} `json:"params"`
}

type selectionTransport struct {
	requests <-chan selectionRequest
	updates  <-chan func()
	output   io.Writer
}

func observeRepositorySelection(t *testing.T, a *app) selectionTransport {
	t.Helper()
	stdout, output := io.Pipe()
	input, stdin := io.Pipe()
	a.core = backend.NewKernel(stdout, stdin, nil)
	updates := make(chan func(), 16)
	a.update = func(update func()) { updates <- update }
	requests := make(chan selectionRequest, 16)
	go func() {
		decoder := json.NewDecoder(input)
		for {
			var request selectionRequest
			if decoder.Decode(&request) != nil {
				return
			}
			requests <- request
		}
	}()
	t.Cleanup(func() {
		a.close()
		input.Close()
		stdin.Close()
		stdout.Close()
		output.Close()
	})
	return selectionTransport{requests, updates, output}
}

func (transport selectionTransport) next(t *testing.T, path string) selectionRequest {
	t.Helper()
	select {
	case request := <-transport.requests:
		if request.Params.Name != "repository.select" || request.Params.Repository.Path == "" || (path != "" && request.Params.Repository.Path != path) {
			t.Fatalf("unexpected original selection request: %+v", request)
		}
		return request
	case <-time.After(time.Second):
		t.Fatalf("selection of %q waited for an earlier background request", path)
		return selectionRequest{}
	}
}

func (transport selectionTransport) reply(t *testing.T, request selectionRequest, snapshot backend.KernelSnapshot) {
	t.Helper()
	response := map[string]any{
		"jsonrpc": "2.0", "id": request.ID,
		"result": backend.KernelResult{Snapshot: snapshot},
	}
	if err := json.NewEncoder(transport.output).Encode(response); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-transport.updates:
		update()
	case <-time.After(time.Second):
		t.Fatal("selection response did not reach the native view")
	}
}

func TestNativeProjectSelectionDoesNotWaitForPreviousRepositoryMetadata(t *testing.T) {
	a := newCoreApp(nil, nil)
	transport := observeRepositorySelection(t, a)
	first := backend.KernelRepositoryReference{ID: 1, Path: "C:/fixture/first"}
	second := backend.KernelRepositoryReference{ID: 2, Path: "C:/fixture/second"}
	a.selectCoreRepository(first)
	firstRequest := transport.next(t, first.Path)
	a.selectCoreRepository(second)
	secondRequest := transport.next(t, second.Path)
	snapshot := backend.KernelSnapshot{
		Protocol: 1, Core: "desktop/desktop", Revision: 2,
		Accounts: []backend.KernelAccount{}, Repositories: []backend.KernelRepository{}, Popups: []backend.KernelPopup{},
		Selected: &backend.KernelSelected{
			Repository: backend.KernelRepository{KernelRepositoryReference: second, Name: "Second", Kind: "repository"},
			Section:    "changes", Files: []backend.KernelFile{}, History: []backend.Commit{},
		},
	}
	transport.reply(t, secondRequest, snapshot)
	if a.repo == nil || a.repo.Path != second.Path || a.corePending != 1 {
		t.Fatal("the second selection was not displayed while the first was pending")
	}
	snapshot.Revision = 3
	transport.reply(t, firstRequest, snapshot)
	if a.repo.Path != second.Path || a.corePending != 0 {
		t.Fatal("an older completion replaced the current original selection")
	}
}
