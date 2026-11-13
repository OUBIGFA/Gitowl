package main

import (
	"errors"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

type nativeChoice[T comparable] struct {
	value T
	label string
}

// The native select base retains keyboard/overlay semantics. Only its neutral
// variant and Lucide chevron change; accessible values retain the full names.
func workspaceSelect[T comparable](c *ui.Context, value *T, label, placeholder string, choices []nativeChoice[T], disabled bool) ui.Element {
	shown := placeholder
	for _, choice := range choices {
		if choice.value == *value {
			shown = choice.label
			break
		}
	}
	selectBox := ui.SelectBase(c, value)
	trigger := selectBox.Trigger.MinHeight(40).MinWidth(0).Padding(8, 10).Gap(8).Radius(8).
		Border(1, c.Theme().Border).Background(c.Theme().Surface).Label(label).Value(shown).Disabled(disabled)
	trigger.Children(func() {
		color := c.Theme().Text
		if disabled {
			color = c.Theme().TextMuted
		}
		ui.Text(c, shown).SingleLine().Grow(1).MinWidth(0).TextColor(color)
		ui.Icon(c, icons["chevron-down"]).FontSize(16).Shrink(0).TextColor(c.Theme().TextMuted)
	})
	selectBox.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(8).Background(c.Theme().Surface).
			Border(1, c.Theme().Border)
		ui.Scroll(c).Height(min(264, float32(len(choices)*42))).FillWidth().Gap(2).Children(func() {
			for _, choice := range choices {
				item := selectBox.Item(choice.value).MinHeight(40).Padding(8, 10).Gap(8).Radius(6).
					Label(choice.label)
				if item.Highlighted() {
					item.Background(c.Theme().SurfacePressed)
				}
				item.Children(func() {
					ui.Text(c, choice.label).SingleLine().Grow(1).MinWidth(0)
					if choice.value == *value {
						ui.Icon(c, icons["check"]).FontSize(16).Shrink(0)
					}
				})
			}
		})
	})
	return trigger
}

