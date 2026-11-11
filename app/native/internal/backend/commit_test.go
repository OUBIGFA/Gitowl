package backend

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// These tests exercise the production JSON client over real pipes. A protocol
// confirmation fixture is not evidence of a Git write; real staging/hooks and
// the native success UI are covered separately with the actual Node service.
func TestCommitJSONContract(t *testing.T) {
	c, p := pipes(t)
	done := make(chan struct {
		result CommitResult
		err    error
	}, 1)
	paths := []string{"目录/first.txt", "new file.txt"}
	go func() {
		result, err := c.Commit(context.Background(), 9, paths, "保存版本", "说明\nsecond line")
		done <- struct {
			result CommitResult
			err    error
		}{result, err}
	}()
	r := p.request(t)
	var params map[string]any
	if err := json.Unmarshal(r.Params, &params); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": float64(9), "paths": []any{"目录/first.txt", "new file.txt"}, "summary": "保存版本", "description": "说明\nsecond line"}
	if r.Method != "repository.commit" || !reflect.DeepEqual(params, want) {
		t.Fatalf("commit wire request: %s %s", r.Method, r.Params)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	p.result(t, r.ID, map[string]string{"sha": sha})
	if got := receive(t, done); got.err != nil || got.result.SHA != sha {
		t.Fatalf("commit confirmation decoding: %+v", got)
	}
}

func TestCommitErrorIsNotRetriedAndEmptyDescriptionIsOmitted(t *testing.T) {
	c, p := pipes(t)
	done := make(chan error, 1)
	go func() {
		_, err := c.Commit(context.Background(), 1, []string{"file.txt"}, "summary", "")
		done <- err
	}()
	r := p.request(t)
	var params map[string]any
	if err := json.Unmarshal(r.Params, &params); err != nil {
		t.Fatal(err)
	}
	if _, exists := params["description"]; exists {
		t.Fatalf("empty optional description was sent: %s", r.Params)
	}
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "error": RPCError{Code: -32002, Message: "pre-commit hook rejected changes"}})
	p.output.Write(append(data, '\n'))
	var remote *RPCError
	if err := receive(t, done); !errors.As(err, &remote) || remote.Code != -32002 {
		t.Fatalf("hook failure: %v", err)
	}
	c.mu.Lock()
	issued, pending := c.nextID, len(c.calls)
	c.mu.Unlock()
	if issued != 1 || pending != 0 || c.Err() != nil {
		t.Fatalf("commit failure retried or broke transport: ids=%d pending=%d error=%v", issued, pending, c.Err())
	}
}

func TestCommitRequiresSHABeforeSuccess(t *testing.T) {
	c, p := pipes(t)
	done := make(chan error, 1)
	go func() {
		_, err := c.Commit(context.Background(), 1, []string{"file.txt"}, "summary", "")
		done <- err
	}()
	p.result(t, p.request(t).ID, map[string]any{})
	var protocol *ProtocolError
	if err := receive(t, done); !errors.As(err, &protocol) {
		t.Fatalf("missing sha became success: %v", err)
	}
}
