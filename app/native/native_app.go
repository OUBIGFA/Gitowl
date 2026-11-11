package main

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/OUBIGFA/Gitowl/app/native/locales"
	"github.com/egoist/mygo/ui"
)

// Only presentation mutations use this ordered queue. The original Dispatcher
// still owns their semantics; replies to its modal callbacks and cancellation
// are concurrent so a pending hook/SSH prompt cannot block its own answer.
type nativeOperation struct {
	call      func() (backend.KernelResult, error)
	complete  func(backend.KernelResult, error)
	draft     bool
	inclusion bool
}

func newCoreApp(core *backend.Kernel, logo ui.ImageSource) *app {
	a := newApp(nil, logo)
	a.core, a.coreZoom = core, 1
	a.coreQueue = make(chan nativeOperation, 256)
	go func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case operation := <-a.coreQueue:
				result, err := operation.call()
				a.update(func() {
					if a.closed {
						return
					}
					a.corePending--
					if operation.draft {
						a.coreDraftPending--
					}
					if operation.inclusion {
						a.coreInclusionPending--
					}
					if operation.complete != nil {
						operation.complete(result, err)
					}
					if err == nil {
						a.applyCoreSnapshot(result.Snapshot)
					}
				})
			}
		}
	}()
	return a
}

func (a *app) queueCore(operation nativeOperation) {
	if a.closed || a.core == nil {
		return
	}
	a.corePending++
	if operation.draft {
		a.coreDraftPending++
	}
	if operation.inclusion {
		a.coreInclusionPending++
	}
	a.coreQueue <- operation
}

func (a *app) connectCore(initialPath string) {
	a.coreInitialPath = initialPath
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.State(ctx) },
		complete: func(result backend.KernelResult, err error) {
			a.ready, a.serviceErr = err == nil, err
			a.commitCapable = err == nil
			if err == nil && initialPath != "" && !result.Snapshot.Welcome {
				a.coreInitialPath = ""
				a.openCoreRepository(initialPath)
			}
		},
	})
}

func coreFiles(files []backend.KernelFile) []backend.File {
	out := make([]backend.File, len(files))
	for i, file := range files {
		out[i] = backend.File{Path: file.Path, Status: file.Status, IsSubmodule: file.IsSubmodule}
		if file.OldPath != nil {
			out[i].OldPath = *file.OldPath
		}
	}
	return out
}

func coreFileIndex(files []backend.KernelFile, ids []string) int {
	if len(ids) == 0 {
		return -1
	}
	for i, file := range files {
		if file.ID == ids[0] {
			return i
		}
	}
	return -1
}

