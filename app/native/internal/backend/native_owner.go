package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/OUBIGFA/Gitowl/app/native/internal/gitcli"
)

// NativeOwner is the single in-process owner of repository presentation state.
// Existing native controls keep their typed contract; no Node process or pipe
// participates in its commands, storage, Git execution or snapshots.
type NativeOwner struct {
	mu                  sync.Mutex
	store               *NativeStore
	git                 *gitcli.Engine
	state               KernelSnapshot
	published           atomic.Pointer[KernelSnapshot]
	current             *gitcli.Repository
	status              gitcli.Status
	include             map[string]bool
	onEvent             func(KernelEvent)
	ctx                 context.Context
	cancel              context.CancelFunc
	done                chan struct{}
	active              atomic.Int32
	closed              bool
	delay               time.Duration
	watchCancel         context.CancelFunc
	watchError          error
	interactionMu       sync.Mutex
	interactionID       int
	answers             map[int]chan nativeAnswer
	modalAnswers        map[int]chan nativeAnswer
	accounts            []nativeAccount
	credentialNamespace string
	userData            string
	component           *nativeComponent
	cancelMu            sync.Mutex
	generationCancel    context.CancelFunc
	resources           string
	oauthConfig         nativeOAuthConfig
	oauthPending        *nativeOAuthPending
	partial             map[string]nativePartialSelection
	media               nativeMedia
	copilotModels       map[string][]ProviderModel
	confirmAction       func(context.Context) error
	recycle             func(string) error
	httpClient          *http.Client
	tags                map[string][]string
	metadataStamp       string
	refreshWake         chan struct{}
	windowFocused       atomic.Bool
	starting            bool
}

func NewNativeKernel(ctx context.Context, git, userData string, onEvent func(KernelEvent)) (*Kernel, error) {
	resources := filepath.Clean(filepath.Join(filepath.Dir(filepath.Dir(git)), ".."))
	if err := prepareNativeLegacy(ctx, resources, userData); err != nil {
		return nil, err
	}
	store, err := OpenNativeStore(ctx, userData)
	if err != nil {
		return nil, err
	}
	owned, cancel := context.WithCancel(ctx)
	n := &NativeOwner{store: store, git: gitcli.New(git, gitcli.DefaultCacheBytes), onEvent: onEvent, ctx: owned, cancel: cancel, done: make(chan struct{}), include: make(map[string]bool), answers: make(map[int]chan nativeAnswer)}
	repositories, err := store.Repositories(ctx)
	if err != nil {
		store.Close()
		cancel()
		return nil, err
	}
	n.state = KernelSnapshot{Protocol: 1, Core: "gitowl/native", Revision: 1, Repositories: repositories, Accounts: []KernelAccount{}, Popups: []KernelPopup{}, Appearance: &KernelAppearance{Theme: "system"}, CommitGeneration: &KernelGenerationSettings{Providers: []Provider{}, EnabledModels: []KernelEnabledProviderModel{}, Source: "copilot", Language: "zh-Hans"}}
	n.state.Menu = n.menu()
	n.credentialNamespace = "io.github.oubigfa.gitowl"
	n.copilotModels = make(map[string][]ProviderModel)
	n.modalAnswers = make(map[int]chan nativeAnswer)
	if os.Getenv("TEST_ENV") == "1" {
		digest := sha256.Sum256([]byte(filepath.Join(userData, "desktop-kernel")))
		n.credentialNamespace = fmt.Sprintf("io.github.oubigfa.gitowl.test.%x", digest[:])
	}
	n.userData = userData
	n.starting = true
	n.refreshWake = make(chan struct{}, 1)
	n.httpClient = nativeHTTPClient()
	n.recycle = func(path string) error { _, err := n.platform(n.ctx, "move-to-trash", path); return err }
	n.resources = filepath.Clean(filepath.Join(filepath.Dir(filepath.Dir(git)), ".."))
	n.media.executable = filepath.Join(n.resources, "imagemagick", "magick.exe")
	n.initComponent(filepath.Join(filepath.Dir(filepath.Dir(git)), "..", "copilot-runtime.json"))
	if err := n.loadAccounts(ctx); err != nil {
		store.Close()
		cancel()
		return nil, err
	}
	if err := n.loadProviders(ctx); err != nil {
		store.Close()
		cancel()
		return nil, err
	}
	if theme, err := store.Get(ctx, "theme"); err == nil && theme != "" {
		n.state.Appearance.Theme = theme
	}
	if imageMode, err := store.Get(ctx, "image-diff-type"); err == nil {
		n.state.ImageDiffType, _ = strconv.Atoi(imageMode)
	}
	if shown, err := store.Get(ctx, "has-shown-welcome-flow"); err != nil {
		store.Close()
		cancel()
		return nil, err
	} else if shown != "1" {
		if err := n.welcome(ctx, "Start"); err != nil {
			store.Close()
			cancel()
			return nil, err
		}
	}
	n.publish()
	k := &Kernel{native: n, onEvent: onEvent, closeDone: make(chan struct{})}
	if last, err := store.Get(ctx, "last-selected-repository-id"); err != nil {
		k.Close()
		return nil, err
	} else {
		for _, r := range repositories {
			if strconv.Itoa(r.ID) == last {
				if err := n.selectRepository(ctx, r.KernelRepositoryReference); err != nil {
					k.Close()
					return nil, err
				}
				n.publish()
				break
			}
		}
	}
	n.starting = false
	return k, nil
}

