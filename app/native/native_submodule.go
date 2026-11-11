package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// A presentation of the original diff. The full path stays in the kernel; an
// open request is resolved against its currently selected submodule again.
type nativeSubmoduleDiff struct {
	Path          string  `json:"path"`
	URL           *string `json:"url"`
	RepositoryURL *string `json:"repositoryURL"`
	OldSHA        *string `json:"oldSHA"`
	NewSHA        *string `json:"newSHA"`
	Status        struct {
		Modified  bool `json:"modifiedChanges"`
		Untracked bool `json:"untrackedChanges"`
	} `json:"status"`
}

func (a *app) nativeSubmoduleView(c *ui.Context, tx locales.Messages, diff *nativeSubmoduleDiff, allowOpen bool) {
	if diff == nil {
		emptyState(c, tx.DiffSubmoduleEmpty())
		return
	}
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Text(c, tx.DiffSubmodule()).FontWeight(500)
		ui.Text(c, diff.Path)
		for _, item := range []struct {
			label string
			value *string
		}{{tx.ImageBefore(), diff.OldSHA}, {tx.ImageAfter(), diff.NewSHA}} {
			if item.value != nil {
				value := *item.value
				ui.TextInput(c, &value).ReadOnly(true).FillWidth().Label(item.label)
			}
		}
		if diff.Status.Modified {
			ui.Text(c, tx.FileModified())
		}
		if diff.Status.Untracked {
			ui.Text(c, tx.FileUntracked())
		}
		if diff.URL != nil {
			if diff.RepositoryURL != nil {
				ui.Link(c, *diff.RepositoryURL, *diff.RepositoryURL).FillWidth()
			}
			if !allowOpen {
				return
			}
			fileID := ""
			if a.coreSnapshot != nil && a.coreSnapshot.Selected != nil {
				s := a.coreSnapshot.Selected
				files := append(append([]backend.KernelFile{}, s.Files...), s.HistoryFiles...)
				if w := a.nativeWorkspace(); w != nil && w.Stash != nil && w.Stash.Selected {
					files = w.Stash.Files
				}
				for _, file := range files {
					if file.Path == diff.Path {
						fileID = file.ID
						break
					}
				}
			}
			quietButton(c.Key("open-submodule"), tx.OpenRepository(), "folder-open").Disabled(a.core == nil || fileID == "").OnClick(func() {
				a.workspaceAction(backend.KernelWorkspaceAction{Type: "submodule.open", FileID: fileID})
			})
		}
	})
}
