package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// Tests use the production client and real pipes, not a second protocol client.
type peer struct {
	input  *bufio.Reader
	output *io.PipeWriter
}

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func pipes(t *testing.T) (*Client, peer) {
	t.Helper()
	stdout, output := io.Pipe()
	input, stdin := io.Pipe()
	client := NewClient(stdout, stdin)
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
		input.Close()
		output.Close()
	})
	return client, peer{bufio.NewReader(input), output}
}

// The guard detects hangs only; assertions never depend on elapsed time.
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Minute):
		t.Fatal("blocked waiting for pipe event")
		var zero T
		return zero
	}
}

func (p peer) request(t *testing.T) wireRequest {
	t.Helper()
	ch := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		data, err := p.input.ReadBytes('\n')
		ch <- struct {
			data []byte
			err  error
		}{data, err}
	}()
	r := receive(t, ch)
	if r.err != nil {
		t.Fatal(r.err)
	}
	var request wireRequest
	if err := json.Unmarshal(r.data, &request); err != nil {
		t.Fatal(err)
	}
	if request.JSONRPC != "2.0" || request.ID == 0 && request.Method != "$/cancelRequest" || r.data[len(r.data)-1] != '\n' {
		t.Fatalf("invalid request: %s", r.data)
	}
	return request
}

func (p peer) result(t *testing.T, id uint64, value any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": value})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.output.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func callString(c *Client, ctx context.Context, method string) <-chan struct {
	value string
	err   error
} {
	ch := make(chan struct {
		value string
		err   error
	}, 1)
	go func() {
		var value string
		err := c.Call(ctx, method, struct{}{}, &value)
		ch <- struct {
			value string
			err   error
		}{value, err}
	}()
	return ch
}

func TestConcurrentRepliesMatchIDs(t *testing.T) {
	c, p := pipes(t)
	a := callString(c, context.Background(), "a")
	ra := p.request(t)
	b := callString(c, context.Background(), "b")
	rb := p.request(t)
	if rb.ID <= ra.ID || ra.Method != "a" || rb.Method != "b" || string(ra.Params) != "{}" {
		t.Fatalf("requests: %+v %+v", ra, rb)
	}
	p.result(t, rb.ID, "second")
	p.result(t, ra.ID, "first")
	if got := receive(t, a); got.err != nil || got.value != "first" {
		t.Fatalf("a: %+v", got)
	}
	if got := receive(t, b); got.err != nil || got.value != "second" {
		t.Fatalf("b: %+v", got)
	}
}

func TestRPCErrorDoesNotBreakConnection(t *testing.T) {
	c, p := pipes(t)
	call := callString(c, context.Background(), "bad")
	r := p.request(t)
	fmt.Fprintf(p.output, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"error\":{\"code\":-32001,\"message\":\"not a repository\"}}\n", r.ID)
	var remote *RPCError
	if err := receive(t, call).err; !errors.As(err, &remote) || remote.Code != -32001 {
		t.Fatalf("expected RPC error, got %v", err)
	}
	if c.Err() != nil {
		t.Fatalf("remote error closed client: %v", c.Err())
	}
	next := callString(c, context.Background(), "good")
	p.result(t, p.request(t).ID, "ok")
	if got := receive(t, next); got.err != nil || got.value != "ok" {
		t.Fatalf("next call: %+v", got)
	}
}

