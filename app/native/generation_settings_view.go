package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

var providerProtocolChoices = []nativeChoice[string]{
	{"openai-responses", "OpenAI Responses"},
	{"openai-chat", "OpenAI Chat Completions"},
	{"anthropic-messages", "Anthropic Messages"},
	{"gemini", "Gemini v1"},
	{"azure-openai-v1", "Azure OpenAI v1"},
	{"ollama", "Ollama"},
}

func providerProtocolName(format string) string {
	for _, protocol := range providerProtocolChoices {
		if protocol.value == format {
			return protocol.label
		}
	}
	return format
}

func providerURLHint(tx locales.Messages, format string) (hint, placeholder string) {
	switch format {
	case "openai-responses", "openai-chat":
		return tx.ProviderURLApiRoot(), "https://api.openai.com/v1"
	case "anthropic-messages":
		return tx.ProviderURLServiceRoot(), "https://api.anthropic.com"
	case "gemini":
		return tx.ProviderURLServiceRoot(), "https://generativelanguage.googleapis.com"
	case "azure-openai-v1":
		return tx.ProviderURLAzure(), "https://resource.openai.azure.com/openai/v1"
	case "ollama":
		return tx.ProviderURLServiceRoot(), "http://localhost:11434"
	default:
		return "", ""
	}
}

// Settings use the same neutral rail/canvas hierarchy as the workspace. A wide,
// fixed native dialog separates connection editing from model management; it
// is not a growing field wall that can displace the main task's controls.
func (a *app) settingsDialog(c *ui.Context, tx locales.Messages) {
	if a.core != nil {
		return
	} // Production settings live in original Preferences.
	if ui.DialogBase(c.Key("settings-dialog"), &a.settingsModal, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Width(840).Height(552).MaxWidthPercent(94).MaxHeightPercent(90).Padding(0).Gap(0).
			Radius(12).Border(1, c.Theme().Border).Background(c.Theme().Background).Clip()
		ui.Row(c).Height(64).Shrink(0).Padding(0, 24).Gap(16).Children(func() {
			ui.Text(c, tx.Settings()).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Grow(1)
			iconButton(c.Key("close-settings"), tx.Close(), "x").OnClick(a.closeSettings)
		})
		ui.Row(c).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).
			BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Children(func() {
			ui.Column(c.Key("settings-rail")).Width(184).Shrink(0).FillHeight().Padding(16, 12).Gap(4).
				Background(c.Theme().SurfaceHover).Children(func() {
				for _, item := range []struct {
					page        int
					label, icon string
				}{{settingsInterface, tx.InterfaceSettings(), "settings"}, {settingsCopilot, tx.CopilotSettings(), "folder"}, {settingsProviders, tx.CustomProviders(), "file-code"}} {
					button := ui.ButtonBase(c.Key(item.page)).FillWidth().Height(44).Padding(8, 10).Gap(8).
						Justify(ui.Start).Label(item.label)
					quietSurface(c, button)
					if a.settingsPage == item.page {
						button.Background(c.Theme().SurfacePressed).FontWeight(500).Value(tx.CurrentPage())
					}
					button.Children(func() {
						ui.Icon(c, icons[item.icon]).FontSize(18).Shrink(0)
						ui.Text(c, item.label).NoWrap().Shrink(0)
					}).OnClick(func() { a.selectSettingsPage(item.page) })
				}
				if a.core != nil {
					ui.Spacer(c)
					quietButton(c.Key("original-preferences"), tx.PopupOpenPreferences(), "settings").FillWidth().Justify(ui.Start).
						Disabled(!a.ready || a.composerLocked()).OnClick(func() {
						a.closeSettings()
						core, ctx := a.core, a.ctx
						a.runCoreTask(func() (backend.KernelResult, error) { return core.ExecuteMenu(ctx, "preferences") }, func(_ backend.KernelResult, err error) { a.generationSettingsErr = err })
					})
				}
			})
			ui.Column(c.Key(a.settingsPage)).Grow(1).MinWidth(0).FillHeight().Padding(24).Gap(12).Children(func() {
				if a.settingsPage == settingsInterface {
					a.interfaceSettingsView(c, tx)
					return
				}
				if a.core != nil && a.settingsPage == settingsCopilot {
					// Account auth and feature-specific model choices stay on the
					// original Preferences popup, not a separate SDK account state.
					settingsTitle(c, tx.CopilotSettings())
					ui.Text(c, tx.CopilotBrowserHint()).TextColor(c.Theme().TextMuted)
					quietButton(c.Key("original-copilot-settings"), tx.OriginalCopilotSettings(), "settings").MinHeight(40).
						Disabled(!a.ready || a.composerLocked() || a.generationSettingsBusy).OnClick(a.openCoreCopilotSettings)
					if a.coreSnapshot != nil {
						for _, account := range a.coreSnapshot.Accounts {
							ui.Text(c, account.Login+" · "+account.Endpoint).SingleLine().Tooltip(account.Endpoint)
						}
					}
					return
				}
				if a.generating {
					ui.Text(c, tx.StopToConfigure()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
				}
				if a.generationSettingsErr != nil {
					ui.Text(c, tx.GenerationSettingsFailed()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
					boundedGenerationError(c.Key("settings-error"), tx, a.generationSettingsErr, &a.generationSettingsErrorOpen)
				}
				if a.generationSettingsBusy {
					ui.Text(c, tx.GenerationSettingsWorking()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
				}
				if a.settingsPage == settingsCopilot {
					a.copilotSettingsView(c, tx)
				} else {
					a.providerSettingsView(c, tx)
				}
			})
		})
	}).Label(tx.Settings()).Dismissed() {
		a.closeSettings()
	}
	a.providerDeleteDialog(c, tx)
	a.manualModelDialog(c, tx)
}

func settingsTitle(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500).Role(ui.RoleHeading).Level(2).Shrink(0)
}

