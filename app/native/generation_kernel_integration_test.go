package main

import (
	"encoding/json"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// Reuse the existing real Electron/Git/UI fixture; no generation service or
// successful business reply is substituted. Only the unpaid HTTP endpoint is
// a fixture, and the installed official Ollama SDK must speak its real protocol.
func TestNativeGenerationUsesOriginalDesktopKernel(t *testing.T) {
	exerciseOriginalKernelView(t, "generation")
}

type originalGenerationView struct {
	app    *app
	tester *ui.Tester
	await  func(func() bool)
	click  func(string)
	git    func(...string) string
	repo   string
}

type generationRequestMessage struct {
	role, content string
}

func clickGenerationControl(t *testing.T, tt *ui.Tester, role ui.Role, label string) {
	t.Helper()
	control, ok := tt.AccessByRole(role, label)
	if !ok || control.Disabled || control.Bounds.W <= 0 || control.Bounds.H <= 0 {
		t.Fatalf("generation control %q is not available: %+v; texts=%q", label, control, tt.Texts())
	}
	// Field groups share the label with their input. Target the real accessible
	// control, not the first same-named group rectangle chosen by Click(label).
	tt.ClickAt(control.Bounds.X+control.Bounds.W/2, control.Bounds.Y+control.Bounds.H/2)
	tt.Advance(time.Second)
}

func exerciseOriginalKernelGeneration(t *testing.T, h originalGenerationView) {
	t.Helper()
	const modelID = "fixture:latest"
	const modelName = "Fixture model"
	const providerName = "Local generation fixture"
	const title = "真实内核生成摘要"
	var mu sync.Mutex
	var requests []generationRequestMessage
	generationCount := 0
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "content-type,authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/api/tags" && r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"model": modelID, "name": modelName}, {"model": "unchecked:latest", "name": "Unchecked fixture model"}}})
			return
		}
		if r.URL.Path != "/api/chat" || r.Method != "POST" || r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected fixture request", http.StatusBadRequest)
			return
		}
		var input struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Model != modelID || len(input.Messages) != 2 {
			http.Error(w, "invalid Ollama fixture body", http.StatusBadRequest)
			return
		}
		mu.Lock()
		generationCount++
		count := generationCount
		for _, message := range input.Messages {
			requests = append(requests, generationRequestMessage{message.Role, message.Content})
		}
		mu.Unlock()
		if count > 1 {
			started <- struct{}{}
			<-r.Context().Done() // The original cancel command must abort real HTTP.
			return
		}
		content, _ := json.Marshal(map[string]string{"title": title, "description": "仅填入草稿，不自动提交"})
		json.NewEncoder(w).Encode(map[string]any{"model": modelID, "done": true, "done_reason": "stop", "message": map[string]string{"role": "assistant", "content": string(content)}})
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()
	a, tt, tx := h.app, h.tester, locales.In("en")
	if a.coreGenerationSettings() == nil || a.backend != nil || a.generation != nil || a.generationConfig != nil {
		t.Fatal("generation settings did not come from the original kernel snapshot")
	}
	h.click(tx.Settings())
	h.await(func() bool {
		control, visible := tt.AccessByRole(ui.RoleButton, tx.CustomProviders())
		return visible && !control.Disabled && a.corePending == 0 && a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "Preferences" && slices.ContainsFunc(a.coreSnapshot.CurrentPopup.Actions, func(action backend.KernelDialogAction) bool { return action.ID == "cancel" && !action.Disabled })
	})
	h.click(tx.CustomProviders())
	h.click(tx.AddProvider())
	clickGenerationControl(t, tt, ui.RoleTextField, tx.ProviderName())
	tt.Type(providerName)
	clickGenerationControl(t, tt, ui.RolePopUpButton, tx.ProviderFormat())
	h.click("Ollama")
	clickGenerationControl(t, tt, ui.RolePopUpButton, tx.ProviderAuthKind())
	h.click(tx.ProviderNoAuth())
	clickGenerationControl(t, tt, ui.RoleTextField, tx.ProviderBaseURL())
	tt.Type(server.URL)
	h.click(tx.SaveProvider())
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && len(a.generationProviders()) == 1
	})
	providerID := a.generationProviders()[0].ID
	if providerID == "" || a.generationSettingsErr != nil {
		t.Fatalf("real provider save failed: %v", a.generationSettingsErr)
	}
	h.click(tx.ManageProviderModels())
	h.click(tx.FetchModels())
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && len(a.generationProviders()[0].Models) == 2
	})
	for _, model := range a.generationProviders()[0].Models {
		if model.Enabled || !model.Available {
			t.Fatal("a newly discovered model was enabled automatically or marked unavailable")
		}
	}
	h.click(tx.EnableModel(modelName))
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && len(coreEnabledModels(a.coreGenerationSettings())) == 1
	})
	// Original preference discovery can still be completing after the model
	// command's receipt. Wait for its actual cancel availability before Close.
	h.await(func() bool {
		control, ok := tt.AccessByRole(ui.RoleButton, tx.Close())
		return ok && !control.Disabled
	})
	clickGenerationControl(t, tt, ui.RoleButton, tx.Close())
	h.await(func() bool { return a.corePending == 0 && a.coreSnapshot.CurrentPopup == nil })
	h.click(tx.GenerationModel() + " / " + tx.GenerationLanguage())
	clickGenerationControl(t, tt, ui.RolePopUpButton, tx.GenerationModel())
	h.click(providerName + " · " + modelName)
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && coreCustomSelectionValid(a.coreGenerationSettings())
	})
	clickGenerationControl(t, tt, ui.RolePopUpButton, tx.GenerationLanguage())
	h.click(i18n.Autonym("zh-Hans"))
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && a.coreGenerationSettings().Language == "zh-Hans"
	})
	if tt.HasText(locales.In("zh-Hans").SaveVersion()) || !tt.HasText(tx.SaveVersion()) {
		t.Fatal("output language changed the English interface language")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	h.click(tx.GenerateMessage())
	h.await(func() bool {
		return a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "GenerateCommitMessageOverrideWarning"
	})
	h.click(tx.PopupDontAskAgain())
	h.click(tx.PopupOverrideAndGenerate())
	h.await(func() bool {
		return a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "GenerateCommitMessageDisclaimer"
	})
	h.click(tx.PopupUnderstandAndGenerate())
	h.await(func() bool {
		return a.corePending == 0 && !a.generating && a.coreSnapshot.Selected.Message.Summary == title
	})
	if a.coreSnapshot.Selected.Message.GeneratedByCopilot || a.generationErr != nil || a.generation != nil || a.generationConfig != nil {
		t.Fatalf("custom fill used a second store, false Copilot attribution or failed: %v", a.generationErr)
	}
	mu.Lock()
	count := generationCount
	messages := append([]generationRequestMessage(nil), requests...)
	mu.Unlock()
	if count != 1 || len(messages) != 2 || messages[0].role != "system" || messages[1].role != "user" || !strings.Contains(messages[0].content, "language zh-Hans (BCP47)") || !strings.Contains(messages[1].content, "+after") || strings.Contains(messages[1].content, "unselected.txt") {
		t.Fatalf("actual original-kernel SDK request lost selection/language boundaries: count=%d, messages=%+v", count, messages)
	}
	h.click(tx.GenerateMessage())
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the second original request did not reach the HTTP cancellation fixture")
	}
	h.await(func() bool { return a.generating })
	h.click(tx.StopGeneration())
	h.await(func() bool { return a.corePending == 0 && !a.generating && a.core.InFlight() == 0 })
	if a.coreSnapshot.Selected.Message.Summary != title {
		t.Fatal("canceling real generation overwrote the original stored draft")
	}
	if h.git("-C", h.repo, "rev-list", "--count", "HEAD") != "1" || h.git("-C", h.repo, "diff", "--name-only") != "selected.txt\nunselected.txt" {
		t.Fatal("generation or cancellation created a commit or consumed selected/unselected changes")
	}
	h.click(tx.Settings())
	h.await(func() bool {
		control, visible := tt.AccessByRole(ui.RoleButton, tx.CustomProviders())
		return visible && !control.Disabled && a.corePending == 0 && a.coreSnapshot.CurrentPopup != nil && a.coreSnapshot.CurrentPopup.Type == "Preferences" && slices.ContainsFunc(a.coreSnapshot.CurrentPopup.Actions, func(action backend.KernelDialogAction) bool { return action.ID == "cancel" && !action.Disabled })
	})
	h.click(tx.CustomProviders())
	h.click(tx.CustomProviders())
	h.click(tx.EditProvider(providerName))
	h.click(tx.ManageProviderModels())
	h.click(tx.EnableModel(modelName))
	h.await(func() bool {
		return a.corePending == 0 && !a.generationSettingsBusy && a.coreGenerationSettings().Selection == nil
	})
	if a.coreGenerationSettings().Source != "custom" || a.generationProviders()[0].ID != providerID {
		t.Fatal("revoking a model changed provider identity or silently switched to Copilot")
	}
	h.await(func() bool { control, ok := tt.AccessByRole(ui.RoleButton, tx.Close()); return ok && !control.Disabled })
	clickGenerationControl(t, tt, ui.RoleButton, tx.Close())
	h.await(func() bool { return a.corePending == 0 && a.coreSnapshot.CurrentPopup == nil })
}
