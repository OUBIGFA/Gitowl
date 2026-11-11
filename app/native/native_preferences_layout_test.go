package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestPreferencesReuseMeasuredGeometryAcrossFramesAndReopen(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "git", Choices: []backend.KernelDialogChoice{{ID: "git", Label: "settings.option.git"}, {ID: "appearance", Label: "settings.option.appearance"}}}}
	popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "git", Fields: []backend.KernelDialogField{{ID: "committerName", Kind: "text", LabelKey: "settings.committerName", Value: "Original"}}}, {ID: "appearance", Fields: []backend.KernelDialogField{{ID: "reference", Kind: "readonly", LabelKey: "settings.account", Text: "Account"}}}}
	popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true}, {ID: "cancel", LabelKey: "settings.cancel"}}
	measurements := 0
	sections := []nativeDialogSection{{id: "extra", label: "Extra", measureKey: "stable", measure: []func(*ui.Context, ui.Element){func(c *ui.Context, _ ui.Element) { measurements++; ui.Text(c, "Measured content") }}}}
	form := a.nativeDialogFormFor(popup)
	form.sections = sections
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 1440, 900, &replies, &closes)
	tt.Advance(time.Second)
	settled := measurements
	popup.Fields[0].Value = "appearance"
	popup.LayoutPages[0].Fields[0].Value = "Typing does not change fixed-height input geometry"
	tt.Frame()
	if measurements != settled {
		t.Fatal("settled layout still rebuilds every hidden page on tab/input changes")
	}
	// A new original popup owns a new draft, but can reuse numeric geometry.
	id := 101
	popup.ID = &id
	form = a.nativeDialogFormFor(popup)
	form.sections = sections
	tt = dialogTester(popup, form, "en", 1440, 900, &replies, &closes)
	if measurements != settled {
		t.Fatal("reopening remeasures unchanged settings")
	}
	popup.LayoutPages[1].Fields[0].Text = strings.Repeat("Long readonly account details ", 20)
	tt.Frame()
	if measurements == settled {
		t.Fatal("wrapping readonly content did not invalidate geometry")
	}
}

func TestPreferencesSwitchPagesImmediatelyWithoutInputValidation(t *testing.T) {
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "git", ChangeAction: "navigate", Choices: []backend.KernelDialogChoice{{ID: "git", Label: "settings.option.git"}, {ID: "appearance", Label: "settings.option.appearance"}}}, {ID: "committerName", Kind: "text", LabelKey: "settings.committerName", Value: "Original", ChangeAction: "change"}}
	popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "git", Fields: popup.Fields[1:]}, {ID: "appearance", Fields: []backend.KernelDialogField{{ID: "theme", Kind: "select", LabelKey: "settings.theme", Value: "light", ChangeAction: "change", Choices: []backend.KernelDialogChoice{{ID: "light", Label: "settings.option.light"}}}}}}
	popup.Actions = []backend.KernelDialogAction{{ID: "navigate", LabelKey: "settings.tab", Fields: []string{"tab"}}, {ID: "change", LabelKey: "settings.change", Fields: []string{"tab", "theme", "committerName"}}, {ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "theme", "committerName"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	form := &nativeDialogForm{}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(popup, form, "en", 1440, 900, &replies, &closes)
	if err := tt.Click(nativeDialogLabel("en", "settings.option.appearance")); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.AccessByRole(ui.RolePopUpButton, nativeDialogLabel("en", "settings.theme")); !found {
		t.Fatal("page body waits for a navigation reply")
	}
	if tt.HasText(locales.In("en").PopupCheckingFields()) {
		t.Fatal("navigation is mislabeled as checking input")
	}
	if len(replies) != 1 || replies[0].Action != "navigate" {
		t.Fatalf("navigation contract = %+v", replies)
	}
	if err := tt.Click(nativeDialogLabel("en", "settings.option.git")); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("en", "settings.committerName")); !found {
		t.Fatal("rapid navigation did not display latest page")
	}
	if len(replies) != 1 {
		t.Fatal("navigation requests overlap")
	}
	form.finishReply(true, nil)
	popup.Fields[0].Value = "appearance"
	tt.Frame()
	if len(replies) != 2 || replies[1].Values["tab"] != "git" {
		t.Fatalf("late reply lost newest target: %+v", replies)
	}
}