// Apply the latest original state, not a native Git model. Optimistic text and
// checkbox paint is temporary while the ordered original mutations settle;
// switching projects always obtains that project's original stored draft.
func (a *app) applyCoreSnapshot(snapshot backend.KernelSnapshot) {
	if a.closed || (a.coreSnapshot != nil && snapshot.Revision < a.coreSnapshot.Revision) {
		return
	}
	if snapshot.FocusCommitMessage && (a.coreSnapshot == nil || !a.coreSnapshot.FocusCommitMessage) {
		a.coreFocusTarget = "commit-summary"
	}
	if snapshot.Workspace != nil && snapshot.Workspace.Navigation == "repositories" && (a.coreSnapshot == nil || a.coreSnapshot.Workspace == nil || a.coreSnapshot.Workspace.Navigation != "repositories") {
		a.coreFocusTarget = "focus-projects"
	}
	finishedWelcome := a.coreSnapshot != nil && a.coreSnapshot.Welcome && !snapshot.Welcome
	// A successful original generation updates the message while its busy scope
	// is active. Reveal that new body once; unrelated snapshots respect collapse.
	if previous := a.coreSnapshot; previous != nil && previous.Selected != nil && snapshot.Selected != nil {
		before, after := previous.Selected, snapshot.Selected
		if before.GenerationInProgress && before.Repository.KernelRepositoryReference == after.Repository.KernelRepositoryReference &&
			before.Message.Timestamp != after.Message.Timestamp && after.Message.Description != nil && *after.Message.Description != "" && a.coreDraftPending == 0 {
			a.descriptionOpen = true
		}
	}
	if snapshot.Appearance != nil && a.setCoreTheme != nil && (a.coreSnapshot == nil || a.coreSnapshot.Appearance == nil || a.coreSnapshot.Appearance.Theme != snapshot.Appearance.Theme) {
		a.setCoreTheme(snapshot.Appearance.Theme)
	}
	a.coreSnapshot = &snapshot
	if snapshot.RepositoryFilter != nil && a.coreProjectFilterPending == 0 {
		a.coreProjectFilter = snapshot.RepositoryFilter.Text
	}
	a.setNativeImageDiffType(nativeImageDiffType(snapshot.ImageDiffType))
	a.pruneNativeDialogForms(snapshot)
	a.prepareNativeDialogMedia(snapshot)
	if a.serviceErr == nil {
		a.ready = !snapshot.Welcome
	}
	if finishedWelcome && a.coreInitialPath != "" && a.ready {
		path := a.coreInitialPath
		a.coreInitialPath = ""
		a.openCoreRepository(path)
	}
	selected := snapshot.Selected
	if selected == nil {
		a.repo, a.status, a.changeset, a.commits = nil, nil, nil, nil
		a.changeIndex, a.commitIndex, a.commitFileIndex = -1, -1, -1
		a.clearDiff()
		return
	}
	changedRepository := a.repo == nil || a.repo.ID != selected.Repository.ID || a.repo.Path != selected.Repository.Path
	a.applyWorkspaceSnapshot(snapshot, changedRepository)
	if changedRepository {
		a.repoSeq++
		a.reviewingFile, a.viewingCommit, a.reviewOptionsOpen = false, false, false
		a.changesList, a.commitsList, a.commitFilesList = nativeListState{}, nativeListState{}, nativeListState{}
		a.corePreviewDiff, a.corePreviewIdentity = nil, ""
		a.coreHistoryExhausted = false
		a.lastSavedSHA, a.saveErr = "", nil
	}
	a.repo = &backend.Repository{ID: selected.Repository.ID, Path: selected.Repository.Path, Name: selected.Repository.Name}
	if selected.Repository.Alias != nil {
		a.repo.Name = *selected.Repository.Alias
	}
	a.status = &backend.Status{
		Branch: selected.Branch, Upstream: selected.Upstream, Ahead: selected.Ahead, Behind: selected.Behind,
		Files: coreFiles(selected.Files), Conflicted: selected.ConflictKind != nil, Operation: selected.ConflictKind,
	}
	if selected.Section == "history" {
		a.tab = historyTab
	} else {
		a.tab = changesTab
	}
	if a.coreInclusionPending == 0 || changedRepository {
		a.included = make(map[string]bool)
		for _, file := range selected.Files {
			if file.Inclusion != "none" {
				a.included[file.Path] = true
			}
		}
	}
	if a.coreDraftPending == 0 || changedRepository {
		a.commitSummary, a.commitDescription = selected.Message.Summary, ""
		if selected.Message.Description != nil {
			a.commitDescription = *selected.Message.Description
		}
	}
	a.commits = selected.History
	a.changeIndex = coreFileIndex(selected.Files, selected.SelectedFileIDs)
	a.commitIndex = -1
	if len(selected.HistorySelection) > 0 {
		for i, commit := range a.commits {
			if commit.SHA == selected.HistorySelection[0] {
				a.commitIndex = i
				break
			}
		}
	}
	a.changeset = &backend.Changeset{Files: coreFiles(selected.HistoryFiles), LinesAdded: selected.HistoryLinesAdded, LinesDeleted: selected.HistoryLinesDeleted}
	a.commitFileIndex = -1
	if selected.HistoryFileID != nil {
		a.commitFileIndex = coreFileIndex(selected.HistoryFiles, []string{*selected.HistoryFileID})
	}
	a.historyLoaded, a.historyMore = a.tab == historyTab, len(a.commits) > 0 && !a.coreHistoryExhausted
	a.savingVersion = a.coreCommitPending || selected.CommitInProgress
	a.generating = a.coreGenerationPending || selected.GenerationInProgress
	// Original eligibility includes repository account assignment, not a native
	// guess based on any signed-in account or the research SDK's account list.
	a.generationCapable = selected.GenerationAvailable
	a.prepareCorePreview(selected)
}

