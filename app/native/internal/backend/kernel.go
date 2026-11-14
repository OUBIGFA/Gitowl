package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// KernelRepositoryReference identifies an original Desktop repository/worktree.
// Worktrees can share an ID, so every command also carries its canonical path.
type KernelRepositoryReference struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

type KernelRepository struct {
	KernelRepositoryReference
	Name    string                 `json:"name"`
	Alias   *string                `json:"alias"`
	Missing bool                   `json:"missing"`
	Login   *string                `json:"login"`
	Account *KernelAccountIdentity `json:"account,omitempty"`
	Kind    string                 `json:"kind"`
	// Local is the original repository list's ahead/behind and change count.
	Local *KernelRepositoryLocal `json:"local,omitempty"`
}

// Ahead and Behind are nil without a tracking branch.
type KernelRepositoryLocal struct {
	Ahead        *int `json:"ahead"`
	Behind       *int `json:"behind"`
	ChangedFiles int  `json:"changedFiles"`
}

type KernelCloningRepository struct {
	Repository KernelRepository `json:"repository"`
	Progress   struct {
		Value       float64 `json:"value"`
		Description *string `json:"description"`
	} `json:"progress"`
}

type KernelFile struct {
	ID          string  `json:"id"`
	Path        string  `json:"path"`
	Status      string  `json:"status"`
	OldPath     *string `json:"oldPath"`
	Inclusion   string  `json:"inclusion"`
	IsSubmodule bool    `json:"isSubmodule"`
}

type KernelMessage struct {
	Summary            string  `json:"summary"`
	Description        *string `json:"description"`
	Timestamp          float64 `json:"timestamp"`
	GeneratedByCopilot bool    `json:"generatedByCopilot,omitempty"`
}

type KernelCommitContext struct {
	Summary                   string          `json:"summary"`
	Description               *string         `json:"description"`
	MessageGeneratedByCopilot bool            `json:"messageGeneratedByCopilot,omitempty"`
	Amend                     bool            `json:"amend,omitempty"`
	Trailers                  []KernelTrailer `json:"trailers,omitempty"`
}

type KernelTrailer struct {
	Token string `json:"token"`
	Value string `json:"value"`
}

// KernelSelected is a view projection, not a native repository/draft cache.
// These fields are replaced from each original AppStore snapshot.
type KernelSelected struct {
	Repository           KernelRepository     `json:"repository"`
	Section              string               `json:"section"`
	Branch               *string              `json:"branch"`
	Upstream             *string              `json:"upstream"`
	HeadSHA              *string              `json:"headSHA"`
	HasRemote            bool                 `json:"hasRemote"`
	ForcePushRecommended bool                 `json:"forcePushRecommended"`
	Ahead                int                  `json:"ahead"`
	Behind               int                  `json:"behind"`
	Files                []KernelFile         `json:"files"`
	SelectedFileIDs      []string             `json:"selectedFileIDs"`
	Message              KernelMessage        `json:"message"`
	CommitView           *KernelCommitView    `json:"commitView"`
	Diff                 *Diff                `json:"diff"`
	DiffSelection        *KernelDiffSelection `json:"diffSelection"`
	History              []Commit             `json:"history"`
	HistorySelection     []string             `json:"historySelection"`
	HistoryFiles         []KernelFile         `json:"historyFiles"`
	HistoryFileID        *string              `json:"historyFileID"`
	HistoryLinesAdded    int                  `json:"historyLinesAdded"`
	HistoryLinesDeleted  int                  `json:"historyLinesDeleted"`
	CommitInProgress     bool                 `json:"commitInProgress"`
	GenerationInProgress bool                 `json:"generationInProgress"`
	GenerationAvailable  bool                 `json:"generationAvailable"`
	RemoteInProgress     bool                 `json:"remoteInProgress"`
	ConflictKind         *string              `json:"conflictKind"`
}

type KernelAccount struct {
	Endpoint       string `json:"endpoint"`
	Login          string `json:"login"`
	Name           string `json:"name"`
	CopilotEnabled bool   `json:"copilotEnabled"`
}

// Popup carries only explicit presentation data. Original callbacks stay in
// the renderer, addressed by its opaque ID; credentials are never in snapshots.
type KernelPopup struct {
	ID   *int   `json:"id"`
	Type string `json:"type"`
}