func (a *app) interfaceSettingsView(c *ui.Context, tx locales.Messages) {
	settingsTitle(c, tx.InterfaceSettings())
	a.interfaceLanguageView(c, tx)
	a.interfaceVersionView(c, tx)
}

func (a *app) interfaceVersionView(c *ui.Context, tx locales.Messages) {
	version := mygo.App.Version()
	if version == "" {
		version = "0.0.1"
	}
	ui.Row(c).Gap(16).Wrap().FillWidth().Shrink(0).Children(func() {
		ui.Text(c, tx.ProductVersion(version))
		ui.Text(c, tx.BuildNumber(BuildNumber)).TextColor(c.Theme().TextMuted)
	})
}

// Interface language is a native preference, presented beside the original
// appearance draft. Its existing immediate persistence remains unchanged.
func (a *app) interfaceLanguageView(c *ui.Context, tx locales.Messages) {
	ui.Column(c).FillWidth().Gap(16).Shrink(0).Children(func() {
		ui.Field(c, tx.InterfaceLanguage(), func() {
			value := ""
			// MyGo owns both persistence and system-language resolution. Read
			// its explicit choice so system Chinese stays "Follow system".
			if a.localeOverride != nil && a.localeOverride() != "" {
				value = c.Locale()
			}
			choices := []nativeChoice[string]{{"", tx.FollowSystem()}}
			for _, tag := range locales.Catalog.Languages() {
				choices = append(choices, nativeChoice[string]{tag, i18n.Autonym(tag)})
			}
			workspaceSelect(c.Key("interface-language"), &value, tx.InterfaceLanguage(), "", choices, a.setLocale == nil).
				FillWidth().OnChange(func() { a.setLocale(value) })
		})
		a.windowBehaviorView(c, tx)
	})
}