func (n *NativeOwner) publish() {
	n.state.Menu = n.menu()
	n.workspaceMenus()
	if previous := n.published.Load(); previous != nil && reflect.DeepEqual(n.state, *previous) {
		return
	}
	n.state.Revision++
	// A snapshot is immutable after publication. Clone at the state boundary,
	// not per frame; unchanged background checks never enter this path.
	data, err := json.Marshal(n.state)
	if err != nil {
		panic(err)
	}
	var snapshot KernelSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		panic(err)
	}
	n.published.Store(&snapshot)
	if n.onEvent != nil {
		n.onEvent(KernelEvent{Snapshot: &snapshot})
	}
}
func (n *NativeOwner) snapshot() KernelSnapshot { return *n.published.Load() }

type nativeCommand struct {
	Name                        string                    `json:"name"`
	Repository                  KernelRepositoryReference `json:"repository"`
	Paths                       []string                  `json:"paths"`
	Text                        string                    `json:"text"`
	Section                     string                    `json:"section"`
	FileIDs                     []string                  `json:"fileIDs"`
	FileID                      string                    `json:"fileID"`
	SHAs                        []string                  `json:"shas"`
	Included                    bool                      `json:"included"`
	Enabled                     bool                      `json:"enabled"`
	Message                     KernelMessage             `json:"message"`
	Context                     KernelCommitContext       `json:"context"`
	Options                     KernelCommitOptions       `json:"options"`
	Action                      json.RawMessage           `json:"action"`
	ID                          string                    `json:"id"`
	PopupID                     int                       `json:"popupID"`
	Reply                       KernelPopupReply          `json:"reply"`
	NameValue, Email, Step, URL string
	Provider                    ProviderSettings         `json:"provider"`
	ProviderID                  string                   `json:"providerId"`
	ProviderName                string                   `json:"providerName"`
	ModelID                     string                   `json:"modelId"`
	Secret                      *string                  `json:"secret"`
	Source                      string                   `json:"source"`
	Selection                   *KernelProviderSelection `json:"selection"`
	Language                    string                   `json:"language"`
	Endpoint                    string                   `json:"endpoint"`
	Models                      []ProviderDeployment     `json:"models"`
	Identity                    string                   `json:"identity"`
	LineIDs                     []int                    `json:"lineIDs"`
	ImageDiffType               int                      `json:"imageDiffType"`
	Mode, Query, Lease          string
	Path                        []int
	Override                    bool `json:"overrideExistingMessage"`
}

