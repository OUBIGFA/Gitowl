package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestOpenProjectChooserOffersOriginalRepositoryActions(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		a := newApp(nil, nil)
		a.core, a.ready, a.openModal = &backend.Kernel{}, true, true
		a.coreSnapshot = &backend.KernelSnapshot{}
		for _, id := range []string{"clone-repository", "new-repository", "add-local-repository"} {
			a.coreSnapshot.Menu.Items = append(a.coreSnapshot.Menu.Items, backend.KernelMenuItem{ID: id, Type: "normal", Enabled: true, Visible: true})
		}
		tt := ui.NewTester(func(c *ui.Context) { setWorkspaceTheme(c); a.openDialog(c, locales.In(c.Locale())) }, 900, 620)
		tt.SetLocale(locale)
		tt.Frame()
		for _, title := range []string{"CloneRepository", "CreateRepository", "AddRepository"} {
			label := nativeDialogLabel(locale, "repositoryDialog."+title)
			control, found := tt.AccessByRole(ui.RoleButton, label)
			if !found || control.Disabled {
				t.Fatalf("original repository action %q unavailable: %+v", label, control)
			}
			visibleControl(t, tt, label, 900, 620)
		}
		tt.Key(0, ui.KeyEscape)
		if a.openModal {
			t.Fatal("Escape did not dismiss the project chooser")
		}
		a.close()
	}
}

func TestNativePendingOpenDismissalDoesNotCancelOrNavigateFromLateReply(t *testing.T) {
	for _, escape := range []bool{false, true} {
		a := newApp(nil, nil)
		a.core = &backend.Kernel{}
		// Hold the production queue at its I/O boundary. Do not replace opener
		// logic or pretend that dismissal cancels the original AddRepositories.
		a.coreQueue = make(chan nativeOperation, 4)
		a.coreSnapshot = &backend.KernelSnapshot{}
		a.ready, a.openModal = true, true
		a.openCoreRepository("C:/fixture/slow-project")
		op := <-a.coreQueue
		if !a.opening || a.openCancel != nil {
			t.Fatal("native open should have no fabricated Git cancellation")
		}
		tt := ui.NewTester(a.view, 900, 620)
		tt.SetLocale("en")
		tt.Frame()
		if escape {
			tt.Key(0, ui.KeyEscape)
		} else if err := tt.Click(locales.In("en").Cancel()); err != nil {
			t.Fatal(err)
		}
		if a.openModal || a.opening {
			t.Fatal("opener did not dismiss")
		}
		op.complete(backend.KernelResult{AddedRepositories: []backend.KernelRepositoryReference{{ID: 1, Path: "C:/fixture/slow-project"}}}, nil)
		if len(a.coreQueue) != 0 || a.openModal || a.opening {
			t.Fatal("late add navigated after native dismissal")
		}
		a.close()
	}
}

func TestNativeLateSelectionReplyDoesNotCloseANewerOpener(t *testing.T) {
	a := newApp(nil, nil)
	defer a.close()
	a.core = &backend.Kernel{}
	a.coreQueue = make(chan nativeOperation, 4)
	a.coreSnapshot = &backend.KernelSnapshot{}
	a.ready, a.openModal = true, true
	a.openCoreRepository("C:/fixture/A")
	add := <-a.coreQueue
	ref := backend.KernelRepositoryReference{ID: 1, Path: "C:/fixture/A"}
	add.complete(backend.KernelResult{AddedRepositories: []backend.KernelRepositoryReference{ref}}, nil)
	selectReply := <-a.coreQueue
	a.cancelOpen()
	a.showOpen()
	a.pathDraft = "C:/fixture/B"
	a.openCoreRepository(a.pathDraft)
	result := backend.KernelResult{Snapshot: backend.KernelSnapshot{Revision: 2, Selected: &backend.KernelSelected{Repository: backend.KernelRepository{KernelRepositoryReference: ref, Name: "A", Kind: "repository"}, Section: "changes"}}}
	selectReply.complete(result, nil)
	a.applyCoreSnapshot(result.Snapshot)
	if !a.openModal || !a.opening || a.pathDraft != "C:/fixture/B" || len(a.coreQueue) != 1 {
		t.Fatal("late A selection changed B's opener")
	}
	if a.repo == nil || a.repo.Path != ref.Path {
		t.Fatal("the original selection snapshot was discarded instead of applied")
	}
}