func (a *app) providerSettingsView(c *ui.Context, tx locales.Messages) {
	if a.providerEdit != nil {
		a.providerEditorView(c, tx)
		return
	}
	settingsTitle(c, tx.CustomProviders())
	ui.Text(c, tx.ProviderDescription()).TextColor(c.Theme().TextMuted)
	if !a.providerConfigurationReady() {
		ui.Text(c, tx.GenerationUnavailable()).TextColor(c.Theme().TextMuted)
		return
	}
	quietButton(c.Key("add-provider"), tx.AddProvider(), "plus").AlignSelf(ui.Start).Disabled(a.configurationLocked()).
		OnClick(func() { a.editProvider("") })
	providers := a.generationProviders()
	if len(providers) == 0 {
		ui.Text(c, tx.NoProviders()).TextColor(c.Theme().TextMuted)
		return
	}
	ui.List(c.Key("providers"), nil, len(providers), func(i int) {
		provider := providers[i]
		button := ui.ButtonBase(c.Key(provider.ID)).MinHeight(64).FillWidth().MinWidth(0).Padding(8, 10).Gap(12).
			Label(tx.EditProvider(provider.Name)).Tooltip(provider.Name).Disabled(a.generationSettingsBusy)
		quietSurface(c, button).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, provider.Name).SingleLine().FontWeight(500)
				ui.Text(c, providerProtocolName(provider.Format)).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			})
			ui.Icon(c, icons["chevron-right"]).FontSize(18).Shrink(0).TextColor(c.Theme().TextMuted)
		}).OnClick(func() { a.editProvider(provider.ID) })
	}).Grow(1).Basis(0).MinHeight(c.Theme().Rem(10)).FillWidth().Gap(4).Label(tx.CustomProviders())
}

// Measure the actual editor with an isolated draft: size probes are invisible
// and cannot save, change credentials or share editing state with the user.
func (a *app) providerMeasureVariant() string {
	shape := fmt.Sprint(a.generationSettingsBusy, a.generationSettingsErr != nil, a.generationSettingsErrorOpen)
	if a.providerEdit != nil {
		shape += fmt.Sprint(a.providerEdit.Format, a.providerEdit.AuthKind, a.providerNeedsSecret(a.providerEdit))
	}
	for _, provider := range a.generationProviders() {
		shape += fmt.Sprint(provider.Format, len(provider.Models) == 0, len(filteredModels(provider.Models, a.providerSearch)) == 0)
	}
	return shape
}

func (a *app) providerMeasurements(tx locales.Messages) []func(*ui.Context, ui.Element) {
	var probes []func(*ui.Context, ui.Element)
	for _, mode := range []string{"new", "edit", "catalog", "models"} {
		probes = append(probes, func(c *ui.Context, probe ui.Element) {
			// ListState owns a handle and row caches. A value copy still shares
			// them with the visible list. Retain each probe's own state across
			// frames, at a stable address, including deferred layout writes.
			copy := ui.Local(probe, "provider-measurement", func() app { return app{} })
			list := copy.providerModelList
			*copy = *a
			copy.providerModelList = list
			draft := providerDraft{ProviderSettings: backend.ProviderSettings{Format: "openai-responses", AuthKind: "apiKey", RequestTimeoutSeconds: 60}}
			if a.providerEdit != nil {
				draft = *a.providerEdit
			}
			draft.secret = ""
			copy.providerEdit, copy.providerSection = &draft, providerConnection
			switch mode {
			case "new":
				draft.ID = ""
			case "edit":
				if draft.ID == "" {
					draft.ID = "measure-existing"
				}
			case "catalog":
				copy.providerEdit = nil
			case "models":
				copy.providerSection = providerModels
				if providers := a.generationProviders(); draft.ID == "" && len(providers) > 0 {
					draft.ProviderSettings = providers[0].ProviderSettings
				}
			}
			copy.providerSectionView(c, tx)
		})
	}
	return probes
}

// Visible content and every measurement variant include the same status area.
func (a *app) providerSectionView(c *ui.Context, tx locales.Messages) {
	if a.generationSettingsErr != nil {
		boundedGenerationError(c, tx, a.generationSettingsErr, &a.generationSettingsErrorOpen)
	}
	if a.generationSettingsBusy {
		ui.Text(c, tx.GenerationSettingsWorking()).Shrink(0).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
	}
	a.providerSettingsView(c, tx)
}