func (a *app) prepareCorePreview(selected *backend.KernelSelected) {
	path := ""
	if a.tab == changesTab && a.changeIndex >= 0 {
		path = selected.Files[a.changeIndex].Path
	}
	if a.tab == historyTab && a.commitFileIndex >= 0 {
		path = selected.HistoryFiles[a.commitFileIndex].Path
	}
	if a.tab == changesTab {
		if file := a.workspaceStashFile(); file != nil {
			path = file.Path
		}
	}
	identity := selected.Repository.Path + ":" + selected.Section + ":" + path
	projectedDiff := a.workspaceReviewDiff(selected.Diff)
	if identity == a.corePreviewIdentity && reflect.DeepEqual(a.corePreviewDiff, projectedDiff) {
		return
	}
	a.corePreviewDiff, a.corePreviewIdentity = projectedDiff, identity
	a.clearDiff()
	if projectedDiff == nil || path == "" {
		return
	}
	if projectedDiff.Kind == "image" {
		a.preview = &preview{kind: "image", before: projectedDiff.Previous, after: projectedDiff.Current}
		a.loadNativeImageDiff(&a.nativeImageDiffState, *projectedDiff, identity)
		return
	}
	seq, diff, update := a.diffSeq, *projectedDiff, a.update
	a.loadingDiff = true
	go func() {
		preview, err := preparePreview(diff, path)
		update(func() {
			if a.closed || seq != a.diffSeq {
				return
			}
			a.preview, a.diffErr, a.loadingDiff = preview, err, false
		})
	}()
}

func (a *app) coreReference() (backend.KernelRepositoryReference, bool) {
	if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil || !a.ready || a.closed {
		return backend.KernelRepositoryReference{}, false
	}
	return a.coreSnapshot.Selected.Repository.KernelRepositoryReference, true
}

func (a *app) receiveCoreEvent(event backend.KernelEvent) {
	if a.closed {
		return
	}
	if event.Snapshot != nil {
		a.applyCoreSnapshot(*event.Snapshot)
	}
	if event.PresentationAction != "" {
		a.corePresentationActions = append(a.corePresentationActions, event.PresentationAction)
	}
	if event.Zoom != nil {
		a.coreZoom = *event.Zoom
	}
	if event.WindowAction != "" && a.coreWindowAction != nil {
		a.coreWindowAction(event.WindowAction)
	}
	if event.Failure != nil {
		a.serviceStopped(errors.New(event.Failure.Message))
	}
}

func (a *app) openCoreRepository(path string) {
	path = strings.TrimSpace(path)
	if !a.ready || a.savingVersion || path == "" || a.coreSnapshot == nil || a.coreSnapshot.Welcome {
		return
	}
	a.openSeq++
	seq := a.openSeq
	a.opening, a.openErr = true, nil
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.AddRepositories(ctx, []string{path}) },
		complete: func(result backend.KernelResult, err error) {
			// Dismissing the local opener does not cancel AppStore's add. A late
			// result still updates the real repository list, but cannot navigate
			// away from the page the user chose after dismissal.
			if seq != a.openSeq {
				return
			}
			if err != nil || len(result.AddedRepositories) == 0 {
				a.opening, a.openErr = false, err
				return // Original error/recovery popup remains the authority.
			}
			a.selectCoreRepository(result.AddedRepositories[0])
		},
	})
}

