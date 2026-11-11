package main

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

const (
	settingsInterface = iota
	settingsCopilot
	settingsProviders
)

const (
	providerConnection = iota
	providerModels
)

var errProviderSecretRequired = errors.New("Enter a new credential when creating or changing an authenticated provider's protocol, origin or authentication mode.")

type providerDraft struct {
	backend.ProviderSettings
	secret string // Unsent editor input only; never loaded from the service.
}

type generationSettingsUI struct {
	settingsWorkArea         func() (float32, float32)
	expandSettings           func(float32, float32)
	coreSettingsSection      string
	settingsPage             int
	providerEdit             *providerDraft
	providerDrafts           map[string]backend.ProviderSettings
	providerEditEpoch        uint64
	providerSecretEpoch      uint64
	providerSection          int
	providerInputErr         error
	providerSearch           string
	providerModelList        nativeListState
	providerDeleteOpen       bool
	manualModelOpen          bool
	manualModelID            string
	manualModelName          string
	copilotAccountID         string
	copilotModelsView        bool
	copilotSearch            string
	copilotAccountList       ui.ListState
	copilotLoginAccountsList ui.ListState
	copilotModelList         nativeListState
}

func (a *app) showSettings(page int) {
	if a.pendingGeneration != nil {
		a.invalidateGeneration(generationChanged)
	}
	a.selectSettingsPage(page)
	if a.core != nil {
		// The original Preferences popup owns its draft and cancellation.
		// Native-only categories are additional pages in that same shell.
		a.settingsModal = false
		a.coreSettingsSection = "appearance"
		if page == settingsProviders {
			a.coreSettingsSection = "providers"
		}
		if page == settingsCopilot {
			a.coreSettingsSection = ""
			a.openCoreCopilotSettings()
			return
		}
		core, ctx := a.core, a.ctx
		a.runCoreTask(func() (backend.KernelResult, error) { return core.ExecuteMenu(ctx, "preferences") }, func(_ backend.KernelResult, err error) { a.generationSettingsErr = err })
		if a.coreGenerationSettings() == nil && !a.generationSettingsBusy {
			a.loadGenerationSettings()
		}
		return
	}
	a.settingsModal = true
	if a.generationCapable && a.generationConfig == nil && !a.generationSettingsBusy {
		a.loadGenerationSettings()
	}
}

func (a *app) selectSettingsPage(page int) {
	if page != settingsCopilot {
		a.cancelCopilotLogin()
	}
	if a.settingsPage != page {
		a.clearProviderEditor()
	}
	a.settingsPage = page
	if a.core == nil && page == settingsCopilot && a.ready && a.generationCapable && !a.copilotLoading {
		a.loadCopilotAccounts()
	}
}

func (a *app) closeSettings() {
	a.settingsModal = false
	a.cancelCopilotLogin()
	a.clearProviderEditor()
}

func (a *app) clearProviderEditor() {
	if a.providerEdit != nil {
		// Navigation preserves unsaved connection text for this app session.
		// Credentials never enter this draft cache or a native undo history.
		if a.providerDrafts == nil {
			a.providerDrafts = make(map[string]backend.ProviderSettings)
		}
		settings := a.providerEdit.ProviderSettings
		if provider := a.providerByID(settings.ID); provider == nil && settings.ID == "" || provider != nil && provider.ProviderSettings != settings {
			a.providerDrafts[settings.ID] = settings
		} else {
			delete(a.providerDrafts, settings.ID)
		}
		a.providerEdit.secret = ""
	}
	a.providerEdit = nil
	a.providerSecretEpoch++
	a.providerDeleteOpen, a.manualModelOpen = false, false
}

func (a *app) configurationLocked() bool {
	if a.core != nil {
		return !a.ready || a.coreGenerationSettings() == nil || a.generationSettingsBusy || a.corePending > 0 || a.composerLocked()
	}
	return !a.ready || !a.generationCapable || a.generationSettingsBusy || a.copilotLoading || a.composerLocked()
}

func (a *app) providerByID(id string) *backend.Provider {
	providers := a.generationProviders()
	for i := range providers {
		if providers[i].ID == id {
			return &providers[i]
		}
	}
	return nil
}

