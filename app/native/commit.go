package main

import (
	"strings"

	"github.com/egoist/mygo"
)

// A submitted Git write is not undone by canceling its response waiter. Keep
// normal window/app shutdown from killing the service before it reports an outcome.
func (a *app) guardWrite(event *mygo.Preventable) {
	if a.quitBlocked() {
		event.PreventDefault()
	}
}

func (a *app) includedPaths() []string {
	var paths []string
	if a.status != nil {
		// Whole file paths only, in the real status order; stale map entries
		// and partial patch selections must never reach staging.
		for _, file := range a.status.Files {
			if a.included[file.Path] {
				paths = append(paths, file.Path)
			}
		}
	}
	return paths
}

func (a *app) canSaveVersion() bool {
	if a.core != nil {
		// The original commit flow owns conflict warnings, author checks, hooks,
		// co-authors and recovery. Do not substitute the research Git gate.
		ready := a.ready && a.repo != nil && a.status != nil && a.tab == changesTab && !a.composerLocked() &&
			!a.opening && !a.loadingStatus && a.statusErr == nil && a.coreInclusionPending == 0
		if vm := a.nativeCommitView(); vm != nil {
			if vm.Mode == "copilot" {
				return ready && vm.CanCommit
			}
			// Typing updates this local text gate immediately. The ordered queue
			// persists the final draft before saving, and the original submit
			// path validates rules; its asynchronous checks must not blink Save.
			return ready && strings.TrimSpace(a.commitSummary) != "" && (len(a.included) > 0 || vm.AllowEmpty || vm.Amending)
		}
		return ready && strings.TrimSpace(a.commitSummary) != "" && len(a.included) > 0
	}
	return a.ready && a.commitCapable && a.repo != nil && a.status != nil &&
		!a.composerLocked() && !a.loadingStatus && a.statusErr == nil &&
		!a.status.Conflicted && (a.status.Operation == nil || *a.status.Operation == "") &&
		strings.TrimSpace(a.commitSummary) != "" && len(a.included) > 0
}

// Selecting a default is a presentation preference, not permission to write
// Git. Keep it in the existing native profile so later clicks and restarts use
// the same action; a failed preference write leaves the previous action intact.
func (a *app) selectSaveAction(pushAfterCommit bool) {
	if a.closed || a.core == nil || a.composerLocked() {
		return
	}
	next := a.windowPreferences
	if next.PushAfterCommit == pushAfterCommit {
		a.saveActionErr = nil
		return
	}
	next.PushAfterCommit = pushAfterCommit
	if err := writeWindowPreferences(a.windowPreferencesPath, next); err != nil {
		a.saveActionErr = err
		return
	}
	a.windowPreferences, a.saveActionErr = next, nil
}

// The primary button and shortcut execute the same remembered action. Only
// the original kernel can acknowledge the local commit and optional push.
func (a *app) saveVersion() {
	if a.core != nil {
		a.saveCoreVersion(a.windowPreferences.PushAfterCommit)
		return
	}
	if !a.canSaveVersion() {
		return
	}
	a.savingVersion, a.saveErr, a.lastSavedSHA = true, nil, ""
	id, repoSeq := a.repo.ID, a.repoSeq
	paths, summary, description := a.includedPaths(), a.commitSummary, a.commitDescription
	b, update, ctx := a.backend, a.update, a.ctx
	go func() {
		// Hooks may take longer than a read's timeout. Only app shutdown or
		// a terminal service error cancels this write; there is no auto retry.
		result, err := b.Commit(ctx, id, paths, summary, description)
		update(func() {
			if a.closed || repoSeq != a.repoSeq {
				return
			}
			a.savingVersion, a.saveErr = false, err
			if err != nil {
				return
			}
			a.lastSavedSHA = result.SHA
			a.commitSummary, a.commitDescription = "", ""
			clear(a.included)
			a.clearDiff()
			a.loadStatus()
			a.loadHistory(false)
		})
	}()
}
