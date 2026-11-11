package main

import (
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

// An original-kernel notification can replace repository state after paint
// and before Windows UIA asks about that painted frame. Read both sides of
// that boundary without inserting a frame that would hide the lifetime bug.
func TestRepositorySwitchPreservesPaintedListForAccessibility(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	files := []backend.KernelFile{{ID: "file", Path: "file.txt", Status: "Modified"}}
	tt := ui.NewTester(func(c *ui.Context) {
		renderWorkspaceChangesList(c, &a.nativeWorkspaceState, a.changesList.list(), files, []string{"file"}, "before", true, false, workspaceChangesCallbacks{
			checkState: func(string) ui.CheckState { return ui.CheckOn },
		})
	}, 900, 620)
	tt.Frame()
	if list, ok := tt.AccessByRole(ui.RoleList, ""); !ok || list.SetSize != 1 || !list.Multiselectable {
		t.Fatalf("initial list missing: %+v", list)
	}
	a.applyCoreSnapshot(backend.KernelSnapshot{Revision: 1, Selected: &backend.KernelSelected{
		Repository: backend.KernelRepository{KernelRepositoryReference: backend.KernelRepositoryReference{ID: 2, Path: "after"}, Name: "after"},
	}})
	if list, ok := tt.AccessByRole(ui.RoleList, ""); !ok || list.SetSize != 1 || !list.Multiselectable {
		t.Fatalf("last painted list became unreadable before next frame: %+v", list)
	}
	tt.Frame()
	if _, ok := tt.AccessByRole(ui.RoleList, ""); !ok {
		t.Fatal("replacement list missing")
	}
}

func TestModelCatalogResetPreservesPaintedListForAccessibility(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.List(c, a.providerModelList.list(), 2, func(i int) {
			ui.Text(c, []string{"first-model", "second-model"}[i])
		}).Fill()
	}, 900, 620)
	tt.Frame()
	a.editProvider("")
	if list, ok := tt.AccessByRole(ui.RoleList, ""); !ok || list.SetSize != 2 {
		t.Fatalf("old model catalog lost its painted rows: %+v", list)
	}
	tt.Frame()
	if list, ok := tt.AccessByRole(ui.RoleList, ""); !ok || list.SetSize != 2 {
		t.Fatalf("new model catalog lost its rows: %+v", list)
	}
}