func (a *app) selectCoreRepository(reference backend.KernelRepositoryReference) {
	seq := a.openSeq
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.SelectRepository(ctx, reference) },
		complete: func(_ backend.KernelResult, err error) {
			if seq != a.openSeq {
				return
			}
			a.opening, a.openErr = false, err
			if err == nil {
				a.openModal = false
			}
		},
	})
}

// Refresh and remote tasks can remain in flight while the UI reviews or edits;
// they don't block the ordered draft/include queue or their own modal replies.
func (a *app) runCoreTask(call func() (backend.KernelResult, error), complete func(backend.KernelResult, error)) {
	if a.closed || a.core == nil {
		return
	}
	a.corePending++
	go func() {
		result, err := call()
		a.update(func() {
			if a.closed {
				return
			}
			a.corePending--
			if complete != nil {
				complete(result, err)
			}
			if err == nil {
				a.applyCoreSnapshot(result.Snapshot)
			}
		})
	}()
}

func (a *app) refreshCore() {
	ref, ok := a.coreReference()
	if !ok || a.savingVersion {
		return
	}
	core, ctx := a.core, a.ctx
	a.loadingStatus, a.statusErr = true, nil
	a.runCoreTask(func() (backend.KernelResult, error) { return core.RefreshRepository(ctx, ref) }, func(_ backend.KernelResult, err error) {
		a.loadingStatus, a.statusErr = false, err
		a.coreHistoryExhausted = false
	})
}

func (a *app) setCoreTab(tab int) {
	ref, ok := a.coreReference()
	if !ok || tab == a.tab {
		return
	}
	section := "changes"
	if tab == historyTab {
		section = "history"
	}
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.ChangeSection(ctx, ref, section) },
		complete: func(result backend.KernelResult, err error) {
			if err != nil {
				a.historyErr = err
				return
			}
			if tab == historyTab && result.Snapshot.Selected != nil && len(result.Snapshot.Selected.History) == 0 {
				a.loadCoreHistory()
			}
		},
	})
}

func (a *app) loadCoreHistory() {
	ref, ok := a.coreReference()
	if !ok || a.loadingHistory {
		return
	}
	core, ctx, previous := a.core, a.ctx, len(a.commits)
	a.loadingHistory = true
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.LoadHistory(ctx, ref) },
		complete: func(result backend.KernelResult, err error) {
			a.loadingHistory, a.historyErr = false, err
			if err == nil && result.Snapshot.Selected != nil {
				a.coreHistoryExhausted = len(result.Snapshot.Selected.History) <= previous
			}
		},
	})
}

func (a *app) selectCoreCommit(index int) {
	ref, ok := a.coreReference()
	if !ok || index < 0 || index >= len(a.commits) {
		return
	}
	core, ctx, sha := a.core, a.ctx, a.commits[index].SHA
	a.loadingCommit, a.commitErr = true, nil
	a.queueCore(nativeOperation{
		call:     func() (backend.KernelResult, error) { return core.SelectHistory(ctx, ref, []string{sha}, true) },
		complete: func(_ backend.KernelResult, err error) { a.loadingCommit, a.commitErr = false, err },
	})
}

func (a *app) selectCoreFile(index int) {
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	selected, core, ctx := a.coreSnapshot.Selected, a.core, a.ctx
	if a.tab == changesTab {
		if index < 0 || index >= len(selected.Files) {
			return
		}
		id := selected.Files[index].ID
		a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.SelectFiles(ctx, ref, []string{id}) }})
	} else {
		if index < 0 || index >= len(selected.HistoryFiles) {
			return
		}
		id := selected.HistoryFiles[index].ID
		a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.SelectHistoryFile(ctx, ref, id) }})
	}
}

