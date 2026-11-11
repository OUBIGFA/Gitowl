package main

import (
	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Focus only after the original popup stack permits it, then acknowledge the
// same AppStore flag React acknowledges. Progress snapshots cannot steal focus.
func (a *app) focusCoreCommitMessage(input ui.Element) {
	if a.coreFocusTarget != "commit-summary" || a.core == nil || a.coreSnapshot == nil || a.coreSnapshot.CurrentPopup != nil || input.IsDisabled() {
		return
	}
	input.Focus()
	a.coreFocusTarget = ""
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.AcknowledgeCommitMessageFocus(ctx) }})
}

// Consume toolkit actions once, in pipe order. Storing an Element/Context or
// reusing an action from a later snapshot would lose native focus/lifetime.
func (a *app) beginCorePresentation() {
	for _, action := range a.corePresentationActions {
		switch action {
		case "focus-history", "focus-changes":
			a.reviewingFile, a.viewingCommit, a.reviewOptionsOpen = false, false, false
			a.coreFocusTarget = action
		case "find-text":
			if a.preview != nil && len(a.preview.patches) > 0 {
				a.reviewingFile = true
				a.coreFocusTarget = action
			}
		default:
			if !a.workspacePresentation(action) && action == "select-all" && a.coreWindowAction != nil {
				a.coreWindowAction(action)
			}
		}
	}
	a.corePresentationActions = nil
}

func (a *app) finishCorePresentation(c *ui.Context) {
	switch a.coreFocusTarget {
	case "focus-history":
		if a.commitsList.list().Focus(c) {
			a.coreFocusTarget = ""
		}
	case "focus-changes":
		if a.changesList.list().Focus(c) {
			a.coreFocusTarget = ""
		}
	case "find-text":
		if a.preview != nil && len(a.preview.patches) > 0 {
			a.preview.patches[0].state.Run(editor.Find)
			a.coreFocusTarget = ""
		}
	}
	// MyGo's native Select All role first edits the focused text control. Only
	// unhandled editing commands reach this shortcut, where lists select their
	// original file/commit IDs. Never redirect a text selection to Electron.
	if c.Shortcut(ui.Cmd, ui.KeyA) {
		a.workspacePresentation("select-all")
	}
	a.observeWorkspaceFocus(c)
}
