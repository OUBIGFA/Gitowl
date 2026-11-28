package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func providerHeaders(p Provider, secret string) http.Header {
	h := make(http.Header)
	if p.Format == "anthropic-messages" {
		h.Set("anthropic-version", "2026-10-21")
	}
	if p.AuthKind != "none" {
		switch {
		case p.Format == "gemini":
			h.Set("x-goog-api-key", secret)
		case p.Format == "azure-openai-v1" && p.AuthKind == "apiKey":
			h.Set("api-key", secret)
		default:
			h.Set("Authorization", "Bearer "+secret)
		}
	}
	return h
}

// These wire paths and envelopes mirror the application's locked official SDK
// adapters. No environment API keys, automatic retries, model fallback or JSON
// repair are introduced at the native transport boundary.
func providerCatalog(ctx context.Context, client *http.Client, p Provider, secret string) ([]ProviderModel, error) {
	if p.Format == "azure-openai-v1" {
		return nil, errors.New("enter Azure deployment names explicitly")
	}
	base := strings.TrimSuffix(p.BaseURL, "/")
	path := "/models"
	switch p.Format {
	case "anthropic-messages":
		path = "/v1/models?limit=100"
	case "gemini":
		path = "/v1/models?pageSize=100"
	case "ollama":
		path = "/api/tags"
	}
	models := []ProviderModel{}
	seen := make(map[string]bool)
	cursors := make(map[string]bool)
	for {
		var response struct {
			Data []struct {
				ID        string `json:"id"`
				Name      string `json:"display_name"`
				Lifecycle string `json:"lifecycle"`
			} `json:"data"`
			Models []struct {
				ID          string   `json:"model"`
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Methods     []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			HasMore       bool   `json:"has_more"`
			LastID        string `json:"last_id"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := nativeJSON(ctx, client, http.MethodGet, base+path, nil, providerHeaders(p, secret), &response); err != nil {
			return nil, err
		}
		add := func(id, name string) error {
			if id == "" || seen[id] {
				return errors.New("provider returned an invalid model catalog")
			}
			if name == "" {
				name = id
			}
			seen[id] = true
			models = append(models, ProviderModel{ID: id, Name: name, Available: true})
			return nil
		}
		if p.Format == "gemini" || p.Format == "ollama" {
			if response.Models == nil {
				return nil, errors.New("provider model list is missing")
			}
			for _, model := range response.Models {
				if p.Format == "gemini" {
					if !slices.Contains(model.Methods, "generateContent") {
						continue
					}
					if err := add(model.Name, model.DisplayName); err != nil {
						return nil, err
					}
				} else {
					id := model.ID
					if id == "" {
						id = model.Name
					}
					if err := add(id, model.Name); err != nil {
						return nil, err
					}
				}
			}
		} else {
			if response.Data == nil {
				return nil, errors.New("provider model list is missing")
			}
			for _, model := range response.Data {
				if model.Lifecycle != "retired" {
					if err := add(model.ID, model.Name); err != nil {
						return nil, err
					}
				}
			}
		}
		cursor := ""
		if p.Format == "anthropic-messages" && response.HasMore {
			cursor = response.LastID
			path = "/v1/models?limit=100&after_id=" + url.QueryEscape(cursor)
		}
		if p.Format == "gemini" {
			cursor = response.NextPageToken
			path = "/v1/models?pageSize=100&pageToken=" + url.QueryEscape(cursor)
		}
		if cursor == "" {
			if response.HasMore {
				return nil, errors.New("provider pagination cursor is missing")
			}
			break
		}
		if cursors[cursor] {
			return nil, errors.New("provider pagination repeated a cursor")
		}
		cursors[cursor] = true
	}
	return models, nil
}

type providerTextBlock struct {
	Type                string          `json:"type"`
	Text                string          `json:"text"`
	Thought             bool            `json:"thought"`
	FunctionCall        json.RawMessage `json:"functionCall"`
	InlineData          json.RawMessage `json:"inlineData"`
	FileData            json.RawMessage `json:"fileData"`
	FunctionResponse    json.RawMessage `json:"functionResponse"`
	ToolCall            json.RawMessage `json:"toolCall"`
	ToolResponse        json.RawMessage `json:"toolResponse"`
	ExecutableCode      json.RawMessage `json:"executableCode"`
	CodeExecutionResult json.RawMessage `json:"codeExecutionResult"`
}
type providerMessage struct {
	Role         string              `json:"role"`
	Content      string              `json:"content"`
	ToolCalls    []json.RawMessage   `json:"tool_calls"`
	FunctionCall json.RawMessage     `json:"function_call"`
	Refusal      *string             `json:"refusal"`
	Parts        []providerTextBlock `json:"parts"`
	Audio        json.RawMessage     `json:"audio"`
	Images       []json.RawMessage   `json:"images"`
}

func hasJSONValue(value json.RawMessage) bool { return len(value) > 0 && string(value) != "null" }

func providerGenerate(ctx context.Context, client *http.Client, p Provider, model, secret, prompt string) (string, error) {
	if !slices.ContainsFunc(p.Models, func(m ProviderModel) bool { return m.ID == model && m.Enabled && m.Available }) {
		return "", errors.New("select an enabled available model")
	}
	base := strings.TrimSuffix(p.BaseURL, "/")
	path := ""
	var body any
	switch p.Format {
	case "openai-responses", "azure-openai-v1":
		path = "/responses"
		body = map[string]any{"model": model, "input": prompt, "store": false, "stream": false}
	case "openai-chat":
		path = "/chat/completions"
		body = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "stream": false}
	case "anthropic-messages":
		path = "/v1/messages"
		body = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": 4096, "stream": false}
	case "gemini":
		path = "/v1/models/" + url.PathEscape(strings.TrimPrefix(model, "models/")) + ":generateContent"
		body = map[string]any{"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": prompt}}}}}
	case "ollama":
		path = "/api/chat"
		body = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "stream": false}
	default:
		return "", errors.New("unsupported provider protocol")
	}
	var response struct {
		Status     string          `json:"status"`
		Error      json.RawMessage `json:"error"`
		Incomplete json.RawMessage `json:"incomplete_details"`
		Output     []struct {
			Type, Status, Role, Phase string
			Content                   []providerTextBlock
		} `json:"output"`
		Choices []struct {
			FinishReason string          `json:"finish_reason"`
			Message      providerMessage `json:"message"`
		} `json:"choices"`
		Role       string              `json:"role"`
		StopReason string              `json:"stop_reason"`
		Content    []providerTextBlock `json:"content"`
		Candidates []struct {
			FinishReason string          `json:"finishReason"`
			Content      providerMessage `json:"content"`
		} `json:"candidates"`
		PromptFeedback json.RawMessage `json:"promptFeedback"`
		Done           bool            `json:"done"`
		DoneReason     string          `json:"done_reason"`
		Message        providerMessage `json:"message"`
	}
	if err := nativeJSON(ctx, client, http.MethodPost, base+path, body, providerHeaders(p, secret), &response); err != nil {
		return "", err
	}
	var text strings.Builder
	switch p.Format {
	case "openai-responses", "azure-openai-v1":
		if response.Status != "completed" || (len(response.Error) > 0 && string(response.Error) != "null") || (len(response.Incomplete) > 0 && string(response.Incomplete) != "null") {
			return "", errors.New("model response did not complete")
		}
		for _, item := range response.Output {
			if item.Type == "reasoning" {
				continue
			}
			if item.Type != "message" || item.Role != "assistant" || item.Status != "completed" {
				return "", errors.New("model returned an unsupported response")
			}
			for _, part := range item.Content {
				if part.Type != "output_text" {
					return "", errors.New("model returned non-text or refused output")
				}
				if item.Phase != "commentary" {
					text.WriteString(part.Text)
				}
			}
		}
	case "openai-chat":
		if len(response.Choices) != 1 {
			return "", errors.New("model returned an invalid number of choices")
		}
		choice := response.Choices[0]
		if choice.FinishReason != "stop" || choice.Message.Role != "assistant" || len(choice.Message.ToolCalls) > 0 || hasJSONValue(choice.Message.FunctionCall) || hasJSONValue(choice.Message.Audio) || choice.Message.Refusal != nil {
			return "", errors.New("model response was incomplete, refused or requested tools")
		}
		text.WriteString(choice.Message.Content)
	case "anthropic-messages":
		if response.StopReason != "end_turn" || response.Role != "assistant" {
			return "", errors.New("model response did not complete")
		}
		for _, part := range response.Content {
			if part.Type == "thinking" || part.Type == "redacted_thinking" {
				continue
			}
			if part.Type != "text" {
				return "", errors.New("model returned non-text output")
			}
			text.WriteString(part.Text)
		}
	case "gemini":
		if len(response.Candidates) != 1 || response.Candidates[0].FinishReason != "STOP" || response.Candidates[0].Content.Role != "model" {
			return "", errors.New("model response did not complete")
		}
		for _, part := range response.Candidates[0].Content.Parts {
			if hasJSONValue(part.FunctionCall) || hasJSONValue(part.FunctionResponse) || hasJSONValue(part.ToolCall) || hasJSONValue(part.ToolResponse) || hasJSONValue(part.ExecutableCode) || hasJSONValue(part.CodeExecutionResult) || hasJSONValue(part.InlineData) || hasJSONValue(part.FileData) {
				return "", errors.New("model returned tools or non-text output")
			}
			if !part.Thought {
				text.WriteString(part.Text)
			}
		}
	case "ollama":
		if !response.Done || response.DoneReason != "stop" || response.Message.Role != "assistant" || len(response.Message.ToolCalls) > 0 || len(response.Message.Images) > 0 {
			return "", errors.New("model response did not complete")
		}
		text.WriteString(response.Message.Content)
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", errors.New("model returned no text")
	}
	return text.String(), nil
}
