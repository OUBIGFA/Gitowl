package main

import (
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

func TestNativeRemoteToolbarUsesOriginalMenuAvailability(t *testing.T) {
	for _, test := range []struct {
		name                  string
		parentEnabled, remote bool
		pushEnabled           bool
	}{
		{"unpublished", true, false, true},
		{"published", true, true, true},
		{"disabled ancestor", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &app{ready: true, coreSnapshot: &backend.KernelSnapshot{
				Selected: &backend.KernelSelected{HasRemote: test.remote},
				Menu: backend.KernelMenuDTO{Items: []backend.KernelMenuItem{{
					ID: "repository", Type: "submenu", Enabled: test.parentEnabled, Visible: true,
					Items: []backend.KernelMenuItem{
						{ID: "push", Enabled: true, Visible: true},
						{ID: "pull", Enabled: test.remote, Visible: true},
						{ID: "fetch", Enabled: test.remote, Visible: true},
					},
				}}},
			}}
			tt := ui.NewTester(func(c *ui.Context) {
				setWorkspaceTheme(c)
				a.coreRemoteControls(c, locales.In(c.Locale()))
			}, 900, 100)
			tt.SetLocale("en")
			tt.Frame()
			pushLabel := locales.In("en").PushChanges()
			if !test.remote {
				pushLabel = nativeDialogLabel("en", "repositoryDialog.PublishRepository")
			}
			push, ok := tt.AccessByRole(ui.RoleButton, pushLabel)
			if !ok || push.Disabled == test.pushEnabled {
				t.Fatalf("original Push/Publish availability was lost: %+v, found=%v", push, ok)
			}
			for _, label := range []string{locales.In("en").PullUpdates(), locales.In("en").FetchUpdates()} {
				button, ok := tt.AccessByRole(ui.RoleButton, label)
				if !ok || button.Disabled == (test.remote && test.parentEnabled) {
					t.Fatalf("original remote menu availability was lost for %q: %+v", label, button)
				}
			}
		})
	}
}

func TestNativeMenuActionRejectsHiddenAncestorsAndMissingIDs(t *testing.T) {
	item, found := nativeMenuAction([]backend.KernelMenuItem{{
		ID: "repository", Enabled: true, Visible: false,
		Items: []backend.KernelMenuItem{{ID: "push", Enabled: true, Visible: true}},
	}}, "push")
	if !found || item.Enabled {
		t.Fatal("toolbar bypassed the original hidden menu ancestor")
	}
	if _, found := nativeMenuAction(nil, "push"); found {
		t.Fatal("missing original actions must not be invented by the native toolbar")
	}
	// A disconnected frontend has no repository; an action must be a no-op,
	// not a nil snapshot dereference or an operation on a different repository.
	(&app{}).coreRemoteAction("push")
}