func (a *app) changeIncluded(path string, included bool) {
	if a.composerLocked() || !a.ready {
		return
	}
	if included {
		a.included[path] = true
	} else {
		delete(a.included, path)
	}
	if a.core == nil {
		a.generationDraftEdited()
		return
	}
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	for _, file := range a.coreSnapshot.Selected.Files {
		if file.Path != path {
			continue
		}
		core, ctx, id := a.core, a.ctx, file.ID
		a.queueCore(nativeOperation{inclusion: true, call: func() (backend.KernelResult, error) { return core.IncludeFiles(ctx, ref, []string{id}, included) }})
		return
	}
}

func (a *app) changeAllIncluded(included bool) {
	if a.composerLocked() || !a.ready {
		return
	}
	clear(a.included)
	if included && a.status != nil {
		for _, file := range a.status.Files {
			a.included[file.Path] = true
		}
	}
	if a.core == nil {
		a.generationDraftEdited()
		return
	}
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	core, ctx := a.core, a.ctx
	a.queueCore(nativeOperation{inclusion: true, call: func() (backend.KernelResult, error) { return core.IncludeAll(ctx, ref, included) }})
}

func (a *app) persistCoreDraft() {
	ref, ok := a.coreReference()
	if !ok || a.composerLocked() {
		return
	}
	core, ctx := a.core, a.ctx
	message := a.coreSnapshot.Selected.Message
	body := a.commitDescription
	message.Summary, message.Description, message.Timestamp = a.commitSummary, &body, float64(time.Now().UnixMilli())
	message.GeneratedByCopilot = false // Editing is the original manual-input transition.
	a.queueCore(nativeOperation{draft: true, call: func() (backend.KernelResult, error) { return core.SetMessage(ctx, ref, message) }})
}

func (a *app) saveCoreVersion() {
	ref, ok := a.coreReference()
	if !ok || !a.canSaveVersion() {
		return
	}
	if vm := a.nativeCommitView(); vm != nil && vm.Mode == "copilot" {
		a.saveCoreAssistedVersion()
		return
	}
	body := a.commitDescription
	commit := backend.KernelCommitContext{Summary: a.commitSummary, Description: &body, MessageGeneratedByCopilot: a.coreSnapshot.Selected.Message.GeneratedByCopilot}
	core, ctx := a.core, a.ctx
	a.coreCommitPending, a.savingVersion, a.saveErr = true, true, nil
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.CommitIncludedChanges(ctx, ref, commit) },
		complete: func(result backend.KernelResult, err error) {
			a.coreCommitPending, a.savingVersion, a.saveErr = false, false, err
			// Canceled/warning outcomes keep the original draft; never clear it
			// or invent a successful commit, staging, pull or push.
			if err == nil && result.Committed != nil && *result.Committed && result.Snapshot.Selected != nil && result.Snapshot.Selected.HeadSHA != nil {
				a.lastSavedSHA = *result.Snapshot.Selected.HeadSHA
			}
		},
	})
}

func (a *app) generateCoreMessage() {
	ref, ok := a.coreReference()
	if !ok || !a.generationActionEnabled() {
		return
	}
	var ids []string
	for _, file := range a.coreSnapshot.Selected.Files {
		if a.included[file.Path] {
			ids = append(ids, file.ID)
		}
	}
	core, ctx := a.core, a.ctx
	override := a.commitSummary != "" || a.commitDescription != ""
	a.coreGenerationPending, a.generating = true, true
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.GenerateMessage(ctx, ref, ids, override) },
		complete: func(_ backend.KernelResult, err error) {
			a.coreGenerationPending, a.generating, a.generationErr = false, false, err
		},
	})
}

func (a *app) stopCoreGeneration() {
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	core, ctx := a.core, a.ctx
	a.runCoreTask(func() (backend.KernelResult, error) { return core.CancelGeneration(ctx, ref) }, nil)
}

