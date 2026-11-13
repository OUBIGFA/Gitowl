package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// Returning to the window must not alternate an idle shortcut with background
// rule checks. Neither message belongs in the persistent composer.
func TestRuleRefreshKeepsChangesLayoutStable(t *testing.T) {
	for _, size := range [][2]int{{900, 620}, {1180, 760}, {1440, 900}} {
		for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
			for _, descriptionOpen := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/description=%v", size[0], size[1], locale, descriptionOpen), func(t *testing.T) {
					a := designEvidenceApp(t, time.Now(), "changes")
					a.coreSnapshot.Selected.CommitView = &backend.KernelCommitView{Mode: "manual", CanCommit: true}
					a.descriptionOpen = descriptionOpen
					a.commitDescription = "Preserve the draft when returning."
					tt := designEvidenceTester(a, size[0], size[1], locale, false)
					tx := locales.In(locale)
					labels := []string{tx.Changes(), tx.ChangedFiles(), tx.CommitSummary(), tx.SaveVersion()}
					before := make([]ui.Rect, len(labels))
					for i, label := range labels {
						before[i] = visibleControl(t, tt, label, size[0], size[1])
					}
					for _, route := range []string{"refresh", "history", "hidden"} {
						if route == "history" {
							a.tab = historyTab
							tt.Frame()
							a.tab = changesTab
						} else if route == "hidden" {
							tt.SetWindowHidden(true)
							tt.Advance(time.Second)
							tt.SetWindowHidden(false)
						}
						for refresh := 0; refresh < 6; refresh++ {
							pending := refresh%2 == 0
							a.coreSnapshot.Selected.CommitView.RulesPending = pending
							tt.Frame()
							checking := nativeDialogLabel(locale, "commit.checkingRules")
							if tt.HasText(checking) || tt.HasText(tx.SaveShortcut()) {
								t.Fatalf("background refresh displayed a changing composer caption: %q", tt.Texts())
							}
							for i, label := range labels {
								after := visibleControl(t, tt, label, size[0], size[1])
								if before[i] != after {
									t.Fatalf("%s, check %d: %s moved: before=%+v after=%+v", route, refresh, label, before[i], after)
								}
							}
						}
					}
				})
			}
		}
	}
}
