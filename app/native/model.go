package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
	"github.com/egoist/mygo/ui"
)

const (
	changesTab = iota
	historyTab
	historyPage = 100
)

// A draft is scoped to the backend's canonical repository path. It is only
// in-memory UI state; opening another project never stages or persists it.
type projectDraft struct {
	summary, description string
	included             map[string]bool
	descriptionOpen      bool
}

// app is owned by the UI thread. Background work snapshots its inputs, then
// returns through update (Window.Update in production). Sequence numbers also
// reject replies from backends that cannot interrupt an already-running read.
type app struct {
	windowPreferences     nativeWindowPreferences
	windowPreferencesPath string
	windowPreferenceErr   error
	prepareTray           func(bool) error
	quitting              bool
	quitDeferred          bool
	requestQuit           func()
	showWindow            func()
	backend               backend.Backend
	// The production native launch uses the original Desktop kernel. These are
	// immutable view snapshots and queued presentation requests, not another
	// repository/account/draft or Git state owner. backend remains a research
	// test seam until the old prototype is retired, never the default launcher.
	core                      *backend.Kernel
	coreSnapshot              *backend.KernelSnapshot
	coreQueue                 chan nativeOperation
	corePending               int
	coreDraftPending          int
	coreInclusionPending      int
	coreCommitPending         bool
	coreRemotePending         bool
	coreMissingPending        bool
	coreResizableFocus        bool
	coreGenerationPending     bool
	corePreviewDiff           *backend.Diff
	corePreviewIdentity       string
	coreProjects              ui.ListState
	coreProjectFilter         string
	coreProjectFilterPending  int
	coreProjectCursor         int
	coreProjectPaths          string
	coreHistoryExhausted      bool
	corePopupForm             kernelPopupForm
	coreDialogForms           map[int]*nativeDialogForm
	preferencesGeometry       preferenceGeometryCache
	coreOnboardingForm        kernelOnboardingForm
	coreSignInForm            kernelSignInForm
	coreInitialPath           string
	coreZoom                  float64
	coreWindowAction          func(string)
	corePresentationActions   []string
	coreGenerationChoicesOpen bool
	coreFocusTarget           string
	setCoreTheme              func(string)
	coreWindowState           func() backend.KernelWindowState
	update                    func(func())
	ctx                       context.Context
	cancel                    context.CancelFunc
	closed                    bool
	ready                     bool

	generationState
	nativeImageDiffState
	nativeWorkspaceState
	nativeCommitUI
	generationSettingsUI
	copilotLoginState

	commitCapable     bool
	savingVersion     bool
	commitSummary     string
	commitDescription string
	included          map[string]bool
	saveErr           error
	lastSavedSHA      string
	projectDrafts     map[string]projectDraft

	logo       ui.ImageSource
	logoErr    error
	serviceErr error
	openErr    error
	statusErr  error
	historyErr error
	commitErr  error
	diffErr    error

	repo      *backend.Repository
	status    *backend.Status
	commits   []backend.Commit
	changeset *backend.Changeset
	preview   *preview

	tab               int
	changeIndex       int
	commitIndex       int
	commitFileIndex   int
	changesList       nativeListState
	commitsList       nativeListState
	commitFilesList   nativeListState
	patchList         nativeListState
	reviewingFile     bool
	viewingCommit     bool
	descriptionOpen   bool
	branchDetailsOpen bool
	reviewOptionsOpen bool
	saveErrorOpen     bool
	diffLayout        int
	wrap              bool
	opening           bool
	loadingStatus     bool
	loadingHistory    bool
	loadingCommit     bool
	loadingDiff       bool
	historyLoaded     bool
	historyMore       bool
	historySkip       int
	openModal         bool
	settingsModal     bool
	pathDraft         string
	browsing          bool
	browseErr         error
	chooseDirectory   func(title, path string) ([]string, error)
	chooseFile        func(backend.KernelFileDialog) ([]string, error)
	setLocale         func(string)
	localeOverride    func() string
	syncMenu          func()

	openSeq       uint64
	openCancel    context.CancelFunc
	repoSeq       uint64
	statusSeq     uint64
	historySeq    uint64
	commitSeq     uint64
	diffSeq       uint64
	browseSeq     uint64
	repoContext   context.Context
	repoCancel    context.CancelFunc
	statusCancel  context.CancelFunc
	historyCancel context.CancelFunc
	commitCancel  context.CancelFunc
	diffCancel    context.CancelFunc
}

func newApp(b backend.Backend, logo ui.ImageSource) *app {
	ctx, cancel := context.WithCancel(context.Background())
	generation, _ := b.(backend.GenerationBackend)
	return &app{
		backend: b, logo: logo, ctx: ctx, cancel: cancel,
		generationState: generationState{generation: generation},
		changeIndex:     -1, commitIndex: -1, commitFileIndex: -1,
		wrap: true, descriptionOpen: true, included: make(map[string]bool),
		projectDrafts: make(map[string]projectDraft),
	}
}

