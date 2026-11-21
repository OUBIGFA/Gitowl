package main

import (
	"testing"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
)

func TestShrinkingNotebookKeepsSaveAndChangedRowsVisible(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans", "zh-Hant"} {
		t.Run(locale, func(t *testing.T) {
			a := designEvidenceApp(t, time.Now(), "review")
			a.descriptionOpen = true
			a.coreSnapshot.Selected.CommitView = &backend.KernelCommitView{Mode: "manual", CanCommit: true}
			a.coreSnapshot.Workspace.ShowFilesFilter = true
			a.coreSnapshot.Workspace.Menus = append(a.coreSnapshot.Workspace.Menus, backend.KernelWorkspaceMenu{
				Target: backend.KernelWorkspaceMenuTarget{Kind: "changes"}, Lease: "fixture", Items: []backend.KernelWorkspaceMenuItem{{LabelKey: "workspace.discardAll", Enabled: true}},
			})
			tester := designEvidenceTester(a, 1180, 760, locale, true)
			tester.SetSize(884, 611) // 900x620 outer pixels includes the native frame.
			tester.Advance(time.Second)
			tx := locales.In(locale)
			save := visibleControl(t, tester, tx.SaveVersion(), 884, 611)
			if save.H != 40 {
				t.Fatalf("save button clipped: %+v", save)
			}
			visibleControl(t, tester, tx.SaveOptions(), 884, 611)
			row := visibleControl(t, tester, tx.OpenFile("docs/routes/lake-district.md"), 884, 611)
			if row.H < 28 {
				t.Fatalf("changed file row clipped: %+v", row)
			}
		})
	}
}
