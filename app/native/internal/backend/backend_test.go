package backend

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestReadOnlyContract(t *testing.T) {
	branch, upstream, operation := "main", "origin/main", "rebase"
	text := "diff --git a/a.txt b/a.txt\n"
	files := []File{{Path: "a.txt", Status: "Renamed", OldPath: "old.txt", IsSubmodule: false}}
	commits := []Commit{{SHA: "0123456789", ShortSHA: "0123456", Summary: "真实提交", Body: "Body", AuthorName: "Author", AuthorEmail: "author@example.test", Date: "2026-01-01T00:00:00.000Z", Tags: []string{"v1"}}}
	for _, test := range []struct {
		method string
		params map[string]any
		result any
		call   func(*Client) (any, error)
	}{
		{"system.info", map[string]any{}, Info{1, "0.0.1", []string{"repository.open"}}, func(c *Client) (any, error) { return c.Info(context.Background()) }},
		{"repository.open", map[string]any{"path": "C:/repos/仓库"}, Repository{7, "C:/repos/仓库", "仓库"}, func(c *Client) (any, error) { return c.Open(context.Background(), "C:/repos/仓库") }},
		{"repository.status", map[string]any{"id": float64(7)}, Status{&branch, &upstream, 2, 3, true, &operation, files}, func(c *Client) (any, error) { return c.Status(context.Background(), 7) }},
		{"repository.history", map[string]any{"id": float64(7), "skip": float64(20), "limit": float64(50)}, commits, func(c *Client) (any, error) { return c.History(context.Background(), 7, 20, 50) }},
		{"repository.changeset", map[string]any{"id": float64(7), "commit": "0123456789"}, Changeset{files, 12, 9}, func(c *Client) (any, error) { return c.Changeset(context.Background(), 7, "0123456789") }},
		{"repository.diff", map[string]any{"id": float64(7), "path": "a.txt"}, Diff{Kind: "text", Text: &text}, func(c *Client) (any, error) { return c.Diff(context.Background(), 7, "a.txt", "") }},
		{"repository.diff", map[string]any{"id": float64(7), "path": "image.png", "commit": "0123456789"}, Diff{Kind: "image", Current: &Blob{Contents: []byte{1, 2, 3}, MediaType: "image/png", Bytes: 3}}, func(c *Client) (any, error) { return c.Diff(context.Background(), 7, "image.png", "0123456789") }},
	} {
		t.Run(test.method, func(t *testing.T) {
			c, p := pipes(t)
			done := make(chan struct {
				value any
				err   error
			}, 1)
			go func() {
				value, err := test.call(c)
				done <- struct {
					value any
					err   error
				}{value, err}
			}()
			r := p.request(t)
			var params map[string]any
			if err := json.Unmarshal(r.Params, &params); err != nil {
				t.Fatal(err)
			}
			if r.Method != test.method || !reflect.DeepEqual(params, test.params) {
				t.Fatalf("request %s %s, want %s %v", r.Method, r.Params, test.method, test.params)
			}
			p.result(t, r.ID, test.result)
			if got := receive(t, done); got.err != nil || !reflect.DeepEqual(got.value, test.result) {
				t.Fatalf("result %#v / %v, want %#v", got.value, got.err, test.result)
			}
		})
	}
}

func TestNullableStatusAndEmptyHistory(t *testing.T) {
	c, p := pipes(t)
	done := make(chan error, 1)
	go func() {
		status, err := c.Status(context.Background(), 1)
		if err == nil && (status.Branch != nil || status.Upstream != nil || status.Operation != nil || len(status.Files) != 0) {
			err = errors.New("nullable status was changed")
		}
		done <- err
	}()
	p.result(t, p.request(t).ID, Status{Files: []File{}})
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
	go func() {
		commits, err := c.History(context.Background(), 1, 0, 100)
		if err == nil && len(commits) != 0 {
			err = errors.New("empty history was not empty")
		}
		done <- err
	}()
	p.result(t, p.request(t).ID, []Commit{})
	if err := receive(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestMissingRepositoryResultIsNotSuccess(t *testing.T) {
	c, p := pipes(t)
	done := make(chan error, 1)
	go func() {
		_, err := c.Open(context.Background(), "missing")
		done <- err
	}()
	p.result(t, p.request(t).ID, map[string]any{})
	var protocol *ProtocolError
	if err := receive(t, done); !errors.As(err, &protocol) {
		t.Fatalf("empty object looked successful: %v", err)
	}
}