// Only the original assignment identity crosses the presentation boundary.
// Tokens and account storage remain in the Desktop kernel.
type KernelAccountIdentity struct {
	Endpoint string `json:"endpoint"`
	Login    string `json:"login"`
}

type KernelErrorContent struct {
	Message           string   `json:"message"`
	Terminal          bool     `json:"terminal"`
	OversizedFiles    []string `json:"oversizedFiles"`
	RetryAfterMessage string   `json:"retryAfterMessage"`
	ActionText        string   `json:"actionText"`
	ActionURL         string   `json:"actionURL"`
}

// A whitelisted view of a supported original popup, never an arbitrary object
// or a callback/credential-bearing Account. Unknown types remain unsupported.
type KernelPopupPresentation struct {
	ID                            *int                     `json:"popupID"`
	Type                          string                   `json:"type"`
	Supported                     bool                     `json:"supported"`
	Host                          string                   `json:"host"`
	IP                            string                   `json:"ip"`
	KeyType                       string                   `json:"keyType"`
	Fingerprint                   string                   `json:"fingerprint"`
	KeyPath                       string                   `json:"keyPath"`
	Username                      *string                  `json:"username"`
	RemoteURL                     string                   `json:"remoteUrl"`
	HookName                      string                   `json:"hookName"`
	TerminalOutput                string                   `json:"terminalOutput"`
	Choices                       []string                 `json:"choices"`
	GenerationSource              string                   `json:"generationSource"`
	ShowCopilotInstructionsTip    bool                     `json:"showCopilotInstructionsTip"`
	RepositoryName                string                   `json:"repositoryName"`
	Endpoint                      string                   `json:"endpoint"`
	AssignedLogin                 *string                  `json:"assignedLogin"`
	SelectedAccount               *KernelAccountIdentity   `json:"selectedAccount,omitempty"`
	RequiresSignIn                bool                     `json:"requiresSignIn"`
	Accounts                      []KernelAccountIdentity  `json:"accounts"`
	UpstreamBranch                string                   `json:"upstreamBranch"`
	AskForConfirmationOnForcePush bool                     `json:"askForConfirmationOnForcePush"`
	Title                         string                   `json:"title"`
	Content                       KernelErrorContent       `json:"content"`
	Causes                        []KernelErrorContent     `json:"causes"`
	RetryCloneName                *string                  `json:"retryCloneName"`
	Layout                        string                   `json:"layout"`
	TitleKey                      string                   `json:"titleKey"`
	DescriptionKey                string                   `json:"descriptionKey"`
	Description                   string                   `json:"description"`
	Fields                        []KernelDialogField      `json:"fields"`
	Actions                       []KernelDialogAction     `json:"actions"`
	LayoutPages                   []KernelDialogLayoutPage `json:"layoutPages"`
	Error                         *string                  `json:"error"`
	Width                         float32                  `json:"width"`
	Height                        float32                  `json:"height"`
}

// Replies contain only explicit controls for the original popup type. Secrets
// are sent through the owned pipe once and are not part of any state snapshot.
type KernelPopupReply struct {
	PopupID                       int                    `json:"popupID"`
	Type                          string                 `json:"type"`
	Action                        string                 `json:"action"`
	AddHost                       *bool                  `json:"addHost,omitempty"`
	Passphrase                    *string                `json:"passphrase,omitempty"`
	StorePassphrase               *bool                  `json:"storePassphrase,omitempty"`
	Password                      *string                `json:"password,omitempty"`
	StorePassword                 *bool                  `json:"storePassword,omitempty"`
	Username                      *string                `json:"username,omitempty"`
	Result                        *string                `json:"result,omitempty"`
	ConfirmCommitMessageOverride  *bool                  `json:"confirmCommitMessageOverride,omitempty"`
	Account                       *KernelAccountIdentity `json:"account,omitempty"`
	AskForConfirmationOnForcePush *bool                  `json:"askForConfirmationOnForcePush,omitempty"`
	ContentIndex                  *int                   `json:"contentIndex,omitempty"`
	Values                        map[string]any         `json:"values,omitempty"`
}

type KernelAppearance struct {
	Theme   string `json:"theme"`
	TabSize int    `json:"tabSize"`
}

// Filtering and rank belong to the original AppStore/fuzzy-list projection.
type KernelRepositoryFilter struct {
	Text  string   `json:"text"`
	Paths []string `json:"paths"`
}