func (a *app) coreRemoteAction(name string) {
	ref, ok := a.coreReference()
	if !ok {
		return
	}
	item, admitted := nativeMenuAction(a.coreSnapshot.Menu.Items, name)
	if !admitted || !item.Enabled || a.coreSnapshot.Selected.RemoteInProgress || a.coreRemotePending {
		return
	}
	core, ctx := a.core, a.ctx
	a.coreRemotePending = true
	a.runCoreTask(func() (backend.KernelResult, error) {
		switch name {
		case "fetch":
			return core.Fetch(ctx, ref)
		case "pull":
			return core.Pull(ctx, ref)
		case "push":
			return core.Push(ctx, ref)
		default:
			return backend.KernelResult{}, errors.New("unknown native remote action; no operation was sent")
		}
	}, func(_ backend.KernelResult, err error) { a.coreRemotePending, a.statusErr = false, err })
}

func (a *app) replyCorePopup(reply backend.KernelPopupReply) {
	core, ctx := a.core, a.ctx
	if core == nil {
		return
	}
	form := a.coreDialogForms[reply.PopupID]
	automatic := form != nil && (form.changePending || form.navigationPending) && form.changeAction == reply.Action
	a.runCoreTask(func() (backend.KernelResult, error) { return core.ReplyPopup(ctx, reply) }, func(_ backend.KernelResult, err error) {
		if a.corePopupForm.popupID == reply.PopupID {
			a.corePopupForm.busy, a.corePopupForm.err = false, err
		}
		if form != nil && a.coreDialogForms[reply.PopupID] == form {
			form.finishReply(automatic, err)
		}
	})
}

func (a *app) closeCorePopup(id int) {
	core, ctx := a.core, a.ctx
	if core == nil {
		return
	}
	a.runCoreTask(func() (backend.KernelResult, error) { return core.ClosePopup(ctx, id) }, func(_ backend.KernelResult, err error) {
		if a.corePopupForm.popupID == id {
			a.corePopupForm.busy, a.corePopupForm.err = false, err
		}
		if form := a.coreDialogForms[id]; form != nil {
			form.busy, form.err, form.accept = false, err, err == nil
		}
	})
}

func (a *app) reportCoreWindowState() {
	if a.closed || a.core == nil || a.coreWindowState == nil {
		return
	}
	core, ctx, state := a.core, a.ctx, a.coreWindowState()
	go func() {
		if err := core.WindowState(ctx, state); err != nil {
			a.update(func() {
				if !a.closed {
					a.serviceStopped(err)
				}
			})
		}
	}()
}

func (a *app) coreRemoteControls(c *ui.Context, tx locales.Messages) {
	if a.coreSnapshot == nil || a.coreSnapshot.Selected == nil {
		return
	}
	reason := ""
	switch {
	case !a.ready:
		reason = tx.ActionNeedsReady()
	case a.savingVersion:
		reason = tx.ActionSaveBusy()
	case a.coreSnapshot.Selected.RemoteInProgress || a.coreRemotePending:
		reason = tx.RemoteBusy()
	}
	for _, action := range []struct{ name, key, label string }{
		{"fetch", "fetch-updates", tx.FetchUpdates()}, {"pull", "pull-updates", tx.PullUpdates()}, {"push", "push-changes", tx.PushChanges()},
	} {
		item, available := nativeMenuAction(a.coreSnapshot.Menu.Items, action.name)
		label, unavailable := action.label, reason
		if action.name == "push" && !a.coreSnapshot.Selected.HasRemote {
			label = nativeDialogLabel(c.Locale(), "repositoryDialog.PublishRepository")
		} else if action.name == "push" && a.coreSnapshot.Selected.ForcePushRecommended && available {
			item.AccessKey = ""
			label = nativeMenuLabel(item, c.Locale())
		}
		if unavailable == "" && (!available || !item.Enabled) {
			if !a.coreSnapshot.Selected.HasRemote {
				unavailable = tx.RemoteUnconfigured()
			} else {
				unavailable = tx.ActionNotAvailable()
			}
		}
		width, _ := c.Size()
		if action.name == "fetch" && width < 1100 {
			// Fetch is the secondary sync action; Pull and Push/Publish retain
			// visible words even at the minimum window width.
			iconButton(c.Key(action.key), label, "refresh-cw").Size(32, 32).Padding(7).Tooltip(label + " " + unavailable).Disabled(unavailable != "").OnClick(func() { a.coreRemoteAction(action.name) })
		} else {
			quietButton(c.Key(action.key), label, "").Height(32).Tooltip(unavailable).Disabled(unavailable != "").OnClick(func() { a.coreRemoteAction(action.name) })
		}
	}
}