// close runs on the UI thread. Child process reaping happens after App.Run,
// so shutdown never holds the UI thread while waiting on pipes.
func (a *app) close() {
	a.closed = true
	a.clearNativeImageDiff()
	a.pruneNativeDialogForms(backend.KernelSnapshot{})
	a.invalidateGeneration("")
	a.cancelCopilotLogin()
	a.cancel()
}

func (a *app) connect(initialPath string) {
	if a.core != nil {
		a.connectCore(initialPath)
		return
	}
	if a.backend == nil || a.serviceErr != nil {
		return
	}
	b, update := a.backend, a.update
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	go func() {
		defer cancel()
		info, err := b.Info(ctx)
		update(func() {
			if a.closed || a.serviceErr != nil {
				return
			}
			a.serviceErr, a.ready = err, err == nil
			a.commitCapable = err == nil && slices.Contains(info.Capabilities, "repository.commit")
			a.generationCapable = err == nil && a.generation != nil && slices.Contains(info.Capabilities, "repository.generateMessage")
			if a.generationCapable {
				a.loadGenerationSettings()
			}
			if a.ready && initialPath != "" {
				a.openRepository(initialPath)
			}
		})
	}()
}

func (a *app) serviceStopped(err error) {
	if a.closed {
		return
	}
	a.serviceErr, a.ready = err, false
	a.generationServiceStopped(err)
	if a.savingVersion {
		// The service might have saved the commit before its pipe failed.
		// Preserve the draft and require an explicit history check/retry.
		a.savingVersion, a.saveErr = false, err
	}
	if a.repoCancel != nil {
		a.repoCancel()
	}
	if a.openCancel != nil {
		a.openCancel()
	}
	a.openSeq++
	a.repoSeq++
	a.opening, a.loadingStatus, a.loadingHistory, a.loadingCommit = false, false, false, false
	a.clearDiff()
}

func (a *app) showOpen() {
	if a.savingVersion {
		return
	}
	a.openSeq++
	a.openModal, a.openErr, a.browseErr = true, nil, nil
	if a.repo != nil {
		a.pathDraft = a.repo.Path
	}
}

func (a *app) cancelOpen() {
	a.openModal, a.browsing = false, false
	a.browseSeq++
	if a.opening {
		a.openSeq++
		if a.openCancel != nil {
			a.openCancel()
		}
		a.opening = false
	}
}

func (a *app) browse(title string) {
	if a.chooseDirectory == nil || a.browsing {
		return
	}
	a.browsing, a.browseErr = true, nil
	a.browseSeq++
	seq, path, choose, update := a.browseSeq, a.pathDraft, a.chooseDirectory, a.update
	go func() {
		paths, err := choose(title, path)
		update(func() {
			if a.closed || seq != a.browseSeq {
				return
			}
			a.browsing, a.browseErr = false, err
			if a.openModal && err == nil && len(paths) > 0 {
				a.pathDraft = paths[0]
			}
		})
	}()
}

func (a *app) openRepository(path string) {
	if a.core != nil {
		a.openCoreRepository(path)
		return
	}
	path = strings.TrimSpace(path)
	if !a.ready || a.savingVersion || path == "" {
		return
	}
	a.invalidateGeneration(generationChanged)
	if a.openCancel != nil {
		a.openCancel()
	}
	a.openSeq++
	seq := a.openSeq
	a.opening, a.openErr = true, nil
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	a.openCancel = cancel
	b, update := a.backend, a.update
	go func() {
		defer cancel()
		repo, err := b.Open(ctx, path)
		update(func() {
			if a.closed || seq != a.openSeq {
				return
			}
			a.opening, a.openErr = false, err
			if err != nil {
				// Failed or canceled opens leave the previous project's reads,
				// selection and composer intact, rather than showing a blank page.
				return
			}
			a.rememberProjectDraft()
			if a.repoCancel != nil {
				a.repoCancel()
			}
			a.repoSeq++
			a.repoContext, a.repoCancel = context.WithCancel(a.ctx)
			a.repo, a.status, a.changeset, a.commits = &repo, nil, nil, nil
			a.statusErr, a.historyErr, a.commitErr = nil, nil, nil
			a.changeIndex, a.commitIndex, a.commitFileIndex = -1, -1, -1
			a.changesList, a.commitsList, a.commitFilesList = nativeListState{}, nativeListState{}, nativeListState{}
			a.loadingStatus, a.loadingHistory, a.loadingCommit = false, false, false
			a.historyLoaded, a.historyMore, a.historySkip = false, false, 0
			a.reviewingFile, a.viewingCommit, a.branchDetailsOpen, a.reviewOptionsOpen = false, false, false, false
			a.openModal, a.saveErr, a.lastSavedSHA = false, nil, ""
			a.restoreProjectDraft()
			a.clearDiff()
			a.loadStatus()
			if a.tab == historyTab {
				a.loadHistory(false)
			}
		})
	}()
}

