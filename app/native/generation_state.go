package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"golang.org/x/text/language"
)

var (
	errGenerationLanguage = errors.New("Enter a valid BCP 47 language tag, such as zh-Hans, en or fr-CA.")
	errGenerationReply    = errors.New("The generation reply did not match the selected model and output language.")
)

const (
	generationStopped = "stopped"
	generationChanged = "changed"
	generationFilled  = "filled"
)

// The receipt is the only configuration truth. Drafts, consent and pending work
// are UI state, not a second provider/model store or a credential cache.
type generationState struct {
	generation                  backend.GenerationBackend
	generationCapable           bool
	generationConfig            *backend.GenerationSettings
	generationSettingsBusy      bool
	generationSettingsErr       error
	generationSettingsErrorOpen bool
	generationSettingsSeq       uint64
	generationSettingsCancel    context.CancelFunc

	generating            bool
	generationErr         error
	generationErrorOpen   bool
	generationNotice      string
	generationSeq         uint64
	generationCancel      context.CancelFunc
	activeGeneration      *generationSnapshot
	pendingGeneration     *generationSnapshot
	generationConsentOpen bool
	draftRevision         uint64

	generationLanguageCustom bool
	generationLanguageDraft  string
	generationLanguageErr    error
}

// A confirmation and late reply snapshot the destination, not its mutable
// display name. Persistent source authorization belongs to the service.
type generationConsentSource struct {
	project, provider, account, format, baseURL, authKind string
}

type generationSnapshot struct {
	repoSeq, statusSeq, draftRevision uint64
	id                                int
	paths                             []string
	selection                         backend.ModelSelection
	language, summary, description    string
	source                            generationConsentSource
	sourceName, modelName             string
}

func modelEnabled(settings backend.GenerationSettings, selection backend.ModelSelection) bool {
	if selection.ProviderID != "" && selection.AccountID == "" {
		for _, provider := range settings.Providers {
			if provider.ID == selection.ProviderID {
				for _, model := range provider.Models {
					if model.ID == selection.ModelID {
						return model.Enabled && model.Available
					}
				}
			}
		}
	} else if selection.AccountID != "" && selection.ProviderID == "" {
		for _, catalog := range settings.CopilotCatalogs {
			if catalog.Account.SelectionID == selection.AccountID {
				for _, model := range catalog.Models {
					if model.ID == selection.ModelID {
						return model.Enabled && model.Available
					}
				}
			}
		}
	}
	return false
}

func generationOptions(settings *backend.GenerationSettings) []backend.ModelOption {
	if settings == nil {
		return nil
	}
	var options []backend.ModelOption
	for _, option := range settings.Models {
		if modelEnabled(*settings, option.Selection) {
			options = append(options, option)
		}
	}
	return options
}

func selectedGenerationOption(settings *backend.GenerationSettings) (backend.ModelOption, bool) {
	if settings != nil && settings.Selection != nil {
		for _, option := range generationOptions(settings) {
			if option.Selection == *settings.Selection {
				return option, true
			}
		}
	}
	return backend.ModelOption{}, false
}

func (a *app) generationSource(option backend.ModelOption) generationConsentSource {
	source := generationConsentSource{project: a.repo.Path, provider: option.Selection.ProviderID, account: option.Selection.AccountID}
	if source.provider != "" {
		for _, provider := range a.generationConfig.Providers {
			if provider.ID == source.provider {
				source.format, source.baseURL, source.authKind = provider.Format, provider.BaseURL, provider.AuthKind
				break
			}
		}
	} else {
		for _, catalog := range a.generationConfig.CopilotCatalogs {
			if catalog.Account.SelectionID == source.account {
				source.baseURL = catalog.Account.Host
				break
			}
		}
	}
	return source
}

func (s *generationSnapshot) current(a *app) bool {
	option, ok := selectedGenerationOption(a.generationConfig)
	return ok && a.ready && a.repo != nil && a.repo.ID == s.id && a.repoSeq == s.repoSeq &&
		a.statusSeq == s.statusSeq && a.draftRevision == s.draftRevision &&
		option.Selection == s.selection && a.generationConfig.Language == s.language &&
		a.generationSource(option) == s.source && a.commitSummary == s.summary &&
		a.commitDescription == s.description && slices.Equal(a.includedPaths(), s.paths)
}

func generationRPCCode(err error) int {
	var rpc *backend.RPCError
	if errors.As(err, &rpc) {
		return rpc.Code
	}
	return 0
}

func (a *app) composerLocked() bool {
	if a.savingVersion || a.generating {
		return true
	}
	if a.core != nil {
		// Preparing and retained recovery still own the original commit input,
		// even when the ordinary committing/generating flags are false.
		vm := a.nativeCommitView()
		return vm != nil && vm.Assisted.Busy
	}
	return false
}