func (a *app) generationTooltip(tx locales.Messages) string {
	if a.core == nil {
		return tx.GenerationOnly()
	}
	if !a.ready {
		return tx.ActionNeedsReady()
	}
	if a.savingVersion {
		return tx.ActionSaveBusy()
	}
	if a.opening {
		return tx.LoadingRepository()
	}
	if a.loadingStatus {
		return tx.LoadingStatus()
	}
	if a.statusErr != nil {
		return tx.FailedStatus(a.statusErr.Error())
	}
	if settings := a.coreGenerationSettings(); settings != nil && settings.Source == "custom" && !coreCustomSelectionValid(settings) {
		return tx.ChooseGenerationModel()
	}
	if !a.generationCapable {
		return tx.GenerationNeedsAccount()
	}
	if len(a.included) == 0 {
		return tx.GenerationNeedsFiles()
	}
	return tx.GenerationOnly()
}

// The project rail uses the original repository/worktree list and identity.
func (a *app) coreProjectList(c *ui.Context, tx locales.Messages, colors workspaceColors) {
	if a.coreSnapshot == nil || len(a.coreSnapshot.Repositories) == 0 {
		return
	}
	repositories := a.coreSnapshot.Repositories
	if filter := a.coreSnapshot.RepositoryFilter; filter != nil {
		// Keep original rank and path identity, including worktrees sharing IDs.
		byPath := make(map[string]backend.KernelRepository, len(repositories))
		for _, repository := range repositories {
			byPath[repository.Path] = repository
		}
		repositories = make([]backend.KernelRepository, 0, len(filter.Paths))
		for _, path := range filter.Paths {
			if repository, ok := byPath[path]; ok {
				repositories = append(repositories, repository)
			}
		}
		if identity := strings.Join(filter.Paths, "\x00"); identity != a.coreProjectPaths {
			a.coreProjectPaths, a.coreProjectCursor = identity, -1
		}
	}
	searchLabel := workspaceLabel(c.Locale(), "workspace.searchProjects")
	search := ui.SearchField(c.Key("project-search"), &a.coreProjectFilter).Height(36).FillWidth().Shrink(0).
		Margin(12, 0, 8, 0).Label(searchLabel).Placeholder(searchLabel).Disabled(!a.ready).
		OnChange(a.filterCoreProjects)
	// The menu's original foldout becomes focus in the persistent project rail.
	// Acknowledge only after focus is placed; never retain this frame's Element.
	if a.coreFocusTarget == "focus-projects" && a.core != nil && a.coreSnapshot.CurrentPopup == nil && !search.IsDisabled() {
		search.Focus()
		a.coreFocusTarget = ""
		core, ctx := a.core, a.ctx
		a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) { return core.AcknowledgeRepositoryListFocus(ctx) }})
	}
	if len(repositories) == 0 {
		ui.Text(c, workspaceLabel(c.Locale(), "workspace.noProjectsFound")).TextColor(c.Theme().TextMuted).Padding(8, 10).Role(ui.RoleStatus)
		ui.Spacer(c)
		return
	}
	a.coreProjects.Key = func(i int) any { return repositories[i].Path }
	a.coreProjects.Label = func(i int) string {
		if repositories[i].Alias != nil {
			return *repositories[i].Alias
		}
		return repositories[i].Name
	}
	a.coreProjects.Selected = &a.coreProjectCursor
	canSelect := a.ready && !a.savingVersion && a.coreProjectFilterPending == 0
	selectProject := func(i int) {
		if canSelect && i >= 0 && i < len(repositories) {
			a.selectCoreRepository(repositories[i].KernelRepositoryReference)
		}
	}
	baseTheme := c.Theme()
	listTheme := *baseTheme
	if !c.Preferences().HighContrast {
		listTheme.Accent, listTheme.InactiveAccent, listTheme.AccentText = colors.selected, colors.selected, baseTheme.Text
	}
	c.SetTheme(&listTheme)
	defer c.SetTheme(baseTheme)
	list := ui.List(c.Key("kernel-projects"), &a.coreProjects, len(repositories), func(i int) {
		repository := repositories[i]
		name := repository.Name
		if repository.Alias != nil {
			name = *repository.Alias
		}
		button := ui.ButtonBase(c.Key(repository.Path)).Height(36).FillWidth().Padding(6, 10).Gap(8).
			Label(name).Tooltip(repository.Path).Disabled(!canSelect)
		quietSurface(c, button)
		button.PopoverMenu(func(m *ui.Menu) { a.renderRepositoryMenu(m, c.Locale(), repository.KernelRepositoryReference) })
		current := a.repo != nil && a.repo.ID == repository.ID && a.repo.Path == repository.Path
		if clone := a.coreSnapshot.CloningRepository; clone != nil {
			current = clone.Repository.KernelRepositoryReference == repository.KernelRepositoryReference
		}
		if current {
			button.Background(colors.selected).Value(tx.CurrentPage())
		}
		button.Children(func() {
			ui.Icon(c, icons["folder"]).FontSize(18).Shrink(0)
			ui.Text(c, name).SingleLine().Grow(1).MinWidth(0)
		}).OnClick(func() { a.coreProjectCursor = i; selectProject(i) })
	}).Grow(1).MinHeight(0).FillWidth().Gap(2).Label(tx.Projects())
	// Arrow navigation focuses the native list, not its nested button. Give
	// that focus owner the current row's same menu for Menu and Shift+F10.
	list.PopoverMenu(func(m *ui.Menu) {
		if i := a.coreProjectCursor; canSelect && i >= 0 && i < len(repositories) {
			a.renderRepositoryMenu(m, c.Locale(), repositories[i].KernelRepositoryReference)
		}
	})
	search.OnShortcut(0, ui.KeyDown, func() {
		a.coreProjectCursor = 0
		a.coreProjects.ScrollIntoView(0)
		a.coreProjects.Focus(c)
	}).OnSubmit(func() { selectProject(0) })
	if list.Submitted() {
		selectProject(a.coreProjectCursor)
	}
}

