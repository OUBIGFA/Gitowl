package backend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

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
	refreshPending      bool
	refreshWake         chan struct{}
	windowFocused       atomic.Bool
	starting            bool
	historyBase         string
	historyTip          string
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

func (n *NativeOwner) close() error {
	n.cancel()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil
	}
	n.closed = true
	err := n.store.Close()
	n.media.clear()
	n.httpClient.CloseIdleConnections()
	close(n.done)
	return err
}