type KernelSnapshot struct {
	Protocol             int                       `json:"protocol"`
	Core                 string                    `json:"core"`
	PresentationComplete bool                      `json:"presentationComplete"`
	Revision             int64                     `json:"revision"`
	FocusCommitMessage   bool                      `json:"focusCommitMessage"`
	Appearance           *KernelAppearance         `json:"appearance"`
	ImageDiffType        int                       `json:"imageDiffType"`
	CommitGeneration     *KernelGenerationSettings `json:"commitGeneration"`
	Menu                 KernelMenuDTO             `json:"menu"`
	Workspace            *KernelWorkspace          `json:"workspace"`
	Repositories         []KernelRepository        `json:"repositories"`
	RepositoryMenus      []KernelRepositoryMenu    `json:"repositoryMenus"`
	RepositoryFilter     *KernelRepositoryFilter   `json:"repositoryFilter"`
	Accounts             []KernelAccount           `json:"accounts"`
	MissingRepository    *KernelMissingRepository  `json:"missingRepository"`
	CloningRepository    *KernelCloningRepository  `json:"cloningRepository"`
	Selected             *KernelSelected           `json:"selected"`
	Popups               []KernelPopup             `json:"popups"`
	CurrentPopup         *KernelPopupPresentation  `json:"currentPopup"`
	BannerType           *string                   `json:"bannerType"`
	FoldoutType          *int                      `json:"foldoutType"`
	Welcome              bool                      `json:"welcome"`
	Onboarding           *KernelOnboarding         `json:"onboarding"`
	SignIn               *KernelSignIn             `json:"signIn"`
	ErrorCount           int                       `json:"errorCount"`
}

type KernelResult struct {
	MediaChunk        *KernelMediaChunk           `json:"mediaChunk,omitempty"`
	Snapshot          KernelSnapshot              `json:"snapshot"`
	Committed         *bool                       `json:"committed,omitempty"`
	Assisted          *KernelAssistedReceipt      `json:"assisted,omitempty"`
	AddedRepositories []KernelRepositoryReference `json:"addedRepositories,omitempty"`
	HistorySummaries  []KernelHistorySummary      `json:"historySummaries,omitempty"`
}

type KernelHistorySummary struct {
	SHA          string `json:"sha"`
	FileCount    int    `json:"fileCount"`
	LinesAdded   int    `json:"linesAdded"`
	LinesDeleted int    `json:"linesDeleted"`
}

type KernelWindowState struct {
	Visible   bool    `json:"visible"`
	Focused   bool    `json:"focused"`
	Minimized bool    `json:"minimized"`
	State     string  `json:"state"`
	Zoom      float64 `json:"zoom"`
}

type KernelFailure struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

// KernelEvent is emitted in wire order. A handler only schedules UI-thread
// work and must not synchronously call the client from its reader goroutine.
type KernelEvent struct {
	Dialog             *KernelFileDialog
	External           *KernelExternalRequest
	Snapshot           *KernelSnapshot
	WindowAction       string
	PresentationAction string
	Zoom               *float64
	Failure            *KernelFailure
}

type KernelConfig struct {
	Electron        string
	Entry           string
	UserData        string
	CopilotLauncher string // Set by the visible host, never an ambient override.
}