func (a *app) editProvider(id string) {
	a.clearProviderEditor()
	draft := &providerDraft{ProviderSettings: backend.ProviderSettings{
		Format: "openai-responses", AuthKind: "apiKey", RequestTimeoutSeconds: 60,
	}}
	if id != "" {
		provider := a.providerByID(id)
		if provider == nil {
			return
		}
		draft.ProviderSettings = provider.ProviderSettings
		if draft.RequestTimeoutSeconds == 0 {
			draft.RequestTimeoutSeconds = 60
		}
	}
	if saved, ok := a.providerDrafts[id]; ok {
		draft.ProviderSettings = saved
	}
	a.providerEdit, a.providerSection, a.providerInputErr = draft, providerConnection, nil
	a.providerEditEpoch++
	a.providerSearch, a.manualModelID, a.manualModelName = "", "", ""
	a.providerModelList = nativeListState{}
}

func providerOrigin(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return ""
	}
	host, scheme := strings.ToLower(u.Host), strings.ToLower(u.Scheme)
	if scheme == "https" {
		host = strings.TrimSuffix(host, ":443")
	} else if scheme == "http" {
		host = strings.TrimSuffix(host, ":80")
	}
	return scheme + "://" + host
}

// This only controls the credential-entry affordance. URL/auth/secret validity
// and secure storage remain the service's responsibility, not a Go adapter.
func (a *app) providerNeedsSecret(draft *providerDraft) bool {
	if draft.AuthKind == "none" {
		return false
	}
	provider := a.providerByID(draft.ID)
	return provider == nil || provider.Format != draft.Format || provider.AuthKind != draft.AuthKind ||
		providerOrigin(draft.BaseURL) == "" || providerOrigin(provider.BaseURL) != providerOrigin(draft.BaseURL)
}

func (a *app) canSaveProvider() bool {
	draft := a.providerEdit
	return draft != nil && !a.configurationLocked() && strings.TrimSpace(draft.Name) != "" &&
		strings.TrimSpace(draft.BaseURL) != "" && (!a.providerNeedsSecret(draft) || strings.TrimSpace(draft.secret) != "")
}

func (a *app) saveProviderSettings() {
	draft := a.providerEdit
	if draft == nil || a.configurationLocked() {
		return
	}
	if a.providerNeedsSecret(draft) && strings.TrimSpace(draft.secret) == "" {
		a.providerInputErr = errProviderSecretRequired
		return
	}
	if !a.canSaveProvider() {
		return
	}
	submitted := draft.ProviderSettings
	settings := submitted
	settings.Name, settings.BaseURL = strings.TrimSpace(settings.Name), strings.TrimSpace(settings.BaseURL)
	var secret *string
	if draft.AuthKind != "none" && draft.secret != "" {
		value := draft.secret
		secret = &value
	}
	known := make(map[string]bool)
	for _, provider := range a.generationProviders() {
		known[provider.ID] = true
	}
	// Clear even on failure, including the native password field's Undo state.
	// Empty editing later means retain the service-owned key, not reveal it.
	draft.secret = ""
	a.providerSecretEpoch++
	a.providerInputErr = nil
	a.changeProviderSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
		return core.SaveGenerationProvider(ctx, settings, secret)
	}, func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.SaveProvider(ctx, settings, secret)
	}, func(receipt backend.GenerationSettings) {
		// Navigation can cache this editor while the request is in flight.
		// Acknowledge only the submitted version; never erase newer edits.
		if cached, ok := a.providerDrafts[submitted.ID]; ok && cached == submitted {
			delete(a.providerDrafts, submitted.ID)
		}
		id := settings.ID
		if id == "" {
			for _, provider := range receipt.Providers {
				if !known[provider.ID] {
					id = provider.ID
					break
				}
			}
		}
		if a.providerEdit == draft {
			// The returned receipt supersedes this draft after an explicit save.
			a.providerEdit = nil
			delete(a.providerDrafts, settings.ID)
			a.editProvider(id)
		}
	})
}