func (a *app) rememberProjectDraft() {
	if a.repo != nil {
		a.projectDrafts[a.repo.Path] = projectDraft{
			summary: a.commitSummary, description: a.commitDescription,
			included: maps.Clone(a.included), descriptionOpen: a.descriptionOpen,
		}
	}
}

func (a *app) restoreProjectDraft() {
	draft, exists := a.projectDrafts[a.repo.Path]
	if !exists {
		// New projects expose the optional description; returning projects keep
		// the user's explicit disclosure choice along with their draft.
		draft.descriptionOpen = true
	}
	a.commitSummary, a.commitDescription, a.descriptionOpen = draft.summary, draft.description, draft.descriptionOpen
	a.included = make(map[string]bool, len(draft.included))
	maps.Copy(a.included, draft.included)
}

func (a *app) refresh() {
	if a.core != nil {
		a.refreshCore()
		return
	}
	if !a.ready || a.repo == nil || a.savingVersion {
		return
	}
	a.invalidateGeneration(generationChanged)
	a.clearDiff()
	a.loadStatus()
	if a.tab == historyTab {
		a.loadHistory(false)
	} else {
		// History may have changed; do not reuse it on the next tab switch.
		a.historyLoaded = false
		if a.historyCancel != nil {
			a.historyCancel()
		}
		a.historySeq++
		a.loadingHistory = false
	}
}

func (a *app) loadStatus() {
	if a.core != nil {
		a.refreshCore()
		return
	}
	if a.statusCancel != nil {
		a.statusCancel()
	}
	a.statusSeq++
	seq, repoSeq, id := a.statusSeq, a.repoSeq, a.repo.ID
	a.loadingStatus, a.statusErr = true, nil
	ctx, cancel := context.WithTimeout(a.repoContext, 30*time.Second)
	a.statusCancel = cancel
	b, update := a.backend, a.update
	go func() {
		defer cancel()
		status, err := b.Status(ctx, id)
		update(func() {
			if a.closed || repoSeq != a.repoSeq || seq != a.statusSeq {
				return
			}
			a.loadingStatus, a.statusErr = false, err
			if err != nil {
				return
			}
			path := ""
			if a.status != nil && a.changeIndex >= 0 && a.changeIndex < len(a.status.Files) {
				path = a.status.Files[a.changeIndex].Path
			}
			a.status = &status
			present := make(map[string]bool, len(status.Files))
			for _, file := range status.Files {
				present[file.Path] = true
			}
			for path := range a.included {
				if !present[path] {
					delete(a.included, path)
				}
			}
			a.changeIndex = fileIndex(status.Files, path)
			if a.tab == changesTab {
				if a.reviewingFile && !present[path] {
					a.reviewingFile = false
				}
				a.selectFile(a.changeIndex)
			}
		})
	}()
}

func (a *app) setTab(tab int) {
	if a.core != nil {
		a.setCoreTab(tab)
		return
	}
	if tab == a.tab {
		return
	}
	a.tab = tab
	a.clearDiff()
	if !a.ready || a.repo == nil {
		return
	}
	if tab == changesTab {
		a.selectFile(a.changeIndex)
	} else if !a.historyLoaded {
		a.loadHistory(false)
	} else if a.changeset == nil && a.commitIndex >= 0 {
		a.selectCommit(a.commitIndex)
	} else {
		a.selectFile(a.commitFileIndex)
	}
}

