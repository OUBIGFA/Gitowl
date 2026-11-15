package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func kernelPipes(t *testing.T, event func(KernelEvent)) (*Kernel, peer) {
	t.Helper()
	stdout, output := io.Pipe()
	input, stdin := io.Pipe()
	kernel := NewKernel(stdout, stdin, event)
	t.Cleanup(func() {
		kernel.client.Close() // fixture pipes only; no Git action/process exists here
		input.Close()
		output.Close()
	})
	return kernel, peer{bufio.NewReader(input), output}
}

// A renderer can publish its last state after shutdown is acknowledged. The
// native parent must keep draining stdout until EOF, not break that final write.
func TestKernelShutdownDrainsNotificationsAfterAcknowledgment(t *testing.T) {
	events := make(chan KernelEvent, 1)
	k, p := kernelPipes(t, func(event KernelEvent) { events <- event })
	closed := make(chan error, 1)
	go func() { closed <- k.Close() }()
	request := p.request(t)
	if request.Method != "kernel.shutdown" {
		t.Fatalf("unexpected shutdown request: %+v", request)
	}
	p.result(t, request.ID, map[string]bool{"quitting": true})
	if _, err := p.input.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("parent did not close stdin after acknowledgment: %v", err)
	}
	notification(t, p, "kernel.state", emptyKernelSnapshot(1))
	if event := receive(t, events); event.Snapshot == nil || event.Snapshot.Revision != 1 {
		t.Fatalf("last original state was lost: %+v", event)
	}
	if err := p.output.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, closed); err != nil {
		t.Fatalf("graceful shutdown failed: %v", err)
	}
}

func emptyKernelSnapshot(revision int64) KernelSnapshot {
	return KernelSnapshot{
		Protocol: 1, Core: "desktop/desktop", Revision: revision,
		Repositories: []KernelRepository{}, Accounts: []KernelAccount{}, Popups: []KernelPopup{},
	}
}

func kernelCall(fn func() (KernelResult, error)) <-chan struct {
	result KernelResult
	err    error
} {
	ch := make(chan struct {
		result KernelResult
		err    error
	}, 1)
	go func() {
		result, err := fn()
		ch <- struct {
			result KernelResult
			err    error
		}{result, err}
	}()
	return ch
}

func notification(t *testing.T, p peer, method string, params any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.output.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestKernelNotificationsPreservePendingResultIDs(t *testing.T) {
	events := make(chan KernelEvent, 4)
	k, p := kernelPipes(t, func(event KernelEvent) { events <- event })
	call := kernelCall(func() (KernelResult, error) { return k.State(context.Background()) })
	request := p.request(t)
	if request.Method != "kernel.command" || string(request.Params) != `{"name":"state"}` {
		t.Fatalf("wrong core request: %+v", request)
	}
	if _, err := p.output.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	notification(t, p, "kernel.state", emptyKernelSnapshot(1))
	if event := receive(t, events); event.Snapshot == nil || event.Snapshot.Core != "desktop/desktop" {
		t.Fatalf("state notification: %+v", event)
	}
	notification(t, p, "window.action", map[string]string{"action": "show"})
	if event := receive(t, events); event.WindowAction != "show" {
		t.Fatalf("window notification: %+v", event)
	}
	if k.InFlight() != 1 {
		t.Fatal("a notification consumed the response waiter")
	}
	p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(2)})
	if result := receive(t, call); result.err != nil || result.result.Snapshot.Revision != 2 {
		t.Fatalf("result: %+v", result)
	}
	if k.InFlight() != 0 {
		t.Fatal("settled action still counted as pending")
	}
}