func (a *app) generationActionEnabled() bool {
	if a.core != nil {
		_, ok := a.coreReference()
		settings := a.coreGenerationSettings()
		configured := a.coreSnapshot != nil && a.coreSnapshot.Selected != nil && a.coreSnapshot.Selected.GenerationAvailable
		if settings != nil && settings.Source == "custom" && !coreCustomSelectionValid(settings) {
			configured = true // Opens settings; it does not send an invalid request.
		}
		return ok && configured && !a.composerLocked() && !a.generationSettingsBusy &&
			!a.opening && !a.loadingStatus && a.statusErr == nil && len(a.included) > 0
	}
	if !a.ready || !a.generationCapable || a.savingVersion || a.opening || a.generationSettingsBusy {
		return false
	}
	if _, ok := selectedGenerationOption(a.generationConfig); !ok {
		// An unconfigured action opens real settings instead of pretending to
		// generate or silently choosing the first newly discovered model.
		return true
	}
	return a.repo != nil && a.status != nil && !a.loadingStatus && a.statusErr == nil && len(a.includedPaths()) > 0
}

func (a *app) requestGeneration() {
	if a.core != nil {
		if settings := a.coreGenerationSettings(); settings != nil && settings.Source == "custom" && !coreCustomSelectionValid(settings) {
			a.showSettings(settingsProviders)
			return
		}
		a.generateCoreMessage()
		return
	}
	if a.generating || a.generationConsentOpen || !a.generationActionEnabled() {
		return
	}
	option, ok := selectedGenerationOption(a.generationConfig)
	if !ok {
		a.showSettings(settingsProviders)
		return
	}
	snapshot := &generationSnapshot{
		repoSeq: a.repoSeq, statusSeq: a.statusSeq, draftRevision: a.draftRevision,
		id: a.repo.ID, paths: a.includedPaths(), selection: option.Selection,
		language: a.generationConfig.Language, summary: a.commitSummary, description: a.commitDescription,
		source: a.generationSource(option), sourceName: option.SourceName, modelName: option.ModelName,
	}
	a.generationErr, a.generationNotice = nil, ""
	// The service checks persistent source authorization before reading/sending
	// any diff. An unauthorized false request returns -32020 without network.
	a.startGeneration(snapshot, false)
}

func (a *app) confirmGeneration() {
	snapshot := a.pendingGeneration
	a.pendingGeneration, a.generationConsentOpen = nil, false
	if snapshot == nil || !snapshot.current(a) || !a.generationActionEnabled() {
		a.generationNotice = generationChanged
		return
	}
	a.startGeneration(snapshot, true)
}

func (a *app) startGeneration(snapshot *generationSnapshot, consent bool) {
	a.generationSeq++
	seq := a.generationSeq
	a.generating, a.activeGeneration, a.generationErr, a.generationNotice = true, snapshot, nil, ""
	parent := a.repoContext
	if parent == nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithCancel(parent)
	a.generationCancel = cancel
	b, update := a.generation, a.update
	go func() {
		defer cancel()
		// true is supplied only by confirmGeneration. The service owns source
		// authorization, diff gathering, SDK use and the configured deadline.
		message, err := b.GenerateMessage(ctx, snapshot.id, snapshot.paths, consent)
		update(func() {
			if a.closed || seq != a.generationSeq {
				return
			}
			a.generating, a.activeGeneration, a.generationCancel = false, nil, nil
			if !snapshot.current(a) {
				a.generationNotice = generationChanged
				return
			}
			if !consent && generationRPCCode(err) == -32020 {
				a.pendingGeneration, a.generationConsentOpen = snapshot, true
				return
			}
			if err == nil && (message.Selection != snapshot.selection || message.Language != snapshot.language || strings.TrimSpace(message.Title) == "") {
				err = errGenerationReply
			}
			a.generationErr = err
			if err != nil {
				if code := generationRPCCode(err); code == -32022 || code == -32024 {
					// Refresh only configuration, not generation. A revoked model
					// must require another explicit choice and never a fallback.
					a.loadGenerationSettings()
				}
				return
			}
			// A reply only fills an editable draft. It never stages, commits,
			// clears selected files, pushes, or erases an unconfirmed Git error.
			a.commitSummary, a.commitDescription = message.Title, message.Description
			a.descriptionOpen = a.descriptionOpen || message.Description != ""
			a.draftRevision++
			a.generationNotice, a.lastSavedSHA = generationFilled, ""
		})
	}()
}

func (a *app) invalidateGeneration(notice string) {
	pending := a.generating || a.pendingGeneration != nil
	if a.generationCancel != nil {
		// Client sends the cancellation notification; a late uninterruptible
		// backend reply is also rejected by the sequence and input snapshot.
		a.generationCancel()
	}
	a.generationSeq++
	a.generating, a.generationCancel, a.activeGeneration = false, nil, nil
	a.pendingGeneration, a.generationConsentOpen = nil, false
	if pending {
		a.generationNotice = notice
	}
}

