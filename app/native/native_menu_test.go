package main

import (
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo"
)

func TestNativeMenuPreservesRolesShortcutsAndOriginalIDs(t *testing.T) {
	var clicks []string
	var zooms []string
	items := []backend.KernelMenuItem{
		{ID: "file", Type: "submenu", LabelKey: "menu.file", Enabled: true, Visible: true, Items: []backend.KernelMenuItem{
			{ID: "clone-repository", Type: "normal", LabelKey: "menu.clone-repository", Enabled: true, Visible: true, Accelerator: "CmdOrCtrl+Shift+O"},
			{ID: "copy", Type: "normal", LabelKey: "menu.copy", Role: "copy", Enabled: true, Visible: true},
			{ID: "zoom", Type: "normal", LabelKey: "menu.zoomIn", Role: "zoomIn", Enabled: true, Visible: true, Accelerator: "CmdOrCtrl+="},
			{ID: "check", Type: "checkbox", LabelKey: "menu.view", Enabled: false, Visible: true, Checked: true},
			{ID: "sep", Type: "separator", Visible: true},
		}},
	}
	menu := mygo.NewMenu(nativeMenuItems(items, "en", true, func(id string) { clicks = append(clicks, id) }, func(action string) { zooms = append(zooms, action) }))
	clone := menu.ItemByID("clone-repository")
	if clone.Accelerator != "CmdOrCtrl+Shift+O" || clone.Disabled || clone.Hidden {
		t.Fatalf("clone menu = %+v", clone)
	}
	clone.Click(clone, nil)
	copy := menu.ItemByID("copy")
	if copy.Role != mygo.RoleCopy || copy.Click != nil {
		t.Fatal("native copy must not also execute the hidden Electron copy")
	}
	zoom := menu.ItemByID("zoom")
	if zoom.Role != "" || zoom.Accelerator != "CmdOrCtrl+=" {
		t.Fatal("native UI must not use the no-op page zoom role")
	}
	zoom.Click(zoom, nil)
	if len(clicks) != 1 || clicks[0] != "clone-repository" || len(zooms) != 1 || zooms[0] != "zoomIn" {
		t.Fatalf("clicks=%q zooms=%q", clicks, zooms)
	}
	check := menu.ItemByID("check")
	if check.Type != mygo.MenuItemCheckbox || !check.Checked || !check.Disabled {
		t.Fatal("checkbox state was dropped")
	}
	if menu.ItemByID("sep").Type != mygo.MenuItemSeparator {
		t.Fatal("separator lost")
	}
}

func TestNativeMenuDisablesChildrenOfDisabledOrHiddenParents(t *testing.T) {
	for _, state := range []struct{ enabled, visible, ready bool }{{false, true, true}, {true, false, true}, {true, true, false}} {
		items := nativeMenuItems([]backend.KernelMenuItem{{ID: "root", Type: "submenu", LabelKey: "menu.file", Enabled: state.enabled, Visible: state.visible, Items: []backend.KernelMenuItem{{ID: "leaf", Type: "normal", LabelKey: "menu.copy", Enabled: true, Visible: true}}}}, "en", state.ready, nil, nil)
		if !items[0].Submenu[0].Disabled {
			t.Fatalf("child bypassed disabled ancestor: %+v", state)
		}
	}
}

func TestNativeMenuUsesOriginalDesktopZoomBounds(t *testing.T) {
	for _, test := range []struct {
		current  float64
		action   string
		expected float64
	}{
		{1, "zoomIn", 1.1}, {1, "zoomOut", 0.9}, {2, "zoomIn", 2}, {0.67, "zoomOut", 0.67}, {1.75, "resetZoom", 1}, {1.0999999999, "zoomIn", 1.25},
	} {
		if actual := nextNativeMenuZoom(test.current, test.action); actual != test.expected {
			t.Errorf("zoom(%v,%s)=%v want %v", test.current, test.action, actual, test.expected)
		}
	}
}