func (n *NativeOwner) command(ctx context.Context, value any) (KernelResult, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return KernelResult{}, err
	}
	var command nativeCommand
	if err = json.Unmarshal(data, &command); err != nil {
		return KernelResult{}, err
	}
	if command.Name == "state" {
		return KernelResult{Snapshot: n.snapshot()}, nil
	}
	if command.Name == "popup.reply" || command.Name == "popup.close" {
		if handled, err := n.answerModal(command); handled {
			return KernelResult{Snapshot: n.snapshot()}, err
		}
	}
	if command.Name == "repository.generate.cancel" {
		n.cancelMu.Lock()
		if n.generationCancel != nil {
			n.generationCancel()
		}
		n.cancelMu.Unlock()
		return KernelResult{Snapshot: n.snapshot()}, nil
	}
	n.active.Add(1)
	defer n.active.Add(-1)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return KernelResult{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return KernelResult{}, err
	}
	if command.Name != "repository.select" && command.Name != "repository.missing" && command.Name != "repository.menu" && command.Repository.Path != "" && (n.state.Selected == nil || command.Repository != n.state.Selected.Repository.KernelRepositoryReference) {
		return KernelResult{}, errors.New("repository selection changed")
	}
	result := KernelResult{}
	if n.state.Selected == nil && (strings.HasPrefix(command.Name, "history.") || strings.HasPrefix(command.Name, "commit.") || command.Name == "workspace.action" || strings.HasPrefix(command.Name, "repository.") && command.Name != "repository.select" && command.Name != "repository.missing" && command.Name != "repository.menu") {
		return result, errors.New("no repository selected")
	}
	switch command.Name {
	case "imageDiff.type":
		if command.ImageDiffType < 0 || command.ImageDiffType > 3 {
			err = errors.New("invalid image comparison mode")
		} else {
			n.state.ImageDiffType = command.ImageDiffType
			err = n.store.Set(ctx, "image-diff-type", strconv.Itoa(command.ImageDiffType))
		}
	case "commit.mode":
		if command.Mode != "manual" {
			err = errors.New("automatic multi-commit grouping is not available in the native Git workflow")
		} else {
			n.state.Selected.CommitView.Mode = "manual"
		}
	case "commit.coAuthors.show", "commit.coAuthors.search", "commit.coAuthors.add", "commit.coAuthors.remove":
		err = n.coAuthors(ctx, command)
	case "repository.menu":
		err = n.repositoryMenuCommand(ctx, command)
	case "repositories.add":
		for _, path := range command.Paths {
			repository, e := n.add(ctx, path)
			if e != nil {
				return result, e
			}
			result.AddedRepositories = append(result.AddedRepositories, repository.KernelRepositoryReference)
		}
	case "repository.select":
		err = n.selectRepository(ctx, command.Repository)
	case "repository.missing":
		err = n.recoverMissing(ctx, command)
	case "repositories.filter":
		paths := []string{}
		for _, repository := range n.state.Repositories {
			name := repository.Name
			if repository.Alias != nil {
				name = *repository.Alias
			}
			if strings.Contains(strings.ToLower(name), strings.ToLower(command.Text)) {
				paths = append(paths, repository.Path)
			}
		}
		n.state.RepositoryFilter = &KernelRepositoryFilter{Text: command.Text, Paths: paths}
	case "repository.refresh", "repository.refreshChanges":
		_, err = n.refresh(ctx, true)
	case "repository.section":
		n.state.Selected.Section = command.Section
		if command.Section == "history" {
			err = n.history(ctx, 0)
		}
	case "history.loadNext":
		err = n.history(ctx, len(n.state.Selected.History))
	case "history.select":
		n.state.Selected.HistorySelection = command.SHAs
		err = n.historyFiles(ctx)
	case "history.file.select":
		n.state.Selected.HistoryFileID = &command.FileID
		err = n.selectDiff(ctx, command.FileID, true)
	case "history.summaries":
		for _, sha := range command.SHAs {
			var count, added, deleted int
			count, added, deleted, err = n.current.Summary(ctx, sha)
			if err != nil {
				break
			}
			result.HistorySummaries = append(result.HistorySummaries, KernelHistorySummary{sha, count, added, deleted})
		}
	case "repository.files.select":
		n.state.Selected.SelectedFileIDs = command.FileIDs
		if len(command.FileIDs) > 0 {
			err = n.selectDiff(ctx, command.FileIDs[0], false)
		} else {
			n.state.Selected.Diff = nil
		}
	case "repository.files.include", "repository.files.includeAll":
		for _, file := range n.state.Selected.Files {
			if command.Name == "repository.files.includeAll" || slices.Contains(command.FileIDs, file.ID) {
				n.include[file.Path] = command.Included
				delete(n.partial, file.Path)
			}
		}
		n.updateInclusions()
	case "repository.message":
		n.state.Selected.Message = command.Message
		err = n.saveDraft(ctx)
	case "commit.options":
		view := n.state.Selected.CommitView
		if v := command.Options.AllowEmpty; v != nil {
			view.AllowEmpty = *v
		}
		if v := command.Options.SignOff; v != nil {
			view.SignOff = *v
		}
		if v := command.Options.SkipHooks; v != nil {
			view.SkipHooks = *v
		}
	case "commit.amend":
		n.state.Selected.CommitView.Amending = command.Enabled
		if command.Enabled {
			commits, e := n.current.History(ctx, "HEAD", 0, 1)
			if e != nil {
				err = e
			} else if len(commits) == 1 {
				c := commits[0]
				n.state.Selected.Message = KernelMessage{Summary: c.Subject, Description: &c.Body}
				err = n.saveDraft(ctx)
			}
		}
	case "repository.commit":
		if err = n.refreshRecovery(); err != nil {
			break
		}
		if n.state.Workspace.Banner != nil && n.state.Workspace.Banner.Type == "native-recovery" {
			err = n.recoveryDialog()
			if err == nil {
				err = errors.New("resolve the previous commit result before creating another commit")
			}
			break
		}
		paths := []gitcli.FileSelection{}
		for _, file := range n.state.Selected.Files {
			if n.include[file.Path] {
				selection := gitcli.FileSelection{Path: file.Path}
				if file.OldPath != nil {
					selection.OldPath = *file.OldPath
				}
				if partial, ok := n.partial[file.Path]; ok {
					selection.Identity = partial.identity
					selection.Patch = partial.patch
					selection.Lines = partial.lines
				}
				paths = append(paths, selection)
			}
		}
		message := command.Context.Summary
		if command.Context.Description != nil && *command.Context.Description != "" {
			message += "\n\n" + *command.Context.Description
		}
		if len(command.Context.Trailers) > 0 {
			message += "\n"
			for _, trailer := range command.Context.Trailers {
				if strings.ContainsAny(trailer.Token+trailer.Value, "\r\n\x00") || strings.TrimSpace(trailer.Token) == "" {
					return result, errors.New("invalid commit trailer")
				}
				message += "\n" + trailer.Token + ": " + trailer.Value
			}
		}
		sha, commitErr := n.current.CommitSelected(ctx, paths, message, command.Context.Amend, n.state.Selected.CommitView.AllowEmpty, n.state.Selected.CommitView.SignOff, n.state.Selected.CommitView.SkipHooks)
		err = commitErr
		if sha != "" {
			committed := true
			result.Committed = &committed
			n.state.Selected.Message = KernelMessage{}
			err = errors.Join(err, n.saveDraft(ctx))
			n.partial = make(map[string]nativePartialSelection)
			if err == nil && command.Context.PushAfterCommit {
				err = n.runRemote(ctx, "push")
			}
		}
		_, refreshErr := n.refresh(ctx, true)
		err = errors.Join(err, refreshErr, n.refreshRecovery())
	case "repository.fetch", "repository.pull", "repository.push":
		operation := strings.TrimPrefix(command.Name, "repository.")
		args := []string{operation}
		if operation == "fetch" {
			args = []string{"-c", "fetch.followRemoteHEAD=always", "fetch"}
		}
		err = n.runRemote(ctx, args...)
		_, refreshErr := n.refresh(ctx, true)
		err = errors.Join(err, refreshErr)
	case "presentation.resizable-focus", "presentation.repository-list-focused", "presentation.commit-message-focused":
		n.state.FocusCommitMessage = false
	case "welcome.advance":
		err = n.welcome(ctx, command.Step)
	case "welcome.configureGit":
		err = n.configureAuthor(ctx, command.NameValue, command.Email)
	case "popup.close":
		n.confirmAction = nil
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
	case "menu.execute":
		err = n.executeMenu(ctx, command.ID)
	case "popup.reply":
		err = n.replyForm(ctx, command.Reply)
	case "workspace.action":
		err = n.workspace(ctx, command.Action)
	case "repository.generate":
		err = n.requestGeneration(ctx, command)
	case "signIn.dotcom":
		err = n.beginSignIn(ctx, "https://api.github.com")
	case "signIn.enterprise":
		n.state.SignIn = &KernelSignIn{Kind: "EndpointEntry"}
		n.form("SignIn", "", nil)
		n.state.CurrentPopup.Layout = ""
	case "signIn.endpoint":
		endpoint := strings.TrimSuffix(command.Endpoint, "/")
		if !strings.HasSuffix(endpoint, "/api/v3") && !strings.HasSuffix(endpoint, "api.github.com") {
			endpoint += "/api/v3"
		}
		if _, e := htmlEndpoint(endpoint); e != nil {
			err = e
		} else {
			n.state.SignIn = &KernelSignIn{Kind: "Authentication", Endpoint: &endpoint}
		}
	case "signIn.browser":
		if n.state.SignIn == nil || n.state.SignIn.Endpoint == nil {
			err = errors.New("select an account endpoint")
		} else {
			err = n.beginSignIn(ctx, *n.state.SignIn.Endpoint)
		}
	case "signIn.reset":
		n.oauthPending = nil
		n.state.SignIn = nil
	case "launch.url":
		err = n.launchURL(ctx, command.URL)
	case "launch.cli":
		var action struct{ Kind, Path, URL, Branch string }
		if e := json.Unmarshal(command.Action, &action); e != nil {
			err = e
			break
		}
		if action.Kind == "open-repository" {
			var repository KernelRepository
			repository, err = n.add(ctx, action.Path)
			if err == nil {
				err = n.selectRepository(ctx, repository.KernelRepositoryReference)
			}
		} else if action.Kind == "clone-url" {
			n.cloneForm(action.URL, action.Branch)
		} else {
			err = errors.New("unknown native launch action")
		}
	case "generation.provider.save", "generation.provider.rename", "generation.provider.delete", "generation.provider.models.refresh", "generation.provider.models.enabled", "generation.provider.deployments.set", "generation.select", "generation.language":
		err = n.providerCommand(ctx, command)
	case "generation.copilot.settings":
		n.preferences("copilot")
	case "repository.lines.include":
		err = n.includeLines(command)
	default:
		err = fmt.Errorf("unknown native command: %s", command.Name)
	}
	if n.state.Selected != nil {
		n.updateInclusions()
	}
	n.publish()
	result.Snapshot = n.snapshot()
	return result, err
}