func (a *app) generationChoices(c *ui.Context, tx locales.Messages) {
	if a.core != nil {
		a.coreGenerationChoices(c, tx)
		return
	}
	locked := a.composerLocked() || a.generationSettingsBusy || !a.ready || !a.generationCapable
	ui.Row(c.Key("generation-choices")).Gap(16).AlignItems(ui.Stretch).Children(func() {
		ui.Field(c, tx.GenerationModel(), func() {
			options := generationOptions(a.generationConfig)
			if len(options) == 0 {
				hint := tx.ModelSelectionHint()
				if !a.generationCapable {
					hint = tx.GenerationUnavailable()
				}
				button := ui.ButtonBase(c.Key("generation-model")).MinHeight(40).FillWidth().MinWidth(0).Padding(8, 10).Gap(8).
					Radius(8).Border(1, c.Theme().Border).Background(c.Theme().Surface).Label(tx.GenerationModel()).
					Tooltip(hint).Disabled(locked)
				button.Children(func() {
					ui.Text(c, tx.ConfigureModels()).SingleLine().Grow(1).MinWidth(0)
					ui.Icon(c, icons["arrow-up-right"]).FontSize(16).Shrink(0).TextColor(c.Theme().TextMuted)
				}).OnClick(func() { a.showSettings(settingsProviders) })
			} else {
				var selection backend.ModelSelection
				if option, ok := selectedGenerationOption(a.generationConfig); ok {
					selection = option.Selection
				}
				choices := make([]nativeChoice[backend.ModelSelection], 0, len(options))
				for _, option := range options {
					choices = append(choices, nativeChoice[backend.ModelSelection]{option.Selection, option.SourceName + " · " + option.ModelName})
				}
				workspaceSelect(c.Key("generation-model"), &selection, tx.GenerationModel(), tx.ChooseGenerationModel(), choices, locked).
					FillWidth().OnChange(func() { a.chooseGenerationModel(&selection) })
			}
		}).Grow(1).Basis(0).MinWidth(0).Gap(4)
		ui.Field(c, tx.GenerationLanguage(), func() {
			value := "en"
			if a.generationConfig != nil {
				value = a.generationConfig.Language
			}
			if a.generationLanguageCustom || value != "en" && value != "zh-Hans" {
				value = "custom"
			}
			choices := []nativeChoice[string]{{"zh-Hans", i18n.Autonym("zh-Hans")}, {"en", i18n.Autonym("en")}, {"custom", tx.CustomGenerationLanguage()}}
			workspaceSelect(c.Key("generation-language"), &value, tx.GenerationLanguage(), "", choices, locked).FillWidth().
				OnChange(func() {
					if value == "custom" {
						a.generationLanguageCustom, a.generationLanguageErr = true, nil
						if a.generationConfig != nil && a.generationConfig.Language != "en" && a.generationConfig.Language != "zh-Hans" {
							a.generationLanguageDraft = a.generationConfig.Language
						}
					} else {
						a.chooseGenerationLanguage(value)
					}
				})
		}).Width(200).Shrink(0).Gap(4)
	})
	if a.generationLanguageCustom || a.generationConfig != nil && a.generationConfig.Language != "en" && a.generationConfig.Language != "zh-Hans" {
		ui.Form(c.Key("generation-language-custom"), func() {
			ui.Field(c, tx.GenerationLanguageTag(), func() {
				ui.Row(c).Gap(8).Children(func() {
					if ui.TextInput(c.Key("generation-language-tag"), &a.generationLanguageDraft).Grow(1).MinWidth(0).MinHeight(36).
						Placeholder("fr-CA").Label(tx.GenerationLanguageTag()).Disabled(locked).Submitted() {
						a.chooseGenerationLanguage(a.generationLanguageDraft)
					}
					quietButton(c.Key("apply-generation-language"), tx.ApplyLanguage(), "").MinHeight(36).Disabled(locked).
						OnClick(func() { a.chooseGenerationLanguage(a.generationLanguageDraft) })
				})
			})
		})
	}
	if a.generationLanguageErr != nil {
		ui.Text(c, tx.GenerationLanguageInvalid()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
	}
	if !a.generationCapable {
		ui.Text(c, tx.GenerationUnavailable()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	} else if a.generationSettingsBusy {
		ui.Text(c, tx.GenerationSettingsWorking()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
	}
}

func (a *app) generationErrors(c *ui.Context, tx locales.Messages) {
	// User-initiated outcomes belong with generation feedback, never in the
	// persistent file-count caption. Background refreshes have no notice here.
	if a.generationErr == nil {
		message := ""
		switch a.generationNotice {
		case generationStopped:
			message = tx.GenerationStopped()
		case generationChanged:
			message = tx.GenerationChanged()
		case generationFilled:
			message = tx.GenerationFilled()
		}
		if message != "" {
			ui.Text(c, message).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
		}
	}
	if a.generationErr != nil {
		summary := tx.GenerationFailed()
		if generationRPCCode(a.generationErr) == -32022 {
			summary = tx.ChooseGenerationModel()
		} else if generationRPCCode(a.generationErr) == -32024 {
			summary = tx.GenerationChanged()
		}
		ui.Text(c, summary).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
		ui.Text(c, tx.GenerationNoRetry()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		boundedGenerationError(c.Key("generation-error"), tx, a.generationErr, &a.generationErrorOpen)
	}
	if a.generationSettingsErr != nil {
		ui.Text(c, tx.GenerationSettingsFailed()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
		boundedGenerationError(c.Key("generation-settings-error"), tx, a.generationSettingsErr, &a.generationSettingsErrorOpen)
	}
}

func generationDiagnostic(tx locales.Messages, err error) string {
	switch {
	case errors.Is(err, errGenerationLanguage):
		return tx.GenerationLanguageInvalid()
	case errors.Is(err, errProviderSecretRequired):
		return tx.ProviderSecretRequired()
	case errors.Is(err, errCopilotAuthorizationURL):
		return tx.CopilotUnsafeURL()
	case errors.Is(err, errCopilotSecureStorage):
		return tx.CopilotStorageUnavailable()
	case errors.Is(err, errCopilotInput), errors.Is(err, errCopilotLoginStep):
		return tx.CopilotUnsupportedStep()
	case errors.Is(err, errCopilotBrowser):
		return tx.CopilotBrowserFailed()
	case errors.Is(err, errGenerationReply):
		return tx.GenerationChanged()
	default:
		// RPC errors are projected by the native service to safe diagnostics.
		// Credentials, raw SDK responses and the selected diff are not in them.
		return err.Error()
	}
}

func boundedGenerationError(c *ui.Context, tx locales.Messages, err error, open *bool) {
	ui.Column(c).Gap(4).FillWidth().Shrink(0).Children(func() {
		details := ui.CollapsibleBase(c.Key("error-disclosure"), open)
		quietSurface(c, details.Trigger).MinHeight(28).Padding(2, 8).Gap(8).AlignSelf(ui.Start).
			Label(tx.GenerationErrorDetails()).Children(func() {
			ui.Icon(c, icons["chevron-right"]).FontSize(18).Rotate(90 * details.Progress())
			ui.Text(c, tx.GenerationErrorDetails()).FontSize(c.Theme().Rem(14.0 / 15)).NoWrap()
		})
		details.Panel(func() {
			message := generationDiagnostic(tx, err)
			field := ui.TextArea(c.Key("full-error"), &message).ReadOnly(true).FillWidth().Height(64).MinHeight(64).MaxHeight(64).
				FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Text).Label(tx.GenerationFullErrorDetails())
			shown := ui.Local(field, "shown-diagnostic", func() string { return "" })
			if *shown != message {
				field.SetTextSelection(0, 0)
				*shown = message
			}
		})
	})
}

func (a *app) generationConsentDialog(c *ui.Context, tx locales.Messages) {
	if ui.Modal(c.Key("generation-consent"), &a.generationConsentOpen, func() {
		ui.Text(c, tx.SendingDiffTitle()).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500)
		if snapshot := a.pendingGeneration; snapshot != nil {
			ui.Text(c, tx.SendingDiffNotice(len(snapshot.paths), snapshot.sourceName))
			ui.Text(c, snapshot.modelName).SingleLine().Tooltip(snapshot.modelName).FontWeight(500)
			ui.Text(c, tx.GenerationDestination(snapshot.source.baseURL)).TextColor(c.Theme().TextMuted)
		}
		ui.Text(c, tx.GenerationOnly()).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			quietButton(c.Key("cancel-generation-consent"), tx.Cancel(), "").OnClick(func() {
				a.pendingGeneration, a.generationConsentOpen = nil, false
			})
			ui.PrimaryButton(c.Key("confirm-generation-consent"), tx.SendAndGenerate()).MinHeight(40).Padding(8, 20).FontWeight(500).
				OnClick(a.confirmGeneration)
		})
	}).Width(560).MaxWidthPercent(90).Padding(24).Gap(16).Label(tx.SendingDiffTitle()).Dismissed() {
		a.pendingGeneration = nil
	}
}
