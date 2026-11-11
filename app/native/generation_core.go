package main

import (
	"context"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

// Production generation reads only the original AppStore snapshot. In
// particular it never installs a prototype GenerationBackend/CopilotGateway.
func (a *app) coreGenerationSettings() *backend.KernelGenerationSettings {
	if a.coreSnapshot == nil {
		return nil
	}
	return a.coreSnapshot.CommitGeneration
}

func (a *app) generationProviders() []backend.Provider {
	if a.core != nil {
		if settings := a.coreGenerationSettings(); settings != nil {
			return settings.Providers
		}
		return nil
	}
	if a.generationConfig != nil {
		return a.generationConfig.Providers
	}
	return nil
}

func (a *app) providerConfigurationReady() bool {
	if a.core != nil {
		return a.coreGenerationSettings() != nil
	}
	return a.generationCapable && a.generationConfig != nil
}

func coreEnabledModels(settings *backend.KernelGenerationSettings) []backend.KernelEnabledProviderModel {
	if settings == nil {
		return nil
	}
	var models []backend.KernelEnabledProviderModel
	for _, option := range settings.EnabledModels {
		for _, provider := range settings.Providers {
			if provider.ID != option.ProviderID {
				continue
			}
			for _, model := range provider.Models {
				if model.ID == option.ModelID && model.Enabled && model.Available {
					models = append(models, option)
				}
			}
		}
	}
	return models
}

func coreCustomSelectionValid(settings *backend.KernelGenerationSettings) bool {
	if settings == nil || settings.Selection == nil {
		return false
	}
	for _, model := range coreEnabledModels(settings) {
		if model.KernelProviderSelection == *settings.Selection {
			return true
		}
	}
	return false
}

// Settings mutations are serialized by the UI busy state and by ProviderStore
// in the original renderer. Their completion applies the original snapshot;
// the callback receipt only drives editor state, never a second provider cache.
func (a *app) changeCoreGenerationSettings(call func(context.Context, *backend.Kernel) (backend.KernelResult, error), after func(backend.GenerationSettings)) {
	if a.core == nil || a.closed || !a.ready || a.generationSettingsBusy || a.composerLocked() || a.corePending > 0 {
		return
	}
	a.generationSettingsSeq++
	seq := a.generationSettingsSeq
	a.generationSettingsBusy, a.generationSettingsErr = true, nil
	ctx, cancel := context.WithCancel(a.ctx)
	a.generationSettingsCancel = cancel
	a.corePending++
	core, update := a.core, a.update
	go func() {
		defer cancel()
		result, err := call(ctx, core)
		update(func() {
			if a.closed {
				return
			}
			a.corePending--
			if seq != a.generationSettingsSeq {
				return
			}
			a.generationSettingsBusy, a.generationSettingsCancel, a.generationSettingsErr = false, nil, err
			if err != nil {
				return
			}
			a.applyCoreSnapshot(result.Snapshot)
			if settings := a.coreGenerationSettings(); settings != nil {
				if !a.generationLanguageCustom {
					a.generationLanguageDraft = settings.Language
				}
				if after != nil {
					after(backend.GenerationSettings{Providers: settings.Providers, Language: settings.Language})
				}
			}
		})
	}()
}

// An explicit receiver split keeps prototype tests usable without allowing a
// production kernel launch to invoke any of its service/account methods.
func (a *app) changeProviderSettings(native func(context.Context, *backend.Kernel) (backend.KernelResult, error), research func(context.Context, backend.GenerationBackend) (backend.GenerationSettings, error), after func(backend.GenerationSettings)) {
	if a.core != nil {
		a.changeCoreGenerationSettings(native, after)
	} else {
		a.changeGenerationSettings(research, after)
	}
}

// Navigate to original account/model settings and leave the native provider
// editor first, so its modal cannot obscure the original popup stack.
func (a *app) openCoreCopilotSettings() {
	if a.core == nil || !a.ready || a.composerLocked() || a.generationSettingsBusy {
		return
	}
	a.closeSettings()
	core, ctx := a.core, a.ctx
	a.runCoreTask(func() (backend.KernelResult, error) {
		return core.OpenGenerationCopilotSettings(ctx)
	}, func(_ backend.KernelResult, err error) { a.generationSettingsErr = err })
}

type coreGenerationChoice struct {
	source    string
	selection backend.KernelProviderSelection
}

// Focused review keeps the exact source/language visible in one native opener.
// Its popover reuses the production controls; generation/save remain direct.
func (a *app) coreGenerationMenu(c *ui.Context, tx locales.Messages) {
	name, language := tx.CopilotSettings(), tx.GenerationLanguageAutomatic()
	if settings := a.coreGenerationSettings(); settings != nil {
		if settings.Source == "custom" {
			name = tx.ChooseGenerationModel()
			for _, model := range coreEnabledModels(settings) {
				if settings.Selection != nil && model.KernelProviderSelection == *settings.Selection {
					name = model.ProviderName + " · " + model.ModelName
				}
			}
		}
		if settings.Language != "auto" {
			language = i18n.Autonym(settings.Language)
		}
	}
	caption := name + " · " + language
	anchor := ui.ButtonBase(c.Key("generation-settings-summary")).Height(36).Padding(6, 8).Gap(6).Grow(1).MinWidth(0).
		Border(1, c.Theme().Border).Background(c.Theme().Background).
		Label(tx.GenerationModel() + " / " + tx.GenerationLanguage()).Value(caption).Tooltip(caption)
	quietSurface(c, anchor).Children(func() {
		ui.Text(c, tx.GenerationModel()).FontSize(c.Theme().Rem(14.0 / 15)).NoWrap().Shrink(0).TextColor(c.Theme().TextMuted)
		ui.Text(c, caption).FontSize(c.Theme().Rem(14.0 / 15)).SingleLine().Grow(1).MinWidth(0).TextColor(c.Theme().Text)
		ui.Icon(c, icons["chevron-down"]).FontSize(14).Shrink(0).TextColor(c.Theme().TextMuted)
	}).OnClick(func() { a.coreGenerationChoicesOpen = !a.coreGenerationChoicesOpen })
	ui.Popover(c.Key("generation-settings-popover"), anchor, &a.coreGenerationChoicesOpen, func() { a.coreGenerationChoices(c, tx) }).Width(520).MaxWidthPercent(94).Padding(16).Gap(12)
}

func (a *app) coreGenerationChoices(c *ui.Context, tx locales.Messages) {
	settings := a.coreGenerationSettings()
	if settings != nil && !a.generationLanguageCustom && settings.Language != "auto" && settings.Language != "en" && settings.Language != "zh-Hans" && settings.Language != "zh-Hant" {
		// Restore the saved custom tag into its editor once. Subsequent frames
		// must not overwrite an unsaved native text-input edit.
		a.generationLanguageCustom, a.generationLanguageDraft = true, settings.Language
	}
	locked := a.configurationLocked()
	ui.Row(c.Key("kernel-generation-choices")).Gap(16).AlignItems(ui.Stretch).Children(func() {
		ui.Field(c, tx.GenerationModel(), func() {
			if settings == nil {
				ui.Text(c, tx.GenerationUnavailable()).TextColor(c.Theme().TextMuted)
				return
			}
			value := coreGenerationChoice{source: settings.Source}
			if settings.Source == "custom" && coreCustomSelectionValid(settings) {
				value.selection = *settings.Selection
			}
			choices := []nativeChoice[coreGenerationChoice]{{coreGenerationChoice{source: "copilot"}, tx.CopilotSettings()}}
			for _, model := range coreEnabledModels(settings) {
				choices = append(choices, nativeChoice[coreGenerationChoice]{coreGenerationChoice{"custom", model.KernelProviderSelection}, model.ProviderName + " · " + model.ModelName})
			}
			workspaceSelect(c.Key("generation-model"), &value, tx.GenerationModel(), tx.ChooseGenerationModel(), choices, locked).FillWidth().OnChange(func() {
				var selection *backend.KernelProviderSelection
				if value.source == "custom" {
					selected := value.selection
					selection = &selected
				}
				a.changeCoreGenerationSettings(func(ctx context.Context, core *backend.Kernel) (backend.KernelResult, error) {
					return core.SelectGenerationModel(ctx, value.source, selection)
				}, nil)
			})
		}).Grow(1).Basis(0).MinWidth(0).Gap(4)
		ui.Field(c, tx.GenerationLanguage(), func() {
			value := "auto"
			if settings != nil {
				value = settings.Language
			}
			if a.generationLanguageCustom || value != "auto" && value != "en" && value != "zh-Hans" && value != "zh-Hant" {
				value = "custom"
			}
			// auto is the kernel's protocol sentinel, not "follow UI/system".
			choices := []nativeChoice[string]{{"auto", tx.GenerationLanguageAutomatic()}, {"zh-Hans", i18n.Autonym("zh-Hans")}, {"zh-Hant", i18n.Autonym("zh-Hant")}, {"en", i18n.Autonym("en")}, {"custom", tx.CustomGenerationLanguage()}}
			workspaceSelect(c.Key("generation-language"), &value, tx.GenerationLanguage(), "", choices, locked).FillWidth().OnChange(func() {
				if value == "custom" {
					a.generationLanguageCustom, a.generationLanguageErr = true, nil
					if settings != nil {
						a.generationLanguageDraft = settings.Language
					}
				} else {
					a.chooseGenerationLanguage(value)
				}
			})
		}).Width(200).Shrink(0).Gap(4)
	})
	if a.generationLanguageCustom {
		ui.Field(c, tx.GenerationLanguageTag(), func() {
			ui.Row(c).Gap(8).Children(func() {
				if ui.TextInput(c.Key("generation-language-tag"), &a.generationLanguageDraft).Grow(1).MinWidth(0).Height(36).Placeholder("fr-CA").Label(tx.GenerationLanguageTag()).Disabled(locked).Submitted() {
					a.chooseGenerationLanguage(a.generationLanguageDraft)
				}
				quietButton(c.Key("apply-generation-language"), tx.ApplyLanguage(), "").Height(36).Disabled(locked).OnClick(func() { a.chooseGenerationLanguage(a.generationLanguageDraft) })
			})
		}).FillWidth()
	}
	if a.generationLanguageErr != nil {
		ui.Text(c, tx.GenerationLanguageInvalid()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
	}
	if a.generationSettingsBusy {
		ui.Text(c, tx.GenerationSettingsWorking()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
	}
}