func TestPreferencesAppearanceIncludesLanguageWithoutSeparateInterfacePage(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = []backend.KernelDialogField{
		{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "appearance", Choices: []backend.KernelDialogChoice{{ID: "appearance", Label: "settings.option.appearance"}}},
		{ID: "theme", Kind: "select", LabelKey: "settings.theme", Value: "light", Choices: []backend.KernelDialogChoice{{ID: "light", Label: "settings.option.light"}}},
	}
	popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "theme"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	a.coreSnapshot = &backend.KernelSnapshot{CurrentPopup: popup}
	a.setLocale = func(string) {}
	tt := ui.NewTester(func(c *ui.Context) {
		setWorkspaceTheme(c)
		a.coreAuthenticationView(c, locales.In(c.Locale()))
	}, 900, 620)
	tt.SetLocale("en")
	tt.Frame()
	tx := locales.In("en")
	if _, found := tt.AccessByRole(ui.RoleButton, tx.InterfaceSettings()); found {
		t.Fatal("Interface remains a separate settings category")
	}
	if _, found := tt.AccessByRole(ui.RolePopUpButton, tx.InterfaceLanguage()); !found {
		t.Fatal("Appearance has no interface language selector")
	}
	visibleControl(t, tt, nativeDialogLabel("en", "settings.save"), 900, 620)
}

func TestPreferencesTextFieldsHaveReadableSeparation(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			for _, size := range [][2]int{{900, 620}, {1440, 900}} {
				t.Run(fmt.Sprintf("%s/%v/%v", locale, dark, size), func(t *testing.T) {
					popup := dialogFixture()
					popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
					popup.Fields = []backend.KernelDialogField{
						{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "integrations", Choices: []backend.KernelDialogChoice{{ID: "integrations", Label: "settings.option.integrations"}}},
						{ID: "customEditorPath", Kind: "text", LabelKey: "settings.customEditorPath"},
						{ID: "customEditorArguments", Kind: "text", LabelKey: "settings.customEditorArguments", Value: "%TARGET_PATH%"},
					}
					popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "customEditorPath", "customEditorArguments"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
					var replies []backend.KernelPopupReply
					var closes []int
					tt := dialogTester(popup, &nativeDialogForm{}, locale, size[0], size[1], &replies, &closes)
					tt.SetDark(dark)
					tt.Frame()
					path, ok := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "settings.customEditorPath"))
					if !ok {
						t.Fatal("path input missing")
					}
					label, ok := tt.AccessByRole(ui.RoleText, nativeDialogLabel(locale, "settings.customEditorArguments"))
					if !ok {
						t.Fatal("arguments label missing")
					}
					// The next field must be visually separate from the previous
					// input, including labels that wrap in the minimum window.
					if gap := label.Bounds.Y - path.Bounds.Y - path.Bounds.H; gap < 12 {
						t.Fatalf("next label crowds previous input: gap=%v", gap)
					}
					visibleControl(t, tt, nativeDialogLabel(locale, "settings.save"), size[0], size[1])
				})
			}
		}
	}
}

func TestPreferencesAllParametersVisibleWithoutScrollAtMinimumSize(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		popup := dialogFixture()
		popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
		popup.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "integrations", Choices: []backend.KernelDialogChoice{{ID: "integrations", Label: "settings.option.integrations"}, {ID: "appearance", Label: "settings.option.appearance"}}}}
		for _, id := range []string{"customEditorPath", "customEditorArguments", "customShellPath", "customShellArguments", "copilotAppPath", "committerName", "committerEmail", "defaultBranch"} {
			popup.Fields = append(popup.Fields, backend.KernelDialogField{ID: id, Kind: "text", LabelKey: "settings." + id})
		}
		popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "integrations", Fields: popup.Fields[1:]}, {ID: "appearance", Fields: popup.Fields[1:3]}}
		popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true}, {ID: "cancel", LabelKey: "settings.cancel"}}
		var replies []backend.KernelPopupReply
		var closes []int
		tt := dialogTester(popup, &nativeDialogForm{}, locale, 900, 620, &replies, &closes)
		if _, found := tt.AccessByRole(ui.RoleScroll, ""); found {
			t.Fatal("settings still uses scrolling")
		}
		for _, field := range popup.Fields[1:] {
			visibleControl(t, tt, nativeDialogLabel(locale, field.LabelKey), 900, 620)
		}
		visibleControl(t, tt, nativeDialogLabel(locale, "settings.save"), 900, 620)
	}
}

