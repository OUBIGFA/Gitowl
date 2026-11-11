package main

import (
	"fmt"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestCoreComposerActionsFitMinimumWorkspace(t *testing.T) {
	for _, tag := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(tag, func(t *testing.T) {
			a := newApp(nil, nil)
			defer a.close()
			a.core = &backend.Kernel{} // Production presentation, no kernel mutation.
			a.repo = &backend.Repository{ID: 1, Path: "C:/view-fixture"}
			a.ready = true
			for i := 0; i < 10000; i++ {
				a.included[fmt.Sprintf("file-%d.txt", i)] = true
			}
			// The minimum window also contains its project rail and canvas gutters.
			width := 900 - int(projectRailWidth) - 48
			tt := ui.NewTester(func(c *ui.Context) {
				setWorkspaceTheme(c)
				a.commitForm(c, locales.In(c.Locale()))
			}, width, 620)
			tt.SetLocale(tag)
			tx := locales.In(tag)
			for _, open := range []bool{false, true} {
				a.descriptionOpen = open
				tt.Frame()
				model := visibleControl(t, tt, tx.GenerationModel()+" / "+tx.GenerationLanguage(), width, 620)
				if model.W < 100 {
					t.Fatalf("file count squeezed the model entry: %+v", model)
				}
				for _, label := range []string{tx.IncludedFiles(10000), tx.GenerateMessage(), tx.SaveVersion()} {
					visibleControl(t, tt, label, width, 620)
				}
			}
		})
	}
}

func TestCoreGenerationChoicesRestoreSavedCustomLanguage(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core = &backend.Kernel{} // View-only; no business request is substituted or sent.
	a.ready = true
	a.coreSnapshot = &backend.KernelSnapshot{CommitGeneration: &backend.KernelGenerationSettings{Providers: []backend.Provider{}, EnabledModels: []backend.KernelEnabledProviderModel{}, Source: "custom", Language: "fr-CA"}}
	tt := ui.NewTester(func(c *ui.Context) { a.coreGenerationChoices(c, locales.In(c.Locale())) }, 900, 260)
	tt.SetLocale("en")
	tx := locales.In("en")
	field, ok := tt.AccessByRole(ui.RoleTextField, tx.GenerationLanguageTag())
	if !ok || field.Value != "fr-CA" || !a.generationLanguageCustom {
		t.Fatalf("the saved custom output language was hidden or lost: %+v", field)
	}
	tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("sr-Latn-RS")
	tt.Frame()
	if a.generationLanguageDraft != "sr-Latn-RS" || a.coreGenerationSettings().Language != "fr-CA" || a.generationConfig != nil {
		t.Fatal("a frame reset the unsaved input or changed the original language without Apply")
	}
}

func TestCoreGenerationChoicesFollowOriginalSourceNotInactiveCustomSelection(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core = &backend.Kernel{}
	a.ready = true
	selection := backend.KernelProviderSelection{ProviderID: "view-fixture", ModelID: "model"}
	a.coreSnapshot = &backend.KernelSnapshot{CommitGeneration: &backend.KernelGenerationSettings{
		Providers:     []backend.Provider{{ProviderSettings: backend.ProviderSettings{ID: selection.ProviderID, Name: "Fixture source"}, Models: []backend.ProviderModel{{ID: selection.ModelID, Name: "Fixture model", Enabled: true, Available: true}}}},
		EnabledModels: []backend.KernelEnabledProviderModel{{KernelProviderSelection: selection, ProviderName: "Fixture source", ModelName: "Fixture model"}},
		Source:        "copilot", Selection: &selection, Language: "auto",
	}}
	tt := ui.NewTester(func(c *ui.Context) { a.coreGenerationChoices(c, locales.In(c.Locale())) }, 900, 260)
	tt.SetLocale("en")
	field, ok := tt.AccessByRole(ui.RolePopUpButton, locales.In("en").GenerationModel())
	if !ok || field.Value != locales.In("en").CopilotSettings() {
		t.Fatalf("inactive metadata changed the shown original source: %+v", field)
	}
}

func TestCoreComposerLocksOriginalAssistedLifetime(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core = &backend.Kernel{}
	a.ready = true
	a.included["fixture.txt"] = true
	vm := &backend.KernelCommitView{}
	a.coreSnapshot = &backend.KernelSnapshot{Selected: &backend.KernelSelected{
		Repository:          backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 1, Path: "C:/view-fixture"}},
		GenerationAvailable: true, CommitView: vm,
	}}
	for _, kind := range []string{"preparing", "planning", "error", "closing"} {
		vm.Assisted = backend.KernelAssistedState{Kind: kind, Busy: true}
		if !a.composerLocked() || a.generationActionEnabled() {
			t.Fatalf("original %s ownership did not lock the draft and generator", kind)
		}
	}
	vm.Assisted.Busy = false
	if a.composerLocked() || !a.generationActionEnabled() {
		t.Fatal("released original ownership did not unlock the native composer")
	}
	a.core = nil
	vm.Assisted.Busy = true
	if a.composerLocked() {
		t.Fatal("research presentation was coupled to native commit metadata")
	}
	a.generating = true
	if !a.composerLocked() {
		t.Fatal("the original research generation lock was removed")
	}
}