func (a *app) loadHistory(more bool) {
	if a.core != nil {
		a.loadCoreHistory()
		return
	}
	if more && a.loadingHistory {
		return
	}
	if a.historyCancel != nil {
		a.historyCancel()
	}
	a.historySeq++
	seq, repoSeq, id, skip := a.historySeq, a.repoSeq, a.repo.ID, 0
	if more {
		skip = a.historySkip
	} else {
		if a.commitCancel != nil {
			a.commitCancel()
		}
		a.commitSeq++
		a.commits, a.changeset, a.commitErr = nil, nil, nil
		a.commitIndex, a.commitFileIndex = -1, -1
		a.commitsList, a.commitFilesList = nativeListState{}, nativeListState{}
		a.historyLoaded, a.loadingCommit = false, false
		if a.tab == historyTab {
			a.reviewingFile, a.viewingCommit = false, false
			a.clearDiff()
		}
	}
	a.loadingHistory, a.historyErr = true, nil
	ctx, cancel := context.WithTimeout(a.repoContext, 30*time.Second)
	a.historyCancel = cancel
	b, update := a.backend, a.update
	go func() {
		defer cancel()
		commits, err := b.History(ctx, id, skip, historyPage)
		update(func() {
			if a.closed || repoSeq != a.repoSeq || seq != a.historySeq {
				return
			}
			a.loadingHistory, a.historyErr = false, err
			if err != nil {
				return
			}
			a.historyLoaded, a.historyMore = true, len(commits) == historyPage
			a.historySkip = skip + len(commits)
			if more {
				// External commits can move offset-based pages while browsing.
				// Keep row identities unique without losing pagination progress.
				seen := make(map[string]bool, len(a.commits))
				for _, commit := range a.commits {
					seen[commit.SHA] = true
				}
				for _, commit := range commits {
					if !seen[commit.SHA] {
						a.commits = append(a.commits, commit)
						seen[commit.SHA] = true
					}
				}
			} else {
				a.commits = commits
				if len(commits) > 0 {
					a.commitIndex = 0
					if a.tab == historyTab {
						a.selectCommit(0)
					}
				}
			}
		})
	}()
}

func (a *app) selectCommit(index int) {
	if a.core != nil {
		a.selectCoreCommit(index)
		return
	}
	if !a.ready || a.repo == nil || index < 0 || index >= len(a.commits) {
		return
	}
	if a.commitCancel != nil {
		a.commitCancel()
	}
	a.commitSeq++
	seq, repoSeq, id, sha := a.commitSeq, a.repoSeq, a.repo.ID, a.commits[index].SHA
	a.commitIndex, a.commitFileIndex = index, -1
	a.changeset, a.commitErr, a.loadingCommit = nil, nil, true
	a.commitFilesList = nativeListState{}
	a.clearDiff()
	ctx, cancel := context.WithTimeout(a.repoContext, 30*time.Second)
	a.commitCancel = cancel
	b, update := a.backend, a.update
	go func() {
		defer cancel()
		changeset, err := b.Changeset(ctx, id, sha)
		update(func() {
			if a.closed || repoSeq != a.repoSeq || seq != a.commitSeq {
				return
			}
			a.loadingCommit, a.commitErr = false, err
			if err != nil {
				return
			}
			a.changeset = &changeset
			a.commitFileIndex = fileIndex(changeset.Files, "")
			if a.tab == historyTab {
				a.selectFile(a.commitFileIndex)
			}
		})
	}()
}

func fileIndex(files []backend.File, path string) int {
	for i, file := range files {
		if file.Path == path {
			return i
		}
	}
	if len(files) > 0 {
		return 0
	}
	return -1
}

func (a *app) currentFile() (backend.File, string, bool) {
	if a.tab == changesTab && a.status != nil && a.changeIndex >= 0 && a.changeIndex < len(a.status.Files) {
		return a.status.Files[a.changeIndex], "", true
	}
	if a.tab == historyTab && a.changeset != nil && a.commitIndex >= 0 && a.commitIndex < len(a.commits) && a.commitFileIndex >= 0 && a.commitFileIndex < len(a.changeset.Files) {
		return a.changeset.Files[a.commitFileIndex], a.commits[a.commitIndex].SHA, true
	}
	return backend.File{}, "", false
}

func (a *app) clearDiff() {
	a.clearNativeImageDiff()
	if a.diffCancel != nil {
		a.diffCancel()
	}
	a.diffSeq++
	a.preview, a.diffErr, a.loadingDiff = nil, nil, false
	a.patchList = nativeListState{}
}

func (a *app) selectFile(index int) {
	if a.core != nil {
		a.selectCoreFile(index)
		return
	}
	if a.tab == changesTab {
		a.changeIndex = index
	} else {
		a.commitFileIndex = index
	}
	a.clearDiff()
	file, commit, ok := a.currentFile()
	if !ok || !a.ready || a.repo == nil {
		return
	}
	seq, repoSeq, id := a.diffSeq, a.repoSeq, a.repo.ID
	a.loadingDiff = true
	ctx, cancel := context.WithTimeout(a.repoContext, 30*time.Second)
	a.diffCancel = cancel
	b, update := a.backend, a.update
	go func() {
		defer cancel()
		diff, err := b.Diff(ctx, id, file.Path, commit)
		var prepared *preview
		if err == nil {
			// Parsing/decoding a large patch or image is not UI-thread work.
			prepared, err = preparePreview(diff, file.Path)
		}
		update(func() {
			if a.closed || repoSeq != a.repoSeq || seq != a.diffSeq {
				return
			}
			a.preview, a.diffErr, a.loadingDiff = prepared, err, false
		})
	}()
}