func TestPreferencesUseWiderWindowForEditing(t *testing.T) {
	popup := dialogFixture()
	popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
	popup.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "integrations", Choices: []backend.KernelDialogChoice{{ID: "integrations", Label: "settings.option.integrations"}, {ID: "accessibility", Label: "settings.option.accessibility"}}}}
	for _, id := range []string{"selectedExternalEditor", "useCustomEditor", "customEditorPath", "customEditorArguments", "selectedShell", "useCustomShell", "customShellPath", "customShellArguments", "copilotAppPath"} {
		popup.Fields = append(popup.Fields, backend.KernelDialogField{ID: id, Kind: "text", LabelKey: "settings." + id})
	}
	popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "integrations", Fields: popup.Fields[1:]}}
	popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true}, {ID: "cancel", LabelKey: "settings.cancel"}}
	var widths []float32
	for _, size := range [][2]int{{900, 620}, {1440, 900}} {
		var replies []backend.KernelPopupReply
		var closes []int
		tt := dialogTester(popup, &nativeDialogForm{}, "zh-Hans", size[0], size[1], &replies, &closes)
		tt.Frame()
		field, ok := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel("zh-Hans", "settings.customEditorPath"))
		if !ok {
			t.Fatal("editor path missing")
		}
		widths = append(widths, field.Bounds.W)
	}
	if widths[1] <= widths[0] {
		t.Fatalf("a wider window must offer a longer editing line: %v", widths)
	}
}

func TestPreferencesIntegrationGroupsHaveIndependentCopilotPath(t *testing.T) {
	popup := preferencesNavigationFixture()
	popup.Fields[0].Value = "integrations"
	popup.Fields[0].Choices = []backend.KernelDialogChoice{{ID: "integrations", Label: "settings.option.integrations"}}
	fields := []backend.KernelDialogField{}
	for _, id := range []string{"selectedExternalEditor", "useCustomEditor", "customEditorPath", "customEditorArguments", "selectedShell", "useCustomShell", "customShellPath", "customShellArguments", "copilotAppPath"} {
		kind := "text"
		if strings.HasPrefix(id, "useCustom") {
			kind = "checkbox"
		}
		if strings.HasPrefix(id, "selected") {
			kind = "select"
		}
		fields = append(fields, backend.KernelDialogField{ID: id, Kind: kind, LabelKey: "settings." + id})
	}
	popup.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "integrations", Fields: fields}}
	popup.Fields = popup.Fields[:1]
	popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true}, {ID: "cancel", LabelKey: "settings.cancel"}}
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			var replies []backend.KernelPopupReply
			var closes []int
			tt := dialogTester(popup, &nativeDialogForm{}, locale, 1180, 800, &replies, &closes)
			get := func(id string) ui.AccessInfo {
				f, found := tt.AccessByRole(ui.RoleTextField, nativeDialogLabel(locale, "settings."+id))
				if !found {
					t.Fatalf("missing field %s", id)
				}
				return f
			}
			editor, shell, copilot := get("customEditorPath"), get("customShellPath"), get("copilotAppPath")
			if editor.Bounds.Y != shell.Bounds.Y || copilot.Bounds.X != editor.Bounds.X || copilot.Bounds.W < editor.Bounds.W+shell.Bounds.W {
				t.Fatalf("integration sections drift or Copilot is attached to one integration: editor=%+v shell=%+v copilot=%+v", editor.Bounds, shell.Bounds, copilot.Bounds)
			}
			for _, f := range fields {
				visibleControl(t, tt, nativeDialogLabel(locale, f.LabelKey), 1180, 800)
			}
		})
	}
}