func (a *app) providerEditorView(c *ui.Context, tx locales.Messages) {
	draft := a.providerEdit
	ui.Row(c).Gap(12).Wrap().FillWidth().Shrink(0).Children(func() {
		quietButton(c.Key("back-providers"), tx.BackToProviders(), "arrow-left").OnClick(a.clearProviderEditor)
		ui.Spacer(c)
		for _, section := range []struct {
			page  int
			label string
		}{{providerConnection, tx.ProviderConnection()}, {providerModels, tx.ProviderModels()}} {
			button := quietButton(c.Key(section.page), section.label, "").Disabled(section.page == providerModels && draft.ID == "")
			if section.page == providerModels {
				button.Label(tx.ManageProviderModels())
				if draft.ID == "" {
					button.Tooltip(tx.SaveProviderFirst()).Description(tx.SaveProviderFirst())
				}
			}
			if a.providerSection == section.page {
				button.Background(c.Theme().SurfacePressed).FontWeight(500)
			}
			button.OnClick(func() { a.providerSection = section.page })
		}
	})
	if a.providerSection == providerModels {
		provider := a.providerByID(draft.ID)
		if provider != nil {
			a.providerModelsView(c, tx, *provider)
		}
		return
	}
	locked := a.configurationLocked()
	ui.Column(c.Key(a.providerEditEpoch)).Shrink(0).FillWidth().Gap(16).Children(func() {
		// Pair the short identity fields so the required credential remains
		// visible at minimum height; long endpoint and secret fields stay full width.
		ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
			ui.Field(c, tx.RequiredField(tx.ProviderName()), func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c.Key("provider-name"), &draft.Name).Grow(1).MinWidth(0).MinHeight(40).Label(tx.ProviderName()).Disabled(locked)
					if draft.ID != "" {
						provider := a.providerByID(draft.ID)
						quietButton(c.Key("rename-provider"), tx.RenameProvider(), "").MinHeight(40).
							Disabled(locked || provider == nil || strings.TrimSpace(draft.Name) == "" || draft.Name == provider.Name).OnClick(a.renameProvider)
					}
				})
			}).Grow(1).MinWidth(0)
			ui.Field(c, tx.ProviderFormat(), func() {
				workspaceSelect(c.Key("provider-format"), &draft.Format, tx.ProviderFormat(), "", providerProtocolChoices, locked).FillWidth().OnChange(func() {
					draft.secret, a.providerInputErr = "", nil
					a.providerSecretEpoch++
					if draft.Format == "gemini" {
						draft.AuthKind = "apiKey"
					}
				})
			}).Grow(1).MinWidth(0)
		})
		ui.Field(c, tx.RequiredField(tx.ProviderBaseURL()), func() {
			hint, placeholder := providerURLHint(tx, draft.Format)
			origin := providerOrigin(draft.BaseURL)
			ui.TextInput(c.Key("provider-base-url"), &draft.BaseURL).FillWidth().MinHeight(40).Label(tx.ProviderBaseURL()).
				Placeholder(placeholder).Disabled(locked).OnChange(func() {
				if providerOrigin(draft.BaseURL) != origin {
					draft.secret = ""
					a.providerSecretEpoch++
				}
				a.providerInputErr = nil
			})
			ui.Text(c, hint).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		})
		ui.Row(c).Gap(16).AlignItems(ui.Stretch).Children(func() {
			ui.Field(c, tx.ProviderAuthKind(), func() {
				choices := []nativeChoice[string]{{"apiKey", tx.ProviderAPIKey()}}
				if draft.Format != "gemini" {
					choices = append(choices, nativeChoice[string]{"bearer", tx.ProviderBearer()}, nativeChoice[string]{"none", tx.ProviderNoAuth()})
				}
				workspaceSelect(c.Key("provider-auth"), &draft.AuthKind, tx.ProviderAuthKind(), "", choices, locked).FillWidth().OnChange(func() {
					draft.secret, a.providerInputErr = "", nil
					a.providerSecretEpoch++
				})
			}).Grow(1).MinWidth(0)
			ui.Field(c, tx.ProviderTimeout(), func() {
				ui.NumberInput(c.Key("provider-timeout"), &draft.RequestTimeoutSeconds, 0.1, 2147483.647, 0.1).
					FillWidth().MinHeight(40).Label(tx.ProviderTimeout()).Disabled(locked)
			}).Grow(1).MinWidth(0)
		})
		ui.Field(c, tx.ProviderSecret(), func() {
			ui.TextInput(c.Key(a.providerSecretEpoch), &draft.secret).Password().FillWidth().MinHeight(40).
				Label(tx.ProviderSecret()).Disabled(locked || draft.AuthKind == "none").OnChange(func() { a.providerInputErr = nil })
			hint := tx.ProviderSecretRetain()
			if a.providerNeedsSecret(draft) {
				hint = tx.ProviderSecretRequired()
			} else if draft.AuthKind == "none" {
				hint = tx.ProviderNoAuth()
			}
			color := c.Theme().TextMuted
			if a.providerInputErr != nil {
				color = c.Theme().Danger
			}
			ui.Text(c, hint).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(color)
		})
	})
	ui.Row(c).Gap(8).Shrink(0).Margin(4, 0, 0, 0).Children(func() {
		if draft.ID != "" {
			quietButton(c.Key("delete-provider"), tx.DeleteProvider(), "x").TextColor(c.Theme().Danger).Disabled(locked).
				OnClick(func() { a.providerDeleteOpen = true })
		}
		ui.Spacer(c)
		ui.PrimaryButton(c.Key("save-provider"), tx.SaveProvider()).MinHeight(40).Padding(8, 20).FontWeight(500).
			Disabled(!a.canSaveProvider()).OnClick(a.saveProviderSettings)
	})
}