func ResolveKernelConfig(resources, userData, entry, electron string) (KernelConfig, error) {
	if entry == "" {
		entry = filepath.Join(resources, "desktop-kernel", "app", "main.js")
	}
	if electron == "" {
		name := "electron"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		electron = filepath.Join(resources, "desktop-kernel", name)
	}
	if userData == "" {
		return KernelConfig{}, errors.New("missing MyGo user data directory")
	}
	var err error
	entry, err = filepath.Abs(entry)
	if err != nil {
		return KernelConfig{}, fmt.Errorf("kernel entry: %w", err)
	}
	userData, err = filepath.Abs(userData)
	if err != nil {
		return KernelConfig{}, fmt.Errorf("kernel user data: %w", err)
	}
	if strings.ContainsAny(electron, `/\`) && !filepath.IsAbs(electron) {
		electron, err = filepath.Abs(electron)
		if err != nil {
			return KernelConfig{}, fmt.Errorf("Electron path: %w", err)
		}
	}
	return KernelConfig{Electron: electron, Entry: entry, UserData: userData}, nil
}

func kernelEnvironment(config KernelConfig) []string {
	var env []string
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "ELECTRON_RUN_AS_NODE", "ELECTRON_NO_ATTACH_CONSOLE", "NODE_OPTIONS", "LOCAL_GIT_DIRECTORY", "GIT_EXEC_PATH", "GITOWL_USER_DATA", "GITOWL_NATIVE_CORE", "GITOWL_CREDENTIAL_NAMESPACE", "GITOWL_COPILOT_LAUNCHER":
			continue
		}
		env = append(env, item)
	}
	// Electron's Windows bootstrap must not replace the owned pipes with a
	// console. Its EOF-only process.stdin is separately handled by native-mode.
	if config.CopilotLauncher != "" {
		env = append(env, "GITOWL_COPILOT_LAUNCHER="+config.CopilotLauncher)
	}
	return append(env, "GITOWL_USER_DATA="+config.UserData, "ELECTRON_NO_ATTACH_CONSOLE=1")
}

// Kernel owns no Git/credential/business state. It connects the MyGo presenter
// to the original Electron main and non-mounted AppStore/Dispatcher renderer.
type Kernel struct {
	client    *Client
	service   *Service
	onEvent   func(KernelEvent)
	mu        sync.Mutex
	closing   bool
	closeErr  error
	closeDone chan struct{}
}

// NewKernel admits a caller-owned pipe (also used by transport contract tests).
func NewKernel(reader io.ReadCloser, writer io.WriteCloser, onEvent func(KernelEvent)) *Kernel {
	k := &Kernel{onEvent: onEvent, closeDone: make(chan struct{})}
	k.client = newRPCClient(reader, writer, nil, k.receiveEvent, false)
	return k
}

// StartKernel launches Electron as Electron, never as a reduced Node service.
func StartKernel(config KernelConfig, onEvent func(KernelEvent)) (*Kernel, error) {
	stat, err := os.Stat(config.Entry)
	if err != nil {
		return nil, fmt.Errorf("Desktop kernel entry %s: %w", config.Entry, err)
	}
	if !stat.Mode().IsRegular() {
		return nil, errors.New("Desktop kernel entry is not a file")
	}
	if !filepath.IsAbs(config.UserData) {
		return nil, errors.New("Desktop kernel user data must be an absolute MyGo path")
	}
	if err := os.MkdirAll(config.UserData, 0o755); err != nil {
		return nil, fmt.Errorf("Desktop kernel user data: %w", err)
	}
	application := config.Entry
	// Electron's default-app loader reads package identity only when launched
	// with the application directory. A direct main.js override remains a dev
	// entry, but a prepared native runtime must use its adjacent package.json.
	if data, readErr := os.ReadFile(filepath.Join(filepath.Dir(config.Entry), "package.json")); readErr == nil {
		var pkg struct {
			Main string `json:"main"`
		}
		if json.Unmarshal(data, &pkg) == nil && pkg.Main != "" && filepath.Clean(filepath.Join(filepath.Dir(config.Entry), pkg.Main)) == filepath.Clean(config.Entry) {
			application = filepath.Dir(config.Entry)
		}
	}
	cmd := exec.Command(config.Electron, application, "--gitowl-native-core")
	cmd.Dir = filepath.Dir(config.Entry)
	cmd.Env = kernelEnvironment(config)
	k := &Kernel{onEvent: onEvent, closeDone: make(chan struct{})}
	s, err := startProcess(cmd, "Electron", k.receiveEvent, false)
	if err != nil {
		return nil, err
	}
	k.service, k.client = s, s.Client
	return k, nil
}

func (k *Kernel) Done() <-chan struct{} { return k.client.Done() }
func (k *Kernel) Err() error            { return k.client.Err() }
func (k *Kernel) InFlight() int         { return k.client.InFlight() }
func (k *Kernel) CriticalInFlight() int { return k.client.CriticalInFlight() }

func validateKernelSnapshot(snapshot KernelSnapshot) error {
	if snapshot.Protocol != 1 || snapshot.Core != "desktop/desktop" || snapshot.Revision <= 0 || snapshot.Repositories == nil || snapshot.Accounts == nil || snapshot.Popups == nil {
		return &ProtocolError{"expected an original Desktop kernel presentation snapshot"}
	}
	for _, repository := range snapshot.Repositories {
		if repository.Path == "" || repository.Name == "" || (repository.Kind != "repository" && repository.Kind != "cloning") {
			return &ProtocolError{"invalid original Desktop repository view"}
		}
	}
	if clone := snapshot.CloningRepository; clone != nil {
		if snapshot.Selected != nil || snapshot.MissingRepository != nil || clone.Repository.Kind != "cloning" || clone.Repository.ID <= 0 || clone.Repository.Path == "" || clone.Repository.Name == "" || math.IsNaN(clone.Progress.Value) || math.IsInf(clone.Progress.Value, 0) || clone.Progress.Value < 0 || clone.Progress.Value > 1 {
			return &ProtocolError{"invalid original Desktop clone view"}
		}
	}
	if selected := snapshot.Selected; selected != nil {
		if selected.Repository.Path == "" || selected.Files == nil || selected.History == nil || (selected.Section != "changes" && selected.Section != "history") {
			return &ProtocolError{"invalid original Desktop selection view"}
		}
		for _, file := range selected.Files {
			if file.ID == "" || file.Path == "" || (file.Inclusion != "all" && file.Inclusion != "none" && file.Inclusion != "partial") {
				return &ProtocolError{"invalid original Desktop file view"}
			}
		}
	}
	return nil
}

func (k *Kernel) receiveEvent(method string, params json.RawMessage) error {
	var event KernelEvent
	switch method {
	case "external.open":
		var request KernelExternalRequest
		if err := json.Unmarshal(params, &request); err != nil || request.ID <= 0 || request.URL == "" || strings.ContainsRune(request.URL, '\x00') {
			return &ProtocolError{"invalid native external URL request"}
		}
		event.External = &request
	case "dialog.request":
		var dialog KernelFileDialog
		if err := json.Unmarshal(params, &dialog); err != nil || dialog.ID <= 0 || (dialog.Kind != "open" && dialog.Kind != "save") {
			return &ProtocolError{"invalid native file dialog"}
		}
		event.Dialog = &dialog
	case "kernel.state":
		var snapshot KernelSnapshot
		if err := json.Unmarshal(params, &snapshot); err != nil {
			return &ProtocolError{"invalid kernel.state"}
		}
		if err := validateKernelSnapshot(snapshot); err != nil {
			return err
		}
		event.Snapshot = &snapshot
	case "kernel.presentation":
		var value struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(params, &value); err != nil {
			return &ProtocolError{"invalid kernel.presentation"}
		}
		switch value.Action {
		case "focus-history", "focus-changes", "select-all", "find-text", "increase-active-resizable-width", "decrease-active-resizable-width":
		default:
			return &ProtocolError{"unknown native presentation action"}
		}
		event.PresentationAction = value.Action
	case "window.action":
		var value struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(params, &value); err != nil {
			return &ProtocolError{"invalid window.action"}
		}
		switch value.Action {
		case "show", "focus", "restore", "minimize", "maximize", "unmaximize", "close", "select-all", "attention":
		default:
			return &ProtocolError{"unknown native window action"}
		}
		event.WindowAction = value.Action
	case "window.zoom":
		var value struct {
			Factor *float64 `json:"factor"`
		}
		if err := json.Unmarshal(params, &value); err != nil || value.Factor == nil || *value.Factor < 0.25 || *value.Factor > 5 {
			return &ProtocolError{"invalid window.zoom"}
		}
		event.Zoom = value.Factor
	case "kernel.failure":
		var failure KernelFailure
		if err := json.Unmarshal(params, &failure); err != nil || failure.Message == "" {
			return &ProtocolError{"invalid kernel.failure"}
		}
		event.Failure = &failure
	default:
		return &ProtocolError{"unknown Desktop kernel notification"}
	}
	if k.onEvent != nil {
		k.onEvent(event)
	}
	return nil
}

func (k *Kernel) command(ctx context.Context, command any) (KernelResult, error) {
	return k.commandKind(ctx, command, false)
}

func (k *Kernel) commandKind(ctx context.Context, command any, media bool) (KernelResult, error) {
	var out KernelResult
	if err := k.client.call(ctx, "kernel.command", command, &out, false, media); err != nil {
		return out, err
	}
	if err := validateKernelSnapshot(out.Snapshot); err != nil {
		k.client.fail(err)
		return out, err
	}
	return out, nil
}

// ChangeImageDiffType forwards the original global preference, not a native copy.
func (k *Kernel) ChangeImageDiffType(ctx context.Context, mode int) (KernelResult, error) {
	return k.command(ctx, struct {
		Name          string `json:"name"`
		ImageDiffType int    `json:"imageDiffType"`
	}{"imageDiff.type", mode})
}

// AcknowledgeCommitMessageFocus preserves the original one-shot focus flag.
func (k *Kernel) AcknowledgeCommitMessageFocus(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"presentation.commit-message-focused"})
}

func (k *Kernel) State(ctx context.Context) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
	}{"state"})
}

func (k *Kernel) AddRepositories(ctx context.Context, paths []string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name  string   `json:"name"`
		Paths []string `json:"paths"`
	}{"repositories.add", paths})
}

func (k *Kernel) repositoryCommand(ctx context.Context, name string, repository KernelRepositoryReference) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
	}{name, repository})
}

func (k *Kernel) SelectRepository(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.select", repository)
}

func (k *Kernel) FilterRepositories(ctx context.Context, text string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}{"repositories.filter", text})
}
func (k *Kernel) RefreshRepository(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.refresh", repository)
}
func (k *Kernel) Fetch(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.fetch", repository)
}
func (k *Kernel) Pull(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.pull", repository)
}
func (k *Kernel) Push(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.push", repository)
}
func (k *Kernel) CancelGeneration(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "repository.generate.cancel", repository)
}
func (k *Kernel) LoadHistory(ctx context.Context, repository KernelRepositoryReference) (KernelResult, error) {
	return k.repositoryCommand(ctx, "history.loadNext", repository)
}

func (k *Kernel) ChangeSection(ctx context.Context, repository KernelRepositoryReference, section string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Section    string                    `json:"section"`
	}{"repository.section", repository, section})
}

func (k *Kernel) SelectFiles(ctx context.Context, repository KernelRepositoryReference, ids []string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		FileIDs    []string                  `json:"fileIDs"`
	}{"repository.files.select", repository, ids})
}

func (k *Kernel) IncludeFiles(ctx context.Context, repository KernelRepositoryReference, ids []string, included bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		FileIDs    []string                  `json:"fileIDs"`
		Included   bool                      `json:"included"`
	}{"repository.files.include", repository, ids, included})
}

func (k *Kernel) IncludeAll(ctx context.Context, repository KernelRepositoryReference, included bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Included   bool                      `json:"included"`
	}{"repository.files.includeAll", repository, included})
}

func (k *Kernel) SetMessage(ctx context.Context, repository KernelRepositoryReference, message KernelMessage) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Message    KernelMessage             `json:"message"`
	}{"repository.message", repository, message})
}

func (k *Kernel) CommitIncludedChanges(ctx context.Context, repository KernelRepositoryReference, commit KernelCommitContext) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		Context    KernelCommitContext       `json:"context"`
	}{"repository.commit", repository, commit})
}

func (k *Kernel) GenerateMessage(ctx context.Context, repository KernelRepositoryReference, ids []string, override bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		FileIDs    []string                  `json:"fileIDs"`
		Override   bool                      `json:"overrideExistingMessage"`
	}{"repository.generate", repository, ids, override})
}

func (k *Kernel) SelectHistory(ctx context.Context, repository KernelRepositoryReference, shas []string, contiguous bool) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		SHAs       []string                  `json:"shas"`
		Contiguous bool                      `json:"contiguous"`
	}{"history.select", repository, shas, contiguous})
}

func (k *Kernel) HistorySummaries(ctx context.Context, repository KernelRepositoryReference, shas []string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		SHAs       []string                  `json:"shas"`
	}{"history.summaries", repository, shas})
}

func (k *Kernel) SelectHistoryFile(ctx context.Context, repository KernelRepositoryReference, id string) (KernelResult, error) {
	return k.command(ctx, struct {
		Name       string                    `json:"name"`
		Repository KernelRepositoryReference `json:"repository"`
		FileID     string                    `json:"fileID"`
	}{"history.file.select", repository, id})
}

func (k *Kernel) ReplyPopup(ctx context.Context, reply KernelPopupReply) (KernelResult, error) {
	return k.command(ctx, struct {
		Name  string           `json:"name"`
		Reply KernelPopupReply `json:"reply"`
	}{"popup.reply", reply})
}

func (k *Kernel) ClosePopup(ctx context.Context, id int) (KernelResult, error) {
	return k.command(ctx, struct {
		Name    string `json:"name"`
		PopupID int    `json:"popupID"`
	}{"popup.close", id})
}

func (k *Kernel) SignIn(ctx context.Context, endpoint *string) (KernelResult, error) {
	if endpoint == nil {
		return k.command(ctx, struct {
			Name string `json:"name"`
		}{"signIn.dotcom"})
	}
	return k.command(ctx, struct {
		Name     string `json:"name"`
		Endpoint string `json:"endpoint"`
	}{"signIn.enterprise", *endpoint})
}

func (k *Kernel) WindowState(ctx context.Context, state KernelWindowState) error {
	var out struct {
		Updated bool `json:"updated"`
	}
	if err := k.client.Call(ctx, "kernel.windowState", state, &out); err != nil {
		return err
	}
	if !out.Updated {
		return k.client.invalid("kernel.windowState", "window state was not acknowledged")
	}
	return nil
}

func (k *Kernel) prioritizeTree() {
	if k.service != nil && k.service.owned.prioritize != nil {
		k.service.owned.prioritize()
	}
}

// Close timeouts are variables so tests can shorten them.
var (
	closeSettleTimeout = 60 * time.Second
	closeExitTimeout   = 30 * time.Second
)

// Close requires settled operations, atomically stops further admissions, and
// requests normal original-kernel shutdown. Unlike the research Service.Close,
// it never kills a process with an unresolved write or pretends one was canceled.
func (k *Kernel) Close() error {
	// The UI defers quitting until admitted work settles, so this normally
	// finds nothing in flight. Other exits (session end, a late canceled
	// reply) still wait for real replies off the UI thread instead of refusing
	// shutdown or abandoning a Git write; only then is admission stopped.
	deadline := time.Now().Add(closeSettleTimeout)
	for k.client.InFlight() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	k.mu.Lock()
	if k.closing {
		done := k.closeDone
		k.mu.Unlock()
		<-done
		k.mu.Lock()
		err := k.closeErr
		k.mu.Unlock()
		return err
	}
	if err := k.client.beginShutdown(); err != nil {
		k.mu.Unlock()
		return err
	}
	k.closing = true
	k.mu.Unlock()
	// A loaded kernel may flush stores and stop many repository watchers
	// before exiting; that is slower than one RPC round trip.
	ctx, cancel := context.WithTimeout(context.Background(), closeExitTimeout)
	defer cancel()
	var out struct {
		Quitting bool `json:"quitting"`
	}
	// Quitting is user-blocking work: whatever priority Chromium or Windows
	// chose for the hidden kernel tree, restore normal scheduling while it
	// flushes, so a saturated set of efficient cores cannot starve the exit.
	k.prioritizeTree()
	err := k.client.call(ctx, "kernel.shutdown", nil, &out, true, false)
	if err == nil && !out.Quitting {
		err = k.client.invalid("kernel.shutdown", "shutdown was not acknowledged")
	}
	if err == nil {
		// Acknowledgment stops new commands, but a final renderer state may
		// still be on stdout. Close stdin alone and drain that state through EOF
		// before releasing the reader; closing both pipes here races that write.
		err = k.client.writer.Close()
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
			err = nil
		}
		if err == nil {
			done := k.client.readDone
			if k.service != nil {
				done = k.service.waitDone
			}
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
		wait:
			for {
				select {
				case <-done:
					if k.service != nil && k.service.waitErr != nil {
						err = errors.Join(err, k.service.exitError())
					}
					if terminal := k.client.Err(); terminal != nil && !errors.Is(terminal, ErrDisconnected) {
						err = errors.Join(err, terminal)
					}
					break wait
				case <-ticker.C:
					// Chromium starts and reprioritizes children while quitting.
					k.prioritizeTree()
				case <-ctx.Done():
					err = errors.Join(err, errors.New("Desktop kernel did not finish its acknowledged shutdown in time; its settled process tree ends with Gitowl"))
					break wait
				}
			}
		}
	}
	k.mu.Lock()
	k.closeErr = err
	close(k.closeDone)
	k.mu.Unlock()
	return err
}
