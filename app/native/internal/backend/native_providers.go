package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

const nativeProviderStorage = "gitowl-commit-generation-providers"

type nativeProviders struct {
	Version          int                      `json:"version"`
	Providers        []Provider               `json:"providers"`
	CurrentSelection *KernelProviderSelection `json:"currentSelection"`
}

func validateNativeProvider(p ProviderSettings) error {
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return errors.New("invalid provider address")
	}
	local := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return errors.New("provider address must use HTTPS or local HTTP")
	}
	if !slices.Contains([]string{"openai-responses", "openai-chat", "azure-openai-v1", "anthropic-messages", "gemini", "ollama"}, p.Format) || strings.TrimSpace(p.Name) == "" {
		return errors.New("invalid provider settings")
	}
	if !slices.Contains([]string{"apiKey", "bearer", "none"}, p.AuthKind) || (p.Format == "gemini" && p.AuthKind != "apiKey") {
		return errors.New("unsupported provider authentication")
	}
	if p.RequestTimeoutSeconds < 0 || p.RequestTimeoutSeconds > 2147483 {
		return errors.New("invalid provider timeout")
	}
	return nil
}
func (n *NativeOwner) loadProviders(ctx context.Context) error {
	raw, err := n.store.Get(ctx, nativeProviderStorage)
	if err != nil {
		return err
	}
	if raw != "" {
		var state nativeProviders
		if json.Unmarshal([]byte(raw), &state) != nil || state.Version != 1 {
			return errors.New("saved provider data is invalid")
		}
		for _, p := range state.Providers {
			if err := validateNativeProvider(p.ProviderSettings); err != nil {
				return err
			}
		}
		n.state.CommitGeneration.Providers = state.Providers
		n.state.CommitGeneration.Selection = state.CurrentSelection
	}
	if source, err := n.store.Get(ctx, "gitowl-commit-generation-source"); err != nil {
		return err
	} else if source == "custom" {
		n.state.CommitGeneration.Source = source
	}
	if value, err := n.store.Get(ctx, "gitowl-commit-generation-language"); err != nil {
		return err
	} else if value != "" {
		if value != "auto" {
			if _, err := language.Parse(value); err != nil {
				return err
			}
		}
		n.state.CommitGeneration.Language = value
	}
	n.providerModels()
	return nil
}
func (n *NativeOwner) providerModels() {
	g := n.state.CommitGeneration
	g.EnabledModels = []KernelEnabledProviderModel{}
	valid := g.Selection == nil
	for _, provider := range g.Providers {
		for _, model := range provider.Models {
			if model.Enabled && model.Available {
				selection := KernelProviderSelection{provider.ID, model.ID}
				g.EnabledModels = append(g.EnabledModels, KernelEnabledProviderModel{selection, provider.Name, model.Name})
				if g.Selection != nil && *g.Selection == selection {
					valid = true
				}
			}
		}
	}
	if !valid {
		g.Selection = nil
	}
}
func (n *NativeOwner) saveProviders(ctx context.Context) error {
	n.providerModels()
	g := n.state.CommitGeneration
	data, err := json.Marshal(nativeProviders{1, g.Providers, g.Selection})
	if err != nil {
		return err
	}
	_, err = n.store.db.Transaction(ctx, []sqlite.Statement{
		{SQL: "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", Args: []any{nativeProviderStorage, string(data)}},
		{SQL: "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", Args: []any{"gitowl-commit-generation-source", g.Source}},
		{SQL: "INSERT INTO settings(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", Args: []any{"gitowl-commit-generation-language", g.Language}},
	})
	return err
}

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