func (a *app) stopGeneration() {
	if a.core != nil {
		a.stopCoreGeneration()
		return
	}
	a.invalidateGeneration(generationStopped)
}

func (a *app) generationDraftEdited() {
	if a.core != nil {
		a.persistCoreDraft()
		return
	}
	a.draftRevision++
	a.invalidateGeneration(generationChanged)
	if a.generationNotice == generationFilled {
		a.generationNotice = ""
	}
}

func (a *app) loadGenerationSettings() {
	if a.core != nil {
		a.changeCoreGenerationSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
			return core.State(ctx)
		}, nil)
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.GenerationSettings(ctx)
	}, nil)
}

// Serialize settings operations in the UI and apply only their service receipt.
// The service serializes storage too; there is no optimistic checkbox/selection
// state that could survive a failed save or overwrite a newer receipt.
func (a *app) changeGenerationSettings(call func(context.Context, backend.GenerationBackend) (backend.GenerationSettings, error), after func(backend.GenerationSettings)) {
	if a.core != nil || !a.ready || !a.generationCapable || a.generationSettingsBusy || a.copilotLoading || a.savingVersion {
		return
	}
	a.invalidateGeneration(generationChanged)
	a.generationSettingsSeq++
	seq := a.generationSettingsSeq
	a.generationSettingsBusy, a.generationSettingsErr = true, nil
	ctx, cancel := context.WithCancel(a.ctx)
	a.generationSettingsCancel = cancel
	b, update := a.generation, a.update
	go func() {
		defer cancel()
		settings, err := call(ctx, b)
		received := err == nil
		if received && settings.Selection != nil {
			if _, valid := selectedGenerationOption(&settings); !valid {
				// A disappearing/disabled model must be cleared at the service,
				// never replaced with the first option by this UI.
				cleared, clearErr := b.SetGenerationModel(ctx, nil)
				if clearErr == nil {
					settings = cleared
				}
				err = clearErr
			}
		}
		update(func() {
			if a.closed || seq != a.generationSettingsSeq {
				return
			}
			a.generationSettingsBusy, a.generationSettingsCancel, a.generationSettingsErr = false, nil, err
			if received {
				a.generationConfig = &settings
				if !a.generationLanguageCustom {
					a.generationLanguageDraft = settings.Language
				}
				if after != nil && err == nil {
					after(settings)
				}
				if err == nil && a.settingsModal && a.settingsPage == settingsCopilot && a.copilotStatus == nil && a.copilotErr == nil {
					a.loadCopilotAccounts()
				}
			}
		})
	}()
}

func (a *app) chooseGenerationModel(selection *backend.ModelSelection) {
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.SetGenerationModel(ctx, selection)
	}, nil)
}

func canonicalGenerationLanguage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "auto" {
		return value, nil // The original kernel default, independent of UI locale.
	}
	// x/text accepts underscores as locale aliases, but the input promised
	// here is a BCP 47 tag. Service validation still owns the accepted value.
	for _, r := range value {
		if r != '-' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return "", errGenerationLanguage
		}
	}
	if value == "" {
		return "", errGenerationLanguage
	}
	tag, err := language.Parse(value)
	if err != nil {
		return "", errGenerationLanguage
	}
	return tag.String(), nil
}

func (a *app) chooseGenerationLanguage(value string) {
	canonical, err := canonicalGenerationLanguage(value)
	a.generationLanguageErr = err
	if err != nil {
		return
	}
	if a.core != nil {
		a.changeCoreGenerationSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
			return core.SetCommitGenerationLanguage(ctx, canonical)
		}, func(settings backend.GenerationSettings) {
			a.generationLanguageDraft = settings.Language
			a.generationLanguageCustom = settings.Language != "auto" && settings.Language != "en" && settings.Language != "zh-Hans" && settings.Language != "zh-Hant"
		})
		return
	}
	a.changeGenerationSettings(func(ctx context.Context, b backend.GenerationBackend) (backend.GenerationSettings, error) {
		return b.SetGenerationLanguage(ctx, canonical)
	}, func(settings backend.GenerationSettings) {
		a.generationLanguageDraft = settings.Language
		a.generationLanguageCustom = settings.Language != "en" && settings.Language != "zh-Hans"
	})
}

func (a *app) generationServiceStopped(err error) {
	wasGenerating := a.generating
	a.invalidateGeneration("")
	if wasGenerating {
		a.generationErr = err
	}
	if a.generationSettingsCancel != nil {
		a.generationSettingsCancel()
	}
	a.generationSettingsSeq++
	a.generationSettingsBusy = false
	a.cancelCopilotLogin()
	if a.copilotLoadCancel != nil {
		a.copilotLoadCancel()
	}
	a.copilotLoadSeq++
	a.copilotLoading = false
}