func (a *app) providerModelsView(c *ui.Context, tx locales.Messages, provider backend.Provider) {
	ui.Text(c, provider.Name).SingleLine().Tooltip(provider.Name).FontSize(c.Theme().Rem(20.0 / 15)).FontWeight(500)
	azure := provider.Format == "azure-openai-v1"
	if azure {
		ui.Text(c, tx.AzureDeploymentHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	} else {
		ui.Text(c, tx.ModelSelectionHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
	ui.Row(c).Gap(8).Children(func() {
		if !azure {
			quietButton(c.Key("fetch-provider-models"), tx.FetchModels(), "refresh-cw").Disabled(a.configurationLocked()).OnClick(a.refreshProviderModels)
		}
		label := tx.ManuallyAddModel()
		if azure {
			label = tx.AddDeployment()
		}
		quietButton(c.Key("manual-provider-model"), label, "plus").Disabled(a.configurationLocked()).OnClick(func() {
			a.manualModelOpen, a.manualModelID, a.manualModelName = true, "", ""
		})
	})
	ui.TextInput(c.Key("provider-model-search"), &a.providerSearch).FillWidth().MinHeight(40).Label(tx.SearchModels()).Placeholder(tx.SearchModels())
	models := filteredModels(provider.Models, a.providerSearch)
	a.modelCatalogView(c, tx, models, a.providerModelList.list(), func(model string, enabled bool) {
		a.setProviderModelEnabled(provider.ID, model, enabled)
	})
	if len(models) == 0 {
		message := tx.NoModelMatches()
		if len(provider.Models) == 0 {
			message = tx.NoModels()
			if azure {
				message = tx.AzureDeploymentHint()
			}
		}
		ui.Text(c, message).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) modelCatalogView(c *ui.Context, tx locales.Messages, models []backend.ProviderModel, list *ui.ListState, change func(string, bool)) {
	list.Key = func(i int) any { return models[i].ID }
	list.Selected, list.Label = nil, nil
	// A zero flex basis reserves only the readable minimum during settings
	// measurement; actual rows scroll in the space left by the parameter form.
	ui.List(c.Key("models"), list, len(models), func(i int) {
		model := models[i]
		ui.Row(c).MinHeight(56).FillWidth().MinWidth(0).Padding(4).Gap(8).Children(func() {
			enabled := model.Enabled
			ui.Checkbox(c.Key("enable-model"), &enabled, "").Size(32, 32).Padding(8).Label(tx.EnableModel(model.Name)).
				Tooltip(model.Name + " · " + model.ID).Disabled(a.configurationLocked() || !model.Available && !model.Enabled).
				OnChange(func() { change(model.ID, enabled) })
			color, status := c.Theme().Text, tx.ModelEnabled()
			if !model.Available {
				color, status = c.Theme().TextMuted, tx.ModelUnavailable()
			} else if !model.Enabled {
				color, status = c.Theme().TextMuted, tx.ModelNotEnabled()
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Tooltip(model.Name + " · " + model.ID).Children(func() {
				ui.Text(c, model.Name).SingleLine().TextColor(color)
				ui.Text(c, model.ID).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			})
			ui.Text(c, status).FontSize(c.Theme().Rem(14.0 / 15)).NoWrap().Shrink(0).TextColor(c.Theme().TextMuted)
		})
	}).Grow(1).Basis(0).MinHeight(c.Theme().Rem(10)).FillWidth().Gap(2).BorderWidth(1, 0, 0, 0).BorderColor(c.Theme().Border).Label(tx.ConfiguredModels())
}

func (a *app) providerDeleteDialog(c *ui.Context, tx locales.Messages) {
	if a.providerEdit == nil {
		return
	}
	ui.Modal(c.Key("provider-delete"), &a.providerDeleteOpen, func() {
		ui.Text(c, tx.DeleteProviderTitle(a.providerEdit.Name)).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500)
		ui.Text(c, tx.DeleteProviderNotice()).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			quietButton(c, tx.Cancel(), "").OnClick(func() { a.providerDeleteOpen = false })
			quietButton(c.Key("confirm-delete-provider"), tx.DeleteProvider(), "x").TextColor(c.Theme().Danger).
				Disabled(a.configurationLocked()).OnClick(a.deleteProvider)
		})
	}).Width(500).MaxWidthPercent(90).Padding(24).Gap(16).Label(tx.DeleteProvider())
}

func (a *app) manualModelDialog(c *ui.Context, tx locales.Messages) {
	if a.providerEdit == nil {
		return
	}
	provider := a.providerByID(a.providerEdit.ID)
	if provider == nil {
		return
	}
	azure := provider.Format == "azure-openai-v1"
	title, idLabel := tx.ManuallyAddModel(), tx.ModelID()
	if azure {
		title, idLabel = tx.AddDeployment(), tx.DeploymentName()
	}
	ui.Modal(c.Key("manual-model"), &a.manualModelOpen, func() {
		ui.Text(c, title).FontSize(c.Theme().Rem(22.0 / 15)).FontWeight(500)
		if azure {
			ui.Text(c, tx.AzureDeploymentHint()).TextColor(c.Theme().TextMuted)
		}
		ui.Field(c, idLabel, func() {
			ui.TextInput(c.Key("manual-model-id"), &a.manualModelID).FillWidth().MinHeight(40).AutoFocus().Label(idLabel).Disabled(a.configurationLocked())
		})
		ui.Field(c, tx.ModelDisplayName(), func() {
			ui.TextInput(c.Key("manual-model-name"), &a.manualModelName).FillWidth().MinHeight(40).Label(tx.ModelDisplayName()).Disabled(a.configurationLocked())
		})
		ui.Text(c, tx.ModelSelectionHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			quietButton(c, tx.Cancel(), "").OnClick(func() { a.manualModelOpen = false })
			ui.PrimaryButton(c, tx.AddModel()).MinHeight(40).Padding(8, 20).FontWeight(500).
				Disabled(a.configurationLocked() || strings.TrimSpace(a.manualModelID) == "").OnClick(a.addManualProviderModel)
		})
	}).Width(500).MaxWidthPercent(90).Padding(24).Gap(16).Label(title)
}

func (a *app) copilotSettingsView(c *ui.Context, tx locales.Messages) {
	if a.copilotModelsView {
		a.copilotModelsSettingsView(c, tx)
		return
	}
	settingsTitle(c, tx.CopilotSettings())
	ui.Text(c, tx.CopilotAccessHint()).TextColor(c.Theme().TextMuted)
	if !a.generationCapable {
		ui.Text(c, tx.GenerationUnavailable()).TextColor(c.Theme().TextMuted)
		return
	}
	if a.loginBusy || a.loginFlowID != "" {
		a.copilotLoginView(c, tx)
		return
	}
	ui.Row(c).Gap(8).Children(func() {
		quietButton(c.Key("copilot-sign-in"), tx.CopilotSignIn(), "arrow-up-right").
			Disabled(a.configurationLocked() || a.copilotLoading).OnClick(a.beginCopilotLogin)
		quietButton(c.Key("copilot-refresh-accounts"), tx.CopilotRefreshAccounts(), "refresh-cw").
			Disabled(a.configurationLocked() || a.copilotLoading).OnClick(a.loadCopilotAccounts)
	})
	if a.loginErr != nil {
		summary := tx.CopilotLoginFailed()
		if errors.Is(a.loginErr, errCopilotSecureStorage) || errors.Is(a.loginErr, errCopilotAuthorizationURL) || errors.Is(a.loginErr, errCopilotInput) {
			summary = generationDiagnostic(tx, a.loginErr)
		}
		ui.Text(c, summary).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
		boundedGenerationError(c.Key("login-error"), tx, a.loginErr, &a.loginErrorOpen)
	} else if a.loginNotice != "" {
		message := tx.CopilotLoginCancelled()
		if a.loginNotice == "completed" {
			message = tx.CopilotLoginCompleted()
		}
		ui.Text(c, message).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
	}
	if a.copilotLoading {
		ui.Text(c, tx.CopilotLoading()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
	}
	if a.copilotErr != nil {
		ui.Text(c, tx.CopilotAccountsFailed()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
		boundedGenerationError(c.Key("accounts-error"), tx, a.copilotErr, &a.copilotErrorOpen)
	}
	if a.copilotStatus != nil && !errors.Is(a.loginErr, errCopilotSecureStorage) {
		message := tx.CopilotNotSignedIn()
		if a.copilotStatus.IsAuthenticated {
			message = tx.CopilotSignedIn()
		}
		ui.Text(c, message).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		if a.copilotStatus.CopilotPlan != nil {
			ui.Text(c, tx.CopilotPlan(*a.copilotStatus.CopilotPlan)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
		}
	}
	accounts := a.copilotAccounts
	a.copilotAccountList.Key = func(i int) any { return accounts[i].SelectionID }
	a.copilotAccountList.Selected, a.copilotAccountList.Label = nil, nil
	ui.List(c.Key("copilot-accounts"), &a.copilotAccountList, len(accounts), func(i int) {
		account := accounts[i]
		ui.Row(c).Height(64).MinWidth(0).Padding(4).Gap(8).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Tooltip(account.Login + " · " + account.Host).Children(func() {
				ui.Text(c, account.Login).SingleLine().FontWeight(500)
				ui.Text(c, account.Host).SingleLine().FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
			})
			quietButton(c.Key("account-models"), tx.ProviderModels(), "file-text").Label(tx.CopilotAccountModels(account.Login)).
				Disabled(a.configurationLocked()).OnClick(func() {
				a.copilotAccountID, a.copilotModelsView, a.copilotSearch = account.SelectionID, true, ""
				a.copilotModelList = nativeListState{}
			})
			iconButton(c.Key("sign-out"), tx.CopilotSignOut(account.Login), "x").Disabled(a.configurationLocked()).
				OnClick(func() { a.logoutCopilot(account.SelectionID) })
		})
	}).Grow(1).MinHeight(0).FillWidth().Gap(4).Label(tx.CopilotSettings())
	if !a.copilotLoading && a.copilotErr == nil && a.copilotStatus != nil && len(accounts) == 0 {
		ui.Text(c, tx.CopilotNoAccounts()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) copilotModelsSettingsView(c *ui.Context, tx locales.Messages) {
	quietButton(c.Key("back-accounts"), tx.BackToAccounts(), "arrow-left").AlignSelf(ui.Start).OnClick(func() { a.copilotModelsView = false })
	var models []backend.ProviderModel
	login := ""
	for _, account := range a.copilotAccounts {
		if account.SelectionID == a.copilotAccountID {
			login = account.Login
		}
	}
	settingsTitle(c, tx.CopilotAccountModels(login))
	ui.Text(c, tx.ModelSelectionHint()).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	quietButton(c.Key("fetch-copilot-models"), tx.FetchModels(), "refresh-cw").AlignSelf(ui.Start).Disabled(a.configurationLocked()).
		OnClick(func() { a.refreshCopilotModels(a.copilotAccountID) })
	if a.generationConfig != nil {
		for _, catalog := range a.generationConfig.CopilotCatalogs {
			if catalog.Account.SelectionID == a.copilotAccountID {
				models = catalog.Models
			}
		}
	}
	ui.TextInput(c.Key("copilot-model-search"), &a.copilotSearch).FillWidth().MinHeight(40).Label(tx.SearchModels()).Placeholder(tx.SearchModels())
	filtered := filteredModels(models, a.copilotSearch)
	id := a.copilotAccountID
	a.modelCatalogView(c, tx, filtered, a.copilotModelList.list(), func(model string, enabled bool) { a.setCopilotModelEnabled(id, model, enabled) })
	if len(filtered) == 0 {
		message := tx.NoModelMatches()
		if len(models) == 0 {
			message = tx.CopilotNoModels()
		}
		ui.Text(c, message).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().TextMuted)
	}
}

func (a *app) copilotLoginView(c *ui.Context, tx locales.Messages) {
	ui.Text(c, tx.CopilotBrowserHint()).TextColor(c.Theme().TextMuted)
	if a.loginStep != nil && a.loginStep.Kind == "completed" && a.loginStep.Result != nil && a.loginStep.Result.Status == "needs-account-selection" {
		ui.Text(c, tx.CopilotLoginChooseAccount()).FontWeight(500)
		accounts := a.loginStep.Result.Accounts
		a.copilotLoginAccountsList.Key = func(i int) any { return accounts[i].SelectionID }
		a.copilotLoginAccountsList.Selected, a.copilotLoginAccountsList.Label = nil, nil
		ui.List(c.Key("login-account-choices"), &a.copilotLoginAccountsList, len(accounts), func(i int) {
			account := accounts[i]
			button := ui.ButtonBase(c).Height(56).FillWidth().Padding(8, 10).Gap(12).Label(tx.CopilotUseAccount(account.Login)).Disabled(a.loginBusy)
			quietSurface(c, button).Children(func() {
				ui.Text(c, account.Login).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, account.Host).SingleLine().TextColor(c.Theme().TextMuted)
				ui.Icon(c, icons["chevron-right"]).FontSize(18).Shrink(0)
			}).OnClick(func() { a.selectCopilotLoginAccount(account.SelectionID) })
		}).Grow(1).MinHeight(0).FillWidth().Gap(4).Label(tx.CopilotLoginChooseAccount())
	} else {
		message := tx.CopilotLoginPending()
		if a.loginStep == nil {
			message = tx.CopilotLoginStart()
		}
		ui.Text(c, message).TextColor(c.Theme().TextMuted).Role(ui.RoleStatus)
		if a.loginStep != nil && a.loginStep.Kind == "open-url" && a.loginErr != nil {
			ui.Text(c, generationDiagnostic(tx, a.loginErr)).FontSize(c.Theme().Rem(14.0 / 15)).TextColor(c.Theme().Danger)
			quietButton(c.Key("open-copilot-browser"), tx.CopilotOpenBrowser(), "arrow-up-right").AlignSelf(ui.Start).
				Disabled(a.loginBusy).OnClick(a.openCopilotAuthorization)
		}
		ui.Spacer(c)
	}
	quietButton(c.Key("cancel-copilot-login"), tx.CopilotCancelLogin(), "x").AlignSelf(ui.End).OnClick(a.cancelCopilotLogin)
}