func (n *NativeOwner) providerCommand(ctx context.Context, command nativeCommand) (resultErr error) {
	g := n.state.CommitGeneration
	// Settings and catalogs publish only after SQLite commits. A failed save
	// also restores the previous OS secret, rather than stranding the account.
	before, err := json.Marshal(g)
	if err != nil {
		return err
	}
	var restoreSecret func() error
	defer func() {
		if resultErr != nil {
			var previous KernelGenerationSettings
			if err := json.Unmarshal(before, &previous); err == nil {
				n.state.CommitGeneration = &previous
			}
			if restoreSecret != nil {
				resultErr = errors.Join(resultErr, restoreSecret())
			}
		}
	}()
	secretStore := mygo.Secrets.Service(n.credentialNamespace + " :: Gitowl")
	changeSecret := func(id string, value *string) error {
		key := "commit-generation-provider:" + id
		old, err := secretStore.Get(key)
		if err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
			return err
		}
		missing := errors.Is(err, mygo.ErrSecretNotFound)
		restoreSecret = func() error {
			if missing {
				return secretStore.Delete(key)
			}
			return secretStore.Set(key, old)
		}
		if value == nil {
			return secretStore.Delete(key)
		}
		return secretStore.Set(key, []byte(*value))
	}
	index := slices.IndexFunc(g.Providers, func(p Provider) bool { return p.ID == command.ID || p.ID == command.ProviderID })
	switch command.Name {
	case "generation.provider.save":
		p := command.Provider
		if err := validateNativeProvider(p); err != nil {
			return err
		}
		index = slices.IndexFunc(g.Providers, func(old Provider) bool { return old.ID == p.ID })
		if p.ID == "" {
			p.ID = uuid.NewString()
		} else if index < 0 {
			return errors.New("provider no longer exists")
		}
		provider := Provider{ProviderSettings: p, Models: []ProviderModel{}}
		if index >= 0 {
			provider.Models = g.Providers[index].Models
		}
		binding, err := providerBinding(p)
		if err != nil {
			return err
		}
		if p.AuthKind != "none" {
			if command.Secret != nil {
				if strings.TrimSpace(*command.Secret) == "" {
					return errors.New("provider credential is required")
				}
				value := binding + *command.Secret
				if err := changeSecret(p.ID, &value); err != nil {
					return errors.New("provider credential could not be saved")
				}
			} else if _, err := n.providerSecret(provider); err != nil {
				return err
			}
		} else {
			if err := changeSecret(p.ID, nil); err != nil {
				return err
			}
		}
		if index < 0 {
			g.Providers = append(g.Providers, provider)
		} else {
			old := g.Providers[index]
			if old.BaseURL != p.BaseURL || old.Format != p.Format || old.AuthKind != p.AuthKind {
				for i := range provider.Models {
					provider.Models[i].Available = false
				}
			}
			g.Providers[index] = provider
		}
	case "generation.provider.rename":
		if index < 0 {
			return errors.New("provider no longer exists")
		}
		if strings.TrimSpace(command.ProviderName) == "" {
			return errors.New("provider name is required")
		}
		g.Providers[index].Name = command.ProviderName
	case "generation.provider.delete":
		if index < 0 {
			return errors.New("provider no longer exists")
		}
		p := g.Providers[index]
		if err := changeSecret(p.ID, nil); err != nil {
			return err
		}
		g.Providers = slices.Delete(g.Providers, index, index+1)
	case "generation.provider.models.refresh":
		if index < 0 {
			return errors.New("provider no longer exists")
		}
		p := g.Providers[index]
		secret, err := n.providerSecret(p)
		if err != nil {
			return err
		}
		seconds := p.RequestTimeoutSeconds
		if seconds == 0 {
			seconds = 60
		}
		request, cancel := context.WithTimeout(ctx, time.Duration(seconds*float64(time.Second)))
		defer cancel()
		models, err := providerCatalog(request, nativeHTTPClient(), p, secret)
		if err != nil {
			return err
		}
		for i := range p.Models {
			p.Models[i].Available = false
		}
		for _, model := range models {
			old := slices.IndexFunc(p.Models, func(m ProviderModel) bool { return m.ID == model.ID })
			if old >= 0 {
				model.Enabled = p.Models[old].Enabled
				p.Models[old] = model
			} else {
				p.Models = append(p.Models, model)
			}
		}
		g.Providers[index] = p
	case "generation.provider.models.enabled":
		if index < 0 {
			return errors.New("provider no longer exists")
		}
		found := false
		for i := range g.Providers[index].Models {
			model := &g.Providers[index].Models[i]
			if model.ID == command.ModelID {
				model.Enabled = command.Enabled
				found = true
			}
		}
		if !found {
			return errors.New("model no longer exists")
		}
	case "generation.provider.deployments.set":
		if index < 0 || g.Providers[index].Format != "azure-openai-v1" {
			return errors.New("deployment names belong to an Azure provider")
		}
		models := []ProviderModel{}
		seen := make(map[string]bool)
		for _, deployment := range command.Models {
			if strings.TrimSpace(deployment.ID) == "" || seen[deployment.ID] {
				return errors.New("enter unique deployment names")
			}
			seen[deployment.ID] = true
			model := ProviderModel{ID: deployment.ID, Name: deployment.Name, Available: true}
			if model.Name == "" {
				model.Name = model.ID
			}
			for _, old := range g.Providers[index].Models {
				if old.ID == model.ID {
					model.Enabled = old.Enabled
				}
			}
			models = append(models, model)
		}
		g.Providers[index].Models = models
	case "generation.select":
		if command.Source != "custom" && command.Source != "copilot" {
			return errors.New("unknown generation source")
		}
		if command.Source == "custom" && command.Selection != nil && !slices.ContainsFunc(g.EnabledModels, func(model KernelEnabledProviderModel) bool {
			return model.KernelProviderSelection == *command.Selection
		}) {
			return errors.New("select an enabled available provider model")
		}
		g.Source = command.Source
		g.Selection = command.Selection
	case "generation.language":
		if command.Language != "auto" {
			if _, err := language.Parse(command.Language); err != nil {
				return errors.New("invalid generation language")
			}
		}
		g.Language = command.Language
	default:
		return fmt.Errorf("unknown provider action: %s", command.Name)
	}
	return n.saveProviders(ctx)
}
