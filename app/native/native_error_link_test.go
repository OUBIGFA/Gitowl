package main

import (
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestNativeErrorRecoveryLinkUsesNativeRoleAndOpaqueContinuation(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		id := 31
		popup := &backend.KernelPopupPresentation{
			ID: &id, Type: "Error", Supported: true, Title: "Copilot billing not configured",
			Content: backend.KernelErrorContent{Message: "billing setup required", ActionText: "Open GitHub Copilot settings", ActionURL: "https://github.com/settings/copilot"},
			Choices: []string{"close"},
		}
		tt, _, replies, _ := popupTester(t, popup, "zh-Hans")
		name := locales.Catalog.Format("zh-Hans", "popupOpenGitHubCopilotSettings", nil)
		link, found := tt.AccessByRole(ui.RoleLink, name)
		if !found {
			t.Fatal("billing recovery disappeared into read-only diagnostic text")
		}
		if keyboard {
			for steps := 0; steps < 12 && !link.Focused; steps++ {
				tt.Key(0, ui.KeyTab)
				link, _ = tt.AccessByRole(ui.RoleLink, name)
			}
			if !link.Focused {
				t.Fatal("native recovery link is unreachable by Tab")
			}
			tt.Key(0, ui.KeyEnter)
		} else {
			tt.ClickAt(link.Bounds.X+link.Bounds.W/2, link.Bounds.Y+link.Bounds.H/2)
		}
		if len(*replies) != 1 || (*replies)[0].Action != "openAction" || (*replies)[0].ContentIndex == nil || *(*replies)[0].ContentIndex != 0 || (*replies)[0].PopupID != id {
			t.Fatalf("link did not retain the original error's opaque action: %+v", *replies)
		}
	}
}