func (n *NativeOwner) add(ctx context.Context, path string) (KernelRepository, error) {
	// Reopening an already registered root need not rediscover it once here
	// and again during selection. Selection still verifies Git and its state.
	if absolute, err := filepath.Abs(path); err == nil {
		for _, repository := range n.state.Repositories {
			if filepath.Clean(repository.Path) == filepath.Clean(absolute) {
				return repository, nil
			}
		}
	}
	r, err := n.git.Open(ctx, path)
	if err != nil {
		return KernelRepository{}, err
	}
	id := 1
	for _, existing := range n.state.Repositories {
		if existing.Path == r.Path {
			return existing, nil
		}
		id = max(id, existing.ID+1)
	}
	repository := KernelRepository{KernelRepositoryReference: KernelRepositoryReference{id, r.Path}, Name: filepath.Base(r.Path), Kind: "repository"}
	if err := n.store.SaveRepository(ctx, repository); err != nil {
		return KernelRepository{}, err
	}
	n.state.Repositories = append(n.state.Repositories, repository)
	return repository, nil
}
func textPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func (n *NativeOwner) selectRepository(ctx context.Context, reference KernelRepositoryReference) error {
	if n.current != nil && n.state.Selected != nil && n.state.Selected.Repository.KernelRepositoryReference == reference {
		_, err := n.refresh(ctx, false)
		return err
	}
	n.confirmAction = nil
	if popup := n.state.CurrentPopup; popup != nil && (popup.Type == "NativeConfirm" || popup.Type == "NativeRecovery") {
		n.state.CurrentPopup = nil
		n.state.Popups = []KernelPopup{}
	}
	var selected *KernelRepository
	for i := range n.state.Repositories {
		if n.state.Repositories[i].KernelRepositoryReference == reference {
			selected = &n.state.Repositories[i]
			break
		}
	}
	if selected == nil {
		return errors.New("repository is no longer registered")
	}
	r, err := n.git.Open(ctx, reference.Path)
	if err != nil {
		if n.watchCancel != nil {
			n.watchCancel()
		}
		n.current = nil
		n.state.Selected = nil
		n.state.Workspace = nil
		message := err.Error()
		n.state.MissingRepository = &KernelMissingRepository{Repository: *selected, Error: &message}
		if strings.Contains(message, "detected dubious ownership") {
			n.state.MissingRepository.UnsafePath = &selected.Path
		}
		return nil
	}
	n.state.MissingRepository = nil
	n.current = r
	n.media.clear()
	n.status = gitcli.Status{}
	n.include = make(map[string]bool)
	n.partial = make(map[string]nativePartialSelection)
	n.state.Selected = &KernelSelected{Repository: *selected, Section: "changes", Files: []KernelFile{}, SelectedFileIDs: []string{}, History: []Commit{}, HistorySelection: []string{}, HistoryFiles: []KernelFile{}, CommitView: &KernelCommitView{Mode: "manual", CoAuthorsAvailable: true, ShowSkipHooks: true, Assisted: KernelAssistedState{Kind: "idle"}}}
	n.state.Workspace = &KernelWorkspace{Repository: &reference, Navigation: "", CheckMarks: true, Branches: []KernelWorkspaceBranchGroup{}, Worktrees: []KernelWorkspaceWorktree{}, Menus: []KernelWorkspaceMenu{}, VisibleFileIDs: []string{}, Compare: KernelWorkspaceCompare{Mode: "history", Operation: "merge"}}
	draft, err := n.store.Get(ctx, "draft:"+reference.Path)
	if err != nil {
		return err
	}
	if draft != "" {
		if err := json.Unmarshal([]byte(draft), &n.state.Selected.Message); err != nil {
			return err
		}
	}
	_, err = n.refresh(ctx, true)
	if err == nil {
		err = n.refreshRecovery()
		n.watchRepository()
	}
	return errors.Join(err, n.store.Set(ctx, "last-selected-repository-id", strconv.Itoa(reference.ID)))
}
func fileStatus(file gitcli.File) string {
	if file.Conflict {
		return "conflicted"
	}
	code := file.Index + file.Worktree
	for _, v := range []struct{ letter, name string }{{"?", "new"}, {"R", "renamed"}, {"D", "deleted"}, {"A", "new"}, {"T", "modified"}} {
		if strings.Contains(code, v.letter) {
			return v.name
		}
	}
	return "modified"
}
func (n *NativeOwner) refresh(ctx context.Context, full bool) (bool, error) {
	if n.current == nil {
		return false, nil
	}
	status, err := n.current.Status(ctx)
	if err != nil {
		return false, err
	}
	changed := status.Fingerprint != n.status.Fingerprint
	if !changed && !full {
		return false, nil
	}
	before := n.state.Selected.HeadSHA
	n.status = status
	selected := n.state.Selected
	selected.Branch = textPointer(status.Branch)
	selected.HeadSHA = textPointer(status.Head)
	selected.Upstream = textPointer(status.Upstream)
	selected.Ahead = status.Ahead
	selected.Behind = status.Behind
	selected.ConflictKind = textPointer(status.Operation)
	selected.Files = []KernelFile{}
	for _, file := range status.Files {
		if _, ok := n.include[file.Path]; !ok {
			n.include[file.Path] = true
		}
		selected.Files = append(selected.Files, KernelFile{ID: file.Path, Path: file.Path, Status: fileStatus(file), OldPath: textPointer(file.OldPath), IsSubmodule: strings.HasPrefix(file.Submodule, "S")})
	}
	n.updateInclusions()
	if full || !reflect.DeepEqual(before, selected.HeadSHA) {
		stamp, err := n.current.MetadataFingerprint()
		if err != nil {
			return false, err
		}
		metadata, err := n.readMetadata(ctx)
		if err != nil {
			return false, err
		}
		groups := []KernelWorkspaceBranchGroup{{Group: "local", Items: []KernelWorkspaceBranch{}}, {Group: "remote", Items: []KernelWorkspaceBranch{}}}
		n.tags = map[string][]string{}
		for _, ref := range metadata.refs {
			if strings.HasPrefix(ref.Name, "refs/tags/") {
				sha := ref.ID
				if ref.Peeled != "" {
					sha = ref.Peeled
				}
				n.tags[sha] = append(n.tags[sha], strings.TrimPrefix(ref.Name, "refs/tags/"))
				continue
			}
			remote := strings.HasPrefix(ref.Name, "refs/remotes/")
			index := 0
			prefix := "refs/heads/"
			if remote {
				index = 1
				prefix = "refs/remotes/"
			}
			groups[index].Items = append(groups[index].Items, KernelWorkspaceBranch{Ref: ref.Name, Name: strings.TrimPrefix(ref.Name, prefix), Remote: remote, Current: ref.Current, Tip: ref.ID})
		}
		n.state.Workspace.Branches = groups
		if err := n.refreshWorkspaceMetadata(metadata.worktrees, metadata.stash); err != nil {
			return false, err
		}
		selected.HasRemote = len(metadata.remotes) > 0
		if metadata.authorErr == nil {
			identity := string(metadata.author)
			if start := strings.LastIndex(identity, " <"); start >= 0 {
				if end := strings.Index(identity[start+2:], ">"); end >= 0 {
					selected.CommitView.Author = &KernelGitAuthor{Name: identity[:start], Email: identity[start+2 : start+2+end]}
				}
			}
		}
		if selected.Section == "history" {
			if err := n.history(ctx, 0); err != nil {
				return false, err
			}
		}
		n.metadataStamp = stamp
	}
	if selected.Section == "changes" && len(selected.SelectedFileIDs) > 0 {
		valid := slices.ContainsFunc(selected.Files, func(file KernelFile) bool { return file.ID == selected.SelectedFileIDs[0] })
		if !valid {
			selected.SelectedFileIDs = []string{}
			selected.Diff = nil
			selected.DiffSelection = nil
		} else if err := n.selectDiff(ctx, selected.SelectedFileIDs[0], false); err != nil {
			return false, err
		}
	}
	for i := range n.state.Repositories {
		if n.state.Repositories[i].Path == n.current.Path {
			n.state.Repositories[i].Local = &KernelRepositoryLocal{Ahead: &status.Ahead, Behind: &status.Behind, ChangedFiles: len(status.Files)}
		}
	}
	return changed, nil
}
func (n *NativeOwner) updateInclusions() {
	s := n.state.Selected
	if s == nil {
		return
	}
	count := 0
	ids := []string{}
	for i := range s.Files {
		file := &s.Files[i]
		file.Inclusion = "none"
		if n.include[file.Path] {
			file.Inclusion = "all"
			if _, ok := n.partial[file.Path]; ok {
				file.Inclusion = "partial"
			}
			count++
		}
		filter := n.state.Workspace.FilesFilter
		matches := filter.FilterText == "" || strings.Contains(strings.ToLower(file.Path), strings.ToLower(filter.FilterText))
		if filter.Included && !n.include[file.Path] || filter.Excluded && n.include[file.Path] {
			matches = false
		}
		if filter.New || filter.Modified || filter.Deleted {
			matches = matches && (filter.New && file.Status == "new" || filter.Modified && file.Status == "modified" || filter.Deleted && file.Status == "deleted")
		}
		if matches {
			ids = append(ids, file.ID)
		}
	}
	if n.state.Workspace != nil {
		n.state.Workspace.VisibleFileIDs = ids
	}
	s.CommitView.CanCommit = n.status.Operation == "" && (count > 0 || s.CommitView.AllowEmpty || s.CommitView.Amending) && strings.TrimSpace(s.Message.Summary) != ""
	s.CommitView.CanAmend = s.HeadSHA != nil
	s.GenerationAvailable = n.state.CommitGeneration.Source == "custom" || s.Repository.Account != nil
}
func (n *NativeOwner) saveDraft(ctx context.Context) error {
	data, err := json.Marshal(n.state.Selected.Message)
	if err != nil {
		return err
	}
	return n.store.Set(ctx, "draft:"+n.current.Path, string(data))
}
func (n *NativeOwner) history(ctx context.Context, skip int) error {
	if n.status.Head == "" {
		n.state.Selected.History = []Commit{}
		return nil
	}
	ref := n.status.Head
	if compare := n.state.Workspace.Compare; compare.Ref != nil && compare.Mode != "history" {
		if compare.Mode == "ahead" {
			ref = *compare.Ref + ".." + n.status.Head
		} else {
			ref = n.status.Head + ".." + *compare.Ref
		}
	}
	commits, err := n.current.History(ctx, ref, skip, 100)
	if err != nil {
		return err
	}
	if skip == 0 {
		n.state.Selected.History = []Commit{}
	}
	for _, c := range commits {
		n.state.Selected.History = append(n.state.Selected.History, Commit{SHA: c.ID, ShortSHA: c.ID[:7], Summary: c.Subject, Body: c.Body, AuthorName: c.AuthorName, AuthorEmail: c.AuthorEmail, Date: c.Date, Tags: append([]string{}, n.tags[c.ID]...)})
	}
	return nil
}
func (n *NativeOwner) historyFiles(ctx context.Context) error {
	s := n.state.Selected
	s.HistoryFiles = []KernelFile{}
	s.HistoryFileID = nil
	s.Diff = nil
	if len(s.HistorySelection) == 0 {
		return nil
	}
	changes, err := n.current.Changes(ctx, s.HistorySelection[0])
	if err != nil {
		return err
	}
	for _, file := range changes {
		s.HistoryFiles = append(s.HistoryFiles, KernelFile{ID: file.Path, Path: file.Path, OldPath: textPointer(file.OldPath), Status: fileStatus(gitcli.File{Index: file.Status})})
	}
	return nil
}
func (n *NativeOwner) selectDiff(ctx context.Context, path string, history bool) error {
	n.state.Selected.Diff = nil
	n.state.Selected.DiffSelection = nil
	n.state.Workspace.TextDiff = nil
	commit := ""
	if history {
		if len(n.state.Selected.HistorySelection) == 0 {
			return errors.New("no selected commit")
		}
		commit = n.state.Selected.HistorySelection[0]
	}
	oldPath := ""
	files := n.state.Selected.Files
	if history {
		files = n.state.Selected.HistoryFiles
	}
	for _, file := range files {
		if file.Path == path && file.OldPath != nil {
			oldPath = *file.OldPath
			break
		}
	}
	text, err := n.current.DiffFile(ctx, path, oldPath, commit, 3, false)
	if err != nil {
		return err
	}
	kind := "text"
	if strings.Contains(text, "Binary files ") {
		kind = "binary"
	}
	if kind == "binary" || strings.HasSuffix(strings.ToLower(path), ".svg") {
		diff, err := n.imageDiff(ctx, path, commit)
		if err != nil {
			return err
		}
		if diff != nil {
			n.state.Selected.Diff = diff
			n.state.Selected.DiffSelection = nil
			return nil
		}
	}
	n.state.Selected.Diff = &Diff{Kind: kind, Text: &text}
	n.media.clear()
	n.state.Workspace.TextDiff = nil
	if !history && kind == "text" {
		n.diffSelection(path, text)
	} else {
		n.state.Selected.DiffSelection = nil
	}
	if kind == "text" {
		identity, _ := gitcli.PatchLines(text)
		n.state.Workspace.TextDiff = &KernelWorkspaceTextDiff{FileID: path, Identity: identity, Prepared: true, CanExpand: true, Selection: n.state.Selected.DiffSelection}
	}
	return nil
}
func (n *NativeOwner) close() error {
	n.cancel()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil
	}
	n.closed = true
	err := n.store.Close()
	close(n.done)
	return err
}