func TestKernelCommandUsesWorktreeReferenceAndOriginalCommitContext(t *testing.T) {
	k, p := kernelPipes(t, nil)
	ref := KernelRepositoryReference{ID: 7, Path: "C:/fixture/linked-worktree"}
	body := "reviewed description"
	call := kernelCall(func() (KernelResult, error) {
		return k.CommitIncludedChanges(context.Background(), ref, KernelCommitContext{Summary: "save version", Description: &body})
	})
	request := p.request(t)
	var params struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Context    KernelCommitContext       `json:"context"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if request.Method != "kernel.command" || params.Name != "repository.commit" || params.Repository != ref || params.Context.Summary != "save version" || params.Context.Description == nil || *params.Context.Description != body {
		t.Fatalf("original commit contract lost: %s", request.Params)
	}
	// The bridge sends no independent paths/staging/auth fields and treats only
	// the original Dispatcher outcome as a confirmation.
	var all map[string]json.RawMessage
	json.Unmarshal(request.Params, &all)
	if len(all) != 3 {
		t.Fatalf("unexpected business fields: %s", request.Params)
	}
	confirmed := true
	p.result(t, request.ID, KernelResult{Snapshot: emptyKernelSnapshot(1), Committed: &confirmed})
	if result := receive(t, call); result.err != nil || result.result.Committed == nil || !*result.result.Committed {
		t.Fatalf("commit confirmation: %+v", result)
	}
}

func TestKernelCancellationDoesNotFabricateBusinessCancellation(t *testing.T) {
	k, p := kernelPipes(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	call := kernelCall(func() (KernelResult, error) { return k.State(ctx) })
	original := p.request(t)
	cancel()
	if result := receive(t, call); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("waiter cancellation: %+v", result)
	}
	previous := closeSettleTimeout
	closeSettleTimeout = 20 * time.Millisecond
	defer func() { closeSettleTimeout = previous }()
	if k.InFlight() != 1 || !errors.Is(k.Close(), ErrBusy) {
		t.Fatal("unresolved original action no longer guarded shutdown")
	}
	nextCall := kernelCall(func() (KernelResult, error) { return k.State(context.Background()) })
	next := p.request(t)
	if next.Method != "kernel.command" || next.ID <= original.ID {
		t.Fatalf("fabricated cancellation notification: %+v", next)
	}
	p.result(t, original.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
	p.result(t, next.ID, KernelResult{Snapshot: emptyKernelSnapshot(2)})
	if result := receive(t, nextCall); result.err != nil {
		t.Fatal(result.err)
	}
	if k.InFlight() != 0 {
		t.Fatal("late original outcome was not settled")
	}
}

func TestKernelCloseWaitsForAdmittedWorkBeforeShutdown(t *testing.T) {
	k, p := kernelPipes(t, nil)
	call := kernelCall(func() (KernelResult, error) { return k.State(context.Background()) })
	original := p.request(t)
	closed := make(chan error, 1)
	go func() { closed <- k.Close() }()
	time.Sleep(50 * time.Millisecond)
	select {
	case err := <-closed:
		t.Fatalf("Close gave up on a loaded kernel instead of waiting: %v", err)
	default:
	}
	p.result(t, original.ID, KernelResult{Snapshot: emptyKernelSnapshot(1)})
	if result := receive(t, call); result.err != nil {
		t.Fatal(result.err)
	}
	request := p.request(t)
	if request.Method != "kernel.shutdown" {
		t.Fatalf("shutdown was not requested after work settled: %+v", request)
	}
	p.result(t, request.ID, map[string]bool{"quitting": true})
	if _, err := p.input.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("parent stdin did not close: %v", err)
	}
	p.output.Close()
	if err := receive(t, closed); err != nil {
		t.Fatal(err)
	}
}

func TestKernelShutdownIsAcknowledgedAndStopsNewAdmissions(t *testing.T) {
	k, p := kernelPipes(t, nil)
	closed := make(chan error, 1)
	go func() { closed <- k.Close() }()
	request := p.request(t)
	if request.Method != "kernel.shutdown" {
		t.Fatalf("wrong shutdown: %+v", request)
	}
	if _, err := k.State(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("new action admitted during shutdown: %v", err)
	}
	second := make(chan error, 1)
	go func() { second <- k.Close() }()
	select {
	case <-second:
		t.Fatal("concurrent Close reported success before acknowledgment")
	default:
	}
	p.result(t, request.ID, map[string]bool{"quitting": true})
	if _, err := p.input.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("parent stdin did not close: %v", err)
	}
	p.output.Close()
	if err := receive(t, closed); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, second); err != nil {
		t.Fatal(err)
	}
}

func TestKernelRejectsReducedServiceSnapshot(t *testing.T) {
	k, p := kernelPipes(t, nil)
	call := kernelCall(func() (KernelResult, error) { return k.State(context.Background()) })
	request := p.request(t)
	p.result(t, request.ID, map[string]any{"snapshot": map[string]any{"protocol": 1, "core": "native-service", "revision": 1}})
	result := receive(t, call)
	var protocol *ProtocolError
	if !errors.As(result.err, &protocol) {
		t.Fatalf("reduced service looked connected: %+v", result)
	}
	if k.Err() == nil {
		t.Fatal("invalid core handshake left the client usable")
	}
}

func TestKernelConfigKeepsOwnedPathsAndElectronBootstrap(t *testing.T) {
	data := t.TempDir()
	config, err := ResolveKernelConfig("fixture/resources", data, "", "")
	if err != nil {
		t.Fatal(err)
	}
	expectedData, err := filepath.Abs(data)
	if err != nil {
		t.Fatal(err)
	}
	if config.UserData != expectedData || !filepath.IsAbs(config.Entry) || !strings.HasSuffix(filepath.ToSlash(config.Entry), "desktop-kernel/app/main.js") {
		t.Fatalf("wrong default paths: %+v", config)
	}
	if _, err := ResolveKernelConfig("resources", "", "entry.js", "electron"); err == nil {
		t.Fatal("missing owned profile accepted")
	}
	t.Setenv("ELECTRON_RUN_AS_NODE", "1")
	t.Setenv("NODE_OPTIONS", "--require=unrelated-script")
	t.Setenv("GITOWL_USER_DATA", "unrelated-profile")
	t.Setenv("GITOWL_CREDENTIAL_NAMESPACE", "upstream-namespace")
	for _, item := range kernelEnvironment(data) {
		key, value, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "ELECTRON_RUN_AS_NODE", "NODE_OPTIONS", "GITOWL_CREDENTIAL_NAMESPACE":
			t.Fatalf("inherited altered kernel realm: %s", key)
		case "GITOWL_USER_DATA":
			if value != data {
				t.Fatal("profile redirected by parent environment")
			}
		case "ELECTRON_NO_ATTACH_CONSOLE":
			if value != "1" {
				t.Fatal("owned Windows pipe was redirected")
			}
		}
	}
}