func (a *app) renameProvider() {
	draft := a.providerEdit
	if draft == nil || draft.ID == "" || a.configurationLocked() || strings.TrimSpace(draft.Name) == "" {
		return
	}
	id, name := draft.ID, strings.TrimSpace(draft.Name)
	a.changeProviderSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
		return core.RenameGenerationProvider(ctx, id, name)
	}, func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.RenameProvider(ctx, id, name)
	}, func(backend.GenerationSettings) {
		if a.providerEdit == draft {
			if provider := a.providerByID(id); provider != nil {
				// A rename does not save unrelated connection/credential edits.
				draft.Name = provider.Name
			}
		}
	})
}

func (a *app) deleteProvider() {
	if a.providerEdit == nil || a.providerEdit.ID == "" || a.configurationLocked() {
		return
	}
	id := a.providerEdit.ID
	a.providerDeleteOpen = false
	a.changeProviderSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
		return core.DeleteGenerationProvider(ctx, id)
	}, func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.DeleteProvider(ctx, id)
	}, func(backend.GenerationSettings) {
		if a.providerEdit != nil && a.providerEdit.ID == id {
			a.clearProviderEditor()
		}
		delete(a.providerDrafts, id)
	})
}

func (a *app) refreshProviderModels() {
	if a.providerEdit == nil || a.configurationLocked() {
		return
	}
	provider := a.providerByID(a.providerEdit.ID)
	if provider == nil || provider.Format == "azure-openai-v1" {
		return // Azure inference catalogs are not a list of deployments.
	}
	id := provider.ID
	a.changeProviderSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
		return core.RefreshGenerationProviderModels(ctx, id)
	}, func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.RefreshProviderModels(ctx, id)
	}, nil)
}

func (a *app) setProviderModelEnabled(id, model string, enabled bool) {
	if a.configurationLocked() {
		return
	}
	a.changeProviderSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
		return core.SetGenerationProviderModelEnabled(ctx, id, model, enabled)
	}, func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.SetProviderModelEnabled(ctx, id, model, enabled)
	}, nil)
}

func (a *app) addManualProviderModel() {
	if a.providerEdit == nil || a.configurationLocked() || strings.TrimSpace(a.manualModelID) == "" {
		return
	}
	id, model, name := a.providerEdit.ID, strings.TrimSpace(a.manualModelID), strings.TrimSpace(a.manualModelName)
	if a.core != nil {
		provider := a.providerByID(id)
		if provider == nil || provider.Format != "azure-openai-v1" {
			return
		}
		if name == "" {
			name = model
		}
		// The kernel accepts a complete explicit deployment list. Keep existing
		// available entries, not a synthetic Azure inference model directory.
		models := []backend.ProviderDeployment{{ID: model, Name: name}}
		for _, existing := range provider.Models {
			if existing.Available && existing.ID != model {
				models = append(models, backend.ProviderDeployment{ID: existing.ID, Name: existing.Name})
			}
		}
		a.changeCoreGenerationSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
			return core.SetGenerationDeployments(ctx, id, models)
		}, func(backend.GenerationSettings) {
			a.manualModelOpen, a.manualModelID, a.manualModelName = false, "", ""
		})
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.AddProviderModel(ctx, id, model, name)
	}, func(backend.GenerationSettings) {
		a.manualModelOpen, a.manualModelID, a.manualModelName = false, "", ""
	})
}

func (a *app) refreshCopilotModels(id string) {
	if a.configurationLocked() {
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.RefreshCopilotModels(ctx, id)
	}, nil)
}

func (a *app) setCopilotModelEnabled(id, model string, enabled bool) {
	if a.configurationLocked() {
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.SetCopilotModelEnabled(ctx, id, model, enabled)
	}, nil)
}

func (a *app) logoutCopilot(id string) {
	if a.configurationLocked() || a.loginFlowID != "" || a.loginBusy {
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.LogoutCopilot(ctx, id)
	}, func(backend.GenerationSettings) { a.loadCopilotAccounts() })
}

func filteredModels(models []backend.ProviderModel, query string) []backend.ProviderModel {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return models
	}
	var found []backend.ProviderModel
	for _, model := range models {
		if strings.Contains(strings.ToLower(model.Name), query) || strings.Contains(strings.ToLower(model.ID), query) {
			found = append(found, model)
		}
	}
	return found
}