func TestPreferencesProviderStatesFitScaledText(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", locale, dark), func(t *testing.T) {
				a := newApp(nil, nil)
				defer a.close()
				a.ready, a.generationCapable = true, true
				settings := generationFixture(false)
				settings.Providers[1].Name = strings.Repeat("Long supplier 中文 ", 8)
				settings.Providers[1].Format = "azure-openai-v1"
				settings.Providers[1].Models = nil
				for i := 0; i < 30; i++ {
					settings.Providers[1].Models = append(settings.Providers[1].Models, backend.ProviderModel{ID: fmt.Sprintf("deployment-%d-%s", i, strings.Repeat("long-id-", 9)), Name: fmt.Sprintf("Model %d 中文 %s", i, strings.Repeat("long-name-", 8)), Available: true})
				}
				a.generationConfig = &settings
				popup := dialogFixture()
				popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
				popup.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "appearance", Choices: []backend.KernelDialogChoice{{ID: "appearance", Label: "settings.option.appearance"}}}}
				popup.Actions = []backend.KernelDialogAction{{ID: "save", LabelKey: "settings.save", Primary: true}, {ID: "cancel", LabelKey: "settings.cancel"}}
				a.coreSnapshot = &backend.KernelSnapshot{CurrentPopup: popup}
				a.coreSettingsSection = "providers"
				a.editProvider(settings.Providers[1].ID)
				tt := ui.NewTester(func(c *ui.Context) { setWorkspaceTheme(c); a.coreAuthenticationView(c, locales.In(c.Locale())) }, 1440, 1000)
				tt.SetLocale(locale)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{TextScale: 1.5})
				tx := locales.In(locale)
				for _, state := range []string{"connection", "error", "busy", "models", "empty-search"} {
					a.generationSettingsErr, a.generationSettingsBusy = nil, false
					switch state {
					case "error":
						a.generationSettingsErr, a.generationSettingsErrorOpen = errors.New(strings.Repeat("Connection failure with full diagnostic. ", 20)), true
					case "busy":
						a.generationSettingsBusy = true
					case "models":
						a.providerSection = providerModels
					case "empty-search":
						a.providerSearch = "no-such-deployment"
					}
					tt.Frame()
					tt.Advance(time.Second)
					if state == "models" || state == "empty-search" {
						visibleControl(t, tt, tx.SearchModels(), 1440, 1000)
						visibleControl(t, tt, tx.AddDeployment(), 1440, 1000)
						if state == "models" {
							a.providerModelList.list().ScrollIntoView(20)
							tt.Frame()
							tt.Click(tx.SearchModels())
							tt.Key(0, ui.KeyTab)
							first, last := a.providerModelList.list().Visible()
							focused := ""
							for _, model := range settings.Providers[1].Models {
								if label := tx.EnableModel(model.Name); tt.Focused(label) {
									focused = label
									break
								}
							}
							if focused == "" {
								t.Fatal("model checkbox did not receive keyboard focus")
							}
							for i := 0; i < 3; i++ {
								a.generationSettingsSeq++
								tt.Frame()
								start, end := a.providerModelList.list().Visible()
								if start != first || end != last || !tt.Focused(focused) {
									t.Fatal("hidden size measurement changed the visible model list's position or focus")
								}
							}
						}
						if state == "empty-search" && !tt.HasText(tx.NoModelMatches()) {
							t.Fatal("model empty state missing")
						}
					} else {
						for _, label := range []string{tx.ProviderName(), tx.ProviderBaseURL(), tx.ProviderSecret(), tx.SaveProvider(), tx.DeleteProvider(), tx.RenameProvider()} {
							visibleControl(t, tt, label, 1440, 1000)
						}
					}
				}
			})
		}
	}
}

// Model rows are data, not additional settings parameters. Fetching a large
// catalog must keep the settings shell at its form size and scroll the rows.
func TestProviderCatalogDoesNotEnlargeSettings(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.ready, a.generationCapable = true, true
	settings := generationFixture(false)
	settings.Providers[1].Models = nil
	a.generationConfig = &settings
	popup := preferencesNavigationFixture()
	a.coreSnapshot = &backend.KernelSnapshot{CurrentPopup: popup}
	a.coreSettingsSection = "providers"
	a.editProvider(settings.Providers[1].ID)
	a.providerSection = providerModels
	a.settingsWorkArea = func() (float32, float32) { return 3440, 1380 }
	var requested [2]float32
	a.expandSettings = func(w, h float32) { requested = [2]float32{w, h} }
	tt := ui.NewTester(func(c *ui.Context) { setWorkspaceTheme(c); a.coreAuthenticationView(c, locales.In(c.Locale())) }, 1180, 760)
	settle := func() {
		tt.Frame()
		for i := 0; i < 20; i++ {
			tt.Advance(time.Second / 60)
			if requested != [2]float32{} {
				tt.SetSize(int(requested[0]), int(requested[1]))
				requested = [2]float32{}
			}
		}
	}
	settle()
	before, ok := tt.AccessByRole(ui.RoleDialog, locales.In("en").Settings())
	if !ok {
		t.Fatal("settings shell missing")
	}
	if before.Bounds.H > 760 {
		t.Fatalf("settings data list enlarged the initial window: %+v", before.Bounds)
	}
	for i := 0; i < 80; i++ {
		settings.Providers[1].Models = append(settings.Providers[1].Models, backend.ProviderModel{ID: fmt.Sprintf("model-%02d", i), Name: fmt.Sprintf("Model %02d", i), Available: true})
	}
	a.generationSettingsSeq++
	settle()
	after, ok := tt.AccessByRole(ui.RoleDialog, locales.In("en").Settings())
	if !ok || after.Bounds.W != before.Bounds.W || after.Bounds.H > before.Bounds.H+1 {
		t.Fatalf("model count enlarged settings: before=%+v after=%+v", before.Bounds, after.Bounds)
	}
	first, last := a.providerModelList.list().Visible()
	if last-first >= 80 {
		t.Fatal("all model rows grew instead of scrolling")
	}
	a.providerModelList.list().ScrollIntoView(79)
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleCheckBox, locales.In("en").EnableModel("Model 79")); !ok {
		t.Fatal("last model is not reachable inside the list")
	}
}

