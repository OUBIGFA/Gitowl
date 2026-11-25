package main

import (
	"fmt"
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestCopilotDownloadNoticeLanguagesAndCancellation(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		for _, dark := range []bool{false, true} {
			for _, phase := range []string{"downloading", "verifying", "extracting", "error"} {
				t.Run(fmt.Sprintf("%s/%v/%s", locale, dark, phase), func(t *testing.T) {
					state := &backend.KernelCopilotRuntime{Phase: phase, Received: 31 << 20, Total: 58 << 20, Error: "HTTP 503: component download unavailable"}
					clicked := false
					tt := ui.NewTester(func(c *ui.Context) {
						setWorkspaceTheme(c)
						ui.Column(c).Fill().Padding(16).Children(func() { renderCopilotRuntimeNotice(c, state, func() { clicked = true }) })
					}, 460, 240)
					tt.SetLocale(locale)
					tt.SetDark(dark)
					tt.Advance(time.Second)
					if !tt.HasText(workspaceLabel(locale, "copilotRuntime."+phase)) && phase != "downloading" {
						t.Fatal("localized phase missing")
					}
					action := "cancel"
					if phase == "error" {
						action = "dismiss"
					}
					button := workspaceLabel(locale, "copilotRuntime."+action)
					visibleControl(t, tt, button, 460, 240)
					if err := tt.Click(button); err != nil || !clicked {
						t.Fatalf("download action unavailable: %v", err)
					}
					saveDesignEvidence(t, tt, fmt.Sprintf("copilot-%s-%s-dark-%v.png", phase, locale, dark))
				})
			}
		}
	}
}

func TestCopilotDownloadCanBeCancelledInsidePreferences(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			a := newApp(nil, nil)
			defer a.close()
			popup := dialogFixture()
			popup.Type, popup.TitleKey = "Preferences", "settings.title.Preferences"
			popup.Fields = []backend.KernelDialogField{
				{ID: "tab", Kind: "select", LabelKey: "settings.tab", Value: "copilot", Choices: []backend.KernelDialogChoice{{ID: "copilot", Label: "settings.option.copilot"}}},
			}
			popup.Actions = []backend.KernelDialogAction{{ID: "cancel", LabelKey: "settings.cancel"}}
			state := &backend.KernelCopilotRuntime{Phase: "downloading", Received: 8 << 20, Total: 59 << 20}
			a.coreSnapshot = &backend.KernelSnapshot{CurrentPopup: popup, CopilotRuntime: state}
			form := a.nativeDialogFormFor(popup)
			clicked := false
			form.copilotRuntime, form.cancelCopilotDownload = state, func() { clicked = true }
			tt := ui.NewTester(func(c *ui.Context) {
				setWorkspaceTheme(c)
				renderNativeDialog(c, locales.In(c.Locale()), popup, form, nil, func(int) {})
			}, 900, 620)
			tt.SetLocale(locale)
			tt.Advance(time.Second)
			label := workspaceLabel(locale, "copilotRuntime.cancel")
			visibleControl(t, tt, label, 900, 620)
			visibleControl(t, tt, nativeDialogLabel(locale, "settings.cancel"), 900, 620)
			if err := tt.Click(label); err != nil || !clicked {
				t.Fatalf("covered download cancellation: %v", err)
			}
			saveDesignEvidence(t, tt, "copilot-preferences-"+locale+".png")
		})
	}
}