func (a *app) renderRepositoryMenu(m *ui.Menu, locale string, reference backend.KernelRepositoryReference) {
	for _, menu := range a.coreSnapshot.RepositoryMenus {
		if menu.Repository != reference {
			continue
		}
		renderNativeMenuItems(m, locale, menu.Items, func(path []int) {
			core, ctx := a.core, a.ctx
			a.queueCore(nativeOperation{call: func() (backend.KernelResult, error) {
				return core.ExecuteRepositoryMenu(ctx, reference, menu.Lease, path)
			}})
		})
		return
	}
}

// Preserve in-progress typing until the final ordered original reply arrives.
// A status refresh may carry an older filter while those edits are queued.
func (a *app) filterCoreProjects() {
	if a.core == nil || a.closed {
		return
	}
	text, core, ctx := a.coreProjectFilter, a.core, a.ctx
	a.coreProjectFilterPending++
	a.queueCore(nativeOperation{
		call: func() (backend.KernelResult, error) { return core.FilterRepositories(ctx, text) },
		complete: func(_ backend.KernelResult, err error) {
			a.coreProjectFilterPending--
			if err != nil {
				a.openErr = err
			}
		},
	})
}

// Preserve native dialogs/zoom without letting hidden Chromium claim focus or
// visibility. The application binds window methods at its actual lifecycle.
func (a *app) applyCoreZoom(c *ui.Context) {
	if a.core != nil {
		c.SetZoom(float32(a.coreZoom))
	}
}