func preferencesNavigationFixture() *backend.KernelPopupPresentation {
	p := dialogFixture()
	p.Type, p.TitleKey = "Preferences", "settings.title.Preferences"
	p.Fields = []backend.KernelDialogField{{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "git", ChangeAction: "navigate", Choices: []backend.KernelDialogChoice{{ID: "git", Label: "settings.option.git"}, {ID: "appearance", Label: "settings.option.appearance"}}}}
	p.LayoutPages = []backend.KernelDialogLayoutPage{{ID: "git", Fields: []backend.KernelDialogField{{ID: "committerName", Kind: "text", LabelKey: "settings.committerName", Value: "Original", ChangeAction: "change"}}}, {ID: "appearance", Fields: []backend.KernelDialogField{{ID: "theme", Kind: "select", LabelKey: "settings.theme", Value: "light", ChangeAction: "change", Choices: []backend.KernelDialogChoice{{ID: "light", Label: "settings.option.light"}}}}}}
	p.Actions = []backend.KernelDialogAction{{ID: "navigate", LabelKey: "settings.tab", Fields: []string{"tab"}}, {ID: "change", LabelKey: "settings.change", Fields: []string{"tab", "committerName", "theme"}}, {ID: "save", LabelKey: "settings.save", Primary: true, Fields: []string{"tab", "committerName", "theme"}}, {ID: "cancel", LabelKey: "settings.cancel"}}
	return p
}

func TestPreferencesSaveAdmissionWhileNavigationPending(t *testing.T) {
	p, f := preferencesNavigationFixture(), &nativeDialogForm{}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(p, f, "en", 1440, 900, &replies, &closes)
	if err := tt.Click(nativeDialogLabel("en", "settings.option.appearance")); err != nil {
		t.Fatal(err)
	}
	if !f.navigationPending || len(replies) != 1 {
		t.Fatal("navigation did not start")
	}
	save, ok := tt.AccessByRole(ui.RoleButton, nativeDialogLabel("en", "settings.save"))
	if !ok {
		t.Fatal("Save missing")
	}
	if err := tt.Click(nativeDialogLabel("en", "settings.save")); err != nil {
		t.Fatal(err)
	}
	t.Logf("save.disabled=%v replies=%d navigationPending=%v", save.Disabled, len(replies), f.navigationPending)
	if !save.Disabled && len(replies) == 1 {
		t.Fatal("enabled Save silently drops the user's click while navigation is pending")
	}
}

func TestPreferencesProviderSectionSelectableDuringInputValidation(t *testing.T) {
	p, f := preferencesNavigationFixture(), &nativeDialogForm{}
	f.sections = []nativeDialogSection{{id: "providers", label: "Custom providers", render: func(c *ui.Context) { ui.Text(c, "Provider editor") }}}
	var replies []backend.KernelPopupReply
	var closes []int
	tt := dialogTester(p, f, "en", 1440, 900, &replies, &closes)
	if err := tt.Click(nativeDialogLabel("en", "settings.committerName")); err != nil {
		t.Fatal(err)
	}
	tt.Type(" Updated")
	tt.Advance(200 * time.Millisecond)
	if !f.changePending || len(replies) != 1 {
		t.Fatal("input validation did not start")
	}
	providers, ok := tt.AccessByRole(ui.RoleButton, "Custom providers")
	if !ok {
		t.Fatal("Providers missing")
	}
	if err := tt.Click("Custom providers"); err != nil {
		t.Fatal(err)
	}
	t.Logf("providers.disabled=%v section=%q", providers.Disabled, f.section)
	if f.section != "providers" || !tt.HasText("Provider editor") {
		t.Fatal("provider navigation remains locked behind input validation")
	}
}