func TestProtocolFailuresReleasePending(t *testing.T) {
	for _, line := range []string{
		"not JSON", "", "{}", `{"jsonrpc":"1.0","id":1,"result":1}`,
		`{"jsonrpc":"2.0","id":0,"result":1}`,
		`{"jsonrpc":"2.0","id":"1","result":1}`,
		`{"jsonrpc":"2.0","id":1.5,"result":1}`,
		`{"jsonrpc":"2.0","id":1}`,
		`{"jsonrpc":"2.0","id":1,"result":null}`,
		`{"jsonrpc":"2.0","id":1,"result":1,"error":{"code":1,"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":1,"error":null}`,
		`{"jsonrpc":"2.0","id":1,"error":{"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":1,"message":3}}`,
		`{"jsonrpc":"2.0","id":99,"result":"unsolicited"}`,
	} {
		t.Run(line, func(t *testing.T) {
			c, p := pipes(t)
			call := callString(c, context.Background(), "test")
			p.request(t)
			fmt.Fprintln(p.output, line)
			var protocol *ProtocolError
			if err := receive(t, call).err; !errors.As(err, &protocol) {
				t.Fatalf("expected protocol error, got %v", err)
			}
			receive(t, c.Done())
			if c.Err() == nil || len(c.calls) != 0 {
				t.Fatalf("terminal state: %v, pending %d", c.Err(), len(c.calls))
			}
		})
	}
}

func TestCancelIgnoresOnlyItsLateReply(t *testing.T) {
	c, p := pipes(t)
	ctx, cancel := context.WithCancel(context.Background())
	call := callString(c, ctx, "old selection")
	r := p.request(t)
	cancel()
	if err := receive(t, call).err; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	notification := p.request(t)
	var target struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(notification.Params, &target); err != nil || notification.Method != "$/cancelRequest" || notification.ID != 0 || target.ID != r.ID {
		t.Fatalf("wrong cancellation scope: %+v", notification)
	}
	p.result(t, r.ID, "old")
	current := callString(c, context.Background(), "new selection")
	p.result(t, p.request(t).ID, "current")
	if got := receive(t, current); got.err != nil || got.value != "current" {
		t.Fatalf("current: %+v", got)
	}
	fmt.Fprintf(p.output, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":\"duplicate\"}\n", r.ID)
	receive(t, c.Done())
	var protocol *ProtocolError
	if !errors.As(c.Err(), &protocol) {
		t.Fatalf("duplicate response was hidden: %v", c.Err())
	}
}

func TestLargePatchResponse(t *testing.T) {
	c, p := pipes(t)
	call := callString(c, context.Background(), "repository.diff")
	text := strings.Repeat("+actual patch line\n", 12000)
	p.result(t, p.request(t).ID, text)
	if got := receive(t, call); got.err != nil || got.value != text {
		t.Fatalf("large response: length %d, error %v", len(got.value), got.err)
	}
}

func TestCloseAndEOFCleanPending(t *testing.T) {
	for _, eof := range []bool{false, true} {
		t.Run(fmt.Sprint(eof), func(t *testing.T) {
			c, p := pipes(t)
			call := callString(c, context.Background(), "pending")
			p.request(t)
			if eof {
				p.output.Close()
			} else if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			want := ErrClosed
			if eof {
				want = ErrDisconnected
			}
			if err := receive(t, call).err; !errors.Is(err, want) {
				t.Fatalf("want %v, got %v", want, err)
			}
			var value string
			if err := c.Call(context.Background(), "after close", nil, &value); !errors.Is(err, want) {
				t.Fatalf("call after close: %v", err)
			}
			if len(c.calls) != 0 {
				t.Fatalf("pending leak: %d", len(c.calls))
			}
		})
	}
}

func TestCancelWhilePeerIsNotReading(t *testing.T) {
	c, _ := pipes(t)
	ctx, cancel := context.WithCancel(context.Background())
	call := callString(c, ctx, "blocked writer")
	cancel()
	if err := receive(t, call).err; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestResultTypeErrorIsTerminal(t *testing.T) {
	c, p := pipes(t)
	call := callString(c, context.Background(), "string method")
	p.result(t, p.request(t).ID, map[string]string{"unexpected": "object"})
	var protocol *ProtocolError
	if err := receive(t, call).err; !errors.As(err, &protocol) {
		t.Fatalf("result type error: %v", err)
	}
	if c.Err() == nil {
		t.Fatal("schema error did not close client")
	}
}
