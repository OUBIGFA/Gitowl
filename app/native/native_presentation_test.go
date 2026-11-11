package main

import (
	"slices"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestDescriptionStartsExpandedAndKeepsManualCollapse(t *testing.T) {
	a := newCoreApp(nil, nil)
	defer a.close()
	if !a.descriptionOpen {
		t.Fatal("new composer hides its optional description")
	}
	selected := &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 1, Path: "fixture"}}}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 1, Selected: selected})
	a.descriptionOpen = false
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 2, Selected: selected})
	if a.descriptionOpen {
		t.Fatal("snapshot overrides the user's manual collapse")
	}
}

func TestNativeGeneratedDescriptionOpensOnceAndKeepsLaterManualCollapse(t *testing.T) {
	a := newCoreApp(nil, nil)
	defer a.close()
	description := "Explain the actual selected changes"
	selected := backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 1, Path: "fixture"}}, GenerationInProgress: true}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 1, Selected: &selected})
	finished := selected
	finished.GenerationInProgress = false
	finished.Message.Summary, finished.Message.Description, finished.Message.Timestamp = "Summary", &description, 2
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 2, Selected: &finished})
	if !a.descriptionOpen || a.commitDescription != description {
		t.Fatal("the generated description remains hidden")
	}
	a.descriptionOpen = false
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 3, Selected: &finished})
	if a.descriptionOpen {
		t.Fatal("an unrelated snapshot reopened the manually collapsed description")
	}
}

func TestNativeAppearanceFollowsOriginalPreviewRollbackAndSystem(t *testing.T) {
	a := newCoreApp(nil, nil)
	defer a.close()
	var sources []string
	a.setCoreTheme = func(source string) { sources = append(sources, source) }
	var dark bool
	tt := ui.NewTester(func(c *ui.Context) { a.applyCoreAppearance(c); setWorkspaceTheme(c); dark = c.Theme().Dark }, 900, 620)
	tt.SetDark(true)
	for i, source := range []string{"light", "dark", "system", "light"} {
		a.applyCoreSnapshot(backend.KernelSnapshot{Revision: int64(i + 1), Appearance: &backend.KernelAppearance{Theme: source, TabSize: 7}})
		tt.Frame()
		if dark != (source != "light") {
			t.Fatalf("source %s: dark=%v", source, dark)
		}
	}
	if !slices.Equal(sources, []string{"light", "dark", "system", "light"}) {
		t.Fatalf("native window theme notifications = %v", sources)
	}
	// Duplicate original state must not repeatedly reset the OS theme.
	a.applyCoreSnapshot(*a.coreSnapshot)
	if len(sources) != 4 {
		t.Fatal("unchanged snapshot reset the native theme")
	}
}

func TestNativeSelectAllMenuRoleEditsFocusedText(t *testing.T) {
	a := newCoreApp(nil, nil)
	defer a.close()
	value := "preserve original text"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.TextInput(c.Key("draft"), &value).Label("Draft").AutoFocus()
		a.finishCorePresentation(c)
	}, 900, 620)
	tt.Frame()
	// Command uses the same native editing path as a MyGo application-menu role.
	tt.Command("selectAll")
	tt.Type("replacement")
	tt.Frame()
	if value != "replacement" {
		t.Fatalf("native Select All was swallowed: %q", value)
	}
}

func TestNativeGenerationDisclaimerDoesNotMisidentifyCustomProvider(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, source := range []string{"custom", "copilot"} {
			id := 14
			popup := &backend.KernelPopupPresentation{ID: &id, Type: "GenerateCommitMessageDisclaimer", Supported: true, GenerationSource: source}
			tt, _, replies, _ := popupTester(t, popup, locale)
			tx := locales.In(locale)
			if !tt.HasText(tx.PopupGenerationDisclaimerNotice()) || kernelPopupTitle(tx, popup) != tx.GenerateMessage() {
				t.Fatal("neutral review notice missing")
			}
			if tt.HasText(tx.PopupCopilotLearnMore()) != (source == "copilot") {
				t.Fatal("custom endpoint presented as GitHub Copilot")
			}
			if err := tt.Click(tx.PopupUnderstandAndGenerate()); err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			if len(*replies) != 1 || (*replies)[0].PopupID != id || (*replies)[0].Action != "submit" {
				t.Fatalf("original confirmation lost: %+v", *replies)
			}
		}
	}
}
