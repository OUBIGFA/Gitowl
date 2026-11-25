package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeProviderWireContracts(t *testing.T) {
	for _, tc := range []struct{ format, path, response string }{
		{"openai-responses", "/responses", `{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"draft"}]}]}`},
		{"openai-chat", "/chat/completions", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"draft"}}]}`},
		{"azure-openai-v1", "/responses", `{"status":"completed","output":[{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"draft"}]}]}`},
		{"anthropic-messages", "/v1/messages", `{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"draft"}]}`},
		{"gemini", "/v1/models/example:generateContent", `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"draft"}]}}]}`},
		{"ollama", "/api/chat", `{"done":true,"done_reason":"stop","message":{"role":"assistant","content":"draft"}}`},
	} {
		t.Run(tc.format, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != tc.path || r.Method != "POST" {
					t.Errorf("wrong request %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if tc.format != "gemini" && body["model"] != "example" {
					t.Error("model selection was changed")
				}
				if tc.format == "gemini" {
					if r.Header.Get("x-goog-api-key") != "fixture-key" {
						t.Error("missing Gemini auth")
					}
				} else if tc.format == "azure-openai-v1" {
					if r.Header.Get("api-key") != "fixture-key" {
						t.Error("missing Azure auth")
					}
				} else if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Error("missing bearer auth")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.response))
			}))
			defer server.Close()
			p := Provider{ProviderSettings: ProviderSettings{ID: "p", Name: "Fixture", Format: tc.format, BaseURL: server.URL, AuthKind: "apiKey"}, Models: []ProviderModel{{ID: "example", Enabled: true, Available: true}}}
			text, err := providerGenerate(context.Background(), server.Client(), p, "example", "fixture-key", "input")
			if err != nil || text != "draft" {
				t.Fatalf("generation %q %v", text, err)
			}
			p.Models[0].Enabled = false
			if _, err := providerGenerate(context.Background(), server.Client(), p, "example", "fixture-key", "input"); err == nil {
				t.Fatal("disabled model used")
			}
			if requests != 1 {
				t.Fatal("disabled model made a request")
			}
		})
	}
}

func TestNativeProviderRejectsTruncationToolsAndRedirects(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"partial"}}]}`,
		`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{}]}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","refusal":"refused","content":"no"}}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer server.Close()
			p := Provider{ProviderSettings: ProviderSettings{Format: "openai-chat", BaseURL: server.URL, AuthKind: "none"}, Models: []ProviderModel{{ID: "m", Enabled: true, Available: true}}}
			if text, err := providerGenerate(context.Background(), server.Client(), p, "m", "", "prompt"); err == nil || text != "" {
				t.Fatalf("invalid answer accepted %q %v", text, err)
			}
		})
	}
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { leaked = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	client := nativeHTTPClient()
	client.Transport = source.Client().Transport
	p := Provider{ProviderSettings: ProviderSettings{Format: "openai-chat", BaseURL: source.URL, AuthKind: "apiKey"}, Models: []ProviderModel{{ID: "m", Enabled: true, Available: true}}}
	_, err := providerGenerate(context.Background(), client, p, "m", "fixture-key", "private input")
	if err == nil || leaked || strings.Contains(err.Error(), "fixture-key") {
		t.Fatalf("redirect or diagnostic leak %v %v", leaked, err)
	}
}
