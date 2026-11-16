package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestNativeSaveMenuRemembersItsChoiceWithoutSaving(t *testing.T) {
	a := designEvidenceApp(t, time.Now(), "changes")
	a.coreSnapshot.Selected.CommitView = &backend.KernelCommitView{Mode: "manual", CanCommit: true}
	a.windowPreferences.CloseBehavior = closeWindowToTray
	a.windowPreferencesPath = filepath.Join(t.TempDir(), "window-preferences.json")
	tt := designEvidenceTester(a, 1180, 760, "zh-Hans", false)
	tx := locales.In("zh-Hans")
	for _, choice := range []struct {
		label string
		push  bool
	}{{tx.SaveVersionAndPush(), true}, {tx.SaveVersion(), false}} {
		if err := tt.Click(tx.SaveOptions()); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(choice.label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.savingVersion || a.corePending != 0 {
			t.Fatal("choosing the default action started a Git operation")
		}
		button, found := tt.AccessByRole(ui.RoleButton, choice.label)
		if !found || button.Disabled {
			t.Fatalf("the main button did not adopt %q: %+v", choice.label, button)
		}
		// Changing an unrelated native preference must retain the chosen action.
		a.changeWindowCloseBehavior(closeWindowExit)
		data, err := os.ReadFile(a.windowPreferencesPath)
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			PushAfterCommit bool `json:"pushAfterCommit"`
		}
		if err := json.Unmarshal(data, &saved); err != nil || saved.PushAfterCommit != choice.push {
			t.Fatalf("the selected action was not retained: %s, %v", data, err)
		}
		a.windowPreferences, err = readWindowPreferences(a.windowPreferencesPath)
		if err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if _, found := tt.AccessByRole(ui.RoleButton, choice.label); !found {
			t.Fatalf("reloading preferences lost %q", choice.label)
		}
	}
}

func TestNativeSaveChoiceWriteFailureKeepsThePreviousAction(t *testing.T) {
	a := designEvidenceApp(t, time.Now(), "changes")
	a.windowPreferences.CloseBehavior = closeWindowToTray
	a.windowPreferencesPath = t.TempDir() // A directory cannot be replaced by the preference file.
	a.selectSaveAction(true)
	if a.saveActionErr == nil || a.windowPreferences.PushAfterCommit || a.corePending != 0 {
		t.Fatal("a failed preference write changed the action or started Git")
	}
	tt := designEvidenceTester(a, 900, 620, "zh-Hans", false)
	if !tt.HasText(locales.In("zh-Hans").SaveActionPreferenceFailed()) {
		t.Fatal("the preference write failure was not visible")
	}
}

func TestNativeSaveTracksLocalSummaryWhileDraftAndRulesSettle(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			a := designEvidenceApp(t, time.Now(), "changes")
			a.commitSummary = ""
			a.coreSnapshot.Selected.CommitView = &backend.KernelCommitView{Mode: "manual", DefaultSummary: ptr("Update file.txt")}
			tt := designEvidenceTester(a, 1180, 760, locale, false)
			tx := locales.In(locale)
			check := func(disabled bool) {
				t.Helper()
				tt.Frame()
				button, found := tt.AccessByRole(ui.RoleButton, tx.SaveVersion())
				if !found || button.Disabled != disabled {
					t.Fatalf("summary=%q pending=%d: Save disabled=%v, want %v (found=%v)", a.commitSummary, a.coreDraftPending, button.Disabled, disabled, found)
				}
			}
			check(true)
			if err := tt.Click(tx.CommitSummary()); err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{"S", "a", "v", "e", " 内容"} {
				tt.Type(text)
				// The last kernel receipt can still describe the empty draft or
				// unfinished rules. Neither may flicker the local text gate.
				a.coreSnapshot.Selected.CommitView.RulesPending = true
				check(false)
			}
			a.coreSnapshot.Selected.CommitView.RulesPending = false
			a.coreSnapshot.Selected.CommitView.RulesBlocked = true
			check(false) // Submission still runs the original rule validation.
			tt.Key(ui.Ctrl, ui.KeyA)
			tt.Key(0, ui.KeyBackspace)
			check(true)
			a.commitSummary = "   "
			check(true)
			a.commitSummary = "Ready"
			a.savingVersion = true
			if a.canSaveVersion() {
				t.Fatal("an in-flight save admitted a second commit")
			}
			a.savingVersion, a.tab = false, historyTab
			if a.canSaveVersion() {
				t.Fatal("the history page admitted a composer action")
			}
		})
	}
}

func TestNativeSaveMenuFitsLanguagesThemesAndWindowSizes(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			for _, size := range [][2]int{{900, 620}, {1180, 760}, {1440, 900}} {
				t.Run(fmt.Sprintf("%s/dark=%v/%dx%d", locale, dark, size[0], size[1]), func(t *testing.T) {
					a := designEvidenceApp(t, time.Now(), "changes")
					a.coreSnapshot.Selected.CommitView = &backend.KernelCommitView{Mode: "manual", CanCommit: true}
					a.windowPreferences.CloseBehavior = closeWindowToTray
					a.windowPreferencesPath = filepath.Join(t.TempDir(), "window-preferences.json")
					tt := designEvidenceTester(a, size[0], size[1], locale, dark)
					labels := map[string][2]string{
						"en":      {"Save options", "Save version and push"},
						"zh-Hans": {"保存选项", "保存版本并推送"},
						"zh-Hant": {"儲存選項", "儲存版本並推送"},
					}
					menuLabel, actionLabel := labels[locale][0], labels[locale][1]
					visibleControl(t, tt, menuLabel, size[0], size[1])
					if err := tt.Click(menuLabel); err != nil {
						t.Fatal(err)
					}
					tt.Advance(time.Second)
					visibleControl(t, tt, actionLabel, size[0], size[1])
					panel, found := tt.AccessByRole(ui.RoleMenu, "")
					if locale != "en" && (!found || panel.Bounds.W >= 180) {
						t.Fatalf("short labels kept excess menu width: %+v", panel)
					}
					saveDesignEvidence(t, tt, fmt.Sprintf("save-menu-%s-%d-dark-%v.png", locale, size[0], dark))
					tt.Key(0, ui.KeyEscape)
					tt.Frame()
					if tt.HasText(actionLabel) {
						t.Fatal("Escape did not close the save menu")
					}
					if err := tt.Click(menuLabel); err != nil {
						t.Fatal(err)
					}
					if err := tt.ChooseMenuItem(actionLabel); err != nil {
						t.Fatal(err)
					}
					tt.Advance(time.Second)
					visibleControl(t, tt, actionLabel, size[0], size[1])
					saveDesignEvidence(t, tt, fmt.Sprintf("save-push-%s-%d-dark-%v.png", locale, size[0], dark))
					if err := tt.Click(menuLabel); err != nil {
						t.Fatal(err)
					}
					tt.Advance(time.Second)
					saveDesignEvidence(t, tt, fmt.Sprintf("save-menu-push-%s-%d-dark-%v.png", locale, size[0], dark))
					tt.Key(0, ui.KeyEscape)
					tt.Frame()
					if len(tt.Menu()) != 0 {
						t.Fatal("Escape did not close the selected-action menu")
					}
				})
			}
		}
	}
}
