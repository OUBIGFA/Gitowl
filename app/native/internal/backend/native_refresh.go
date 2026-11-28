package backend

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
)

const nativeRefreshDelay = 200 * time.Millisecond
const nativeRefreshMaxDelay = 8 * time.Second

// The selected repository alone owns active recursive watchers. The toolkit's
// hints mode uses OS notifications without caching/stating every file in a tree.
// Git determines actual changes; repeated no-change events back off, and a
// periodic reconciliation covers lost notifications and unavailable watchers.
func (n *NativeOwner) watchRepository() {
	if n.watchCancel != nil {
		n.watchCancel()
	}
	ctx, cancel := context.WithCancel(n.ctx)
	n.watchCancel = cancel
	r := n.current
	notices := make(chan bool, 1)
	var metadataPending atomic.Bool
	roots := make(map[string]bool)
	for _, path := range []string{r.Path, r.GitDir, r.CommonDir} {
		if roots[path] {
			continue
		}
		roots[path] = true
		metadata := path != r.Path
		go func() {
			for ctx.Err() == nil {
				watch, err := mygo.WatchFiles(ctx, path, mygo.FileWatchOptions{Recursive: true, HintsOnly: true, Debounce: nativeRefreshDelay, MaxDelay: time.Second, Ignore: func(name string, _ bool) bool {
					first, _, _ := strings.Cut(filepath.ToSlash(name), "/")
					if !metadata {
						return first == ".git"
					}
					return first == "objects" || first == "logs" || first == "lfs"
				}})
				if err == nil {
					for {
						_, err = watch.Next(ctx)
						if err != nil {
							break
						}
						if metadata {
							metadataPending.Store(true)
						}
						select {
						case notices <- metadata:
						default:
						}
					}
					watch.Close()
				}
				if ctx.Err() != nil {
					return
				}
				// Watching failures stay visible; reconciliation still uses Git.
				n.mu.Lock()
				if n.current == r && !n.closed {
					n.reportWatchError(err)
				}
				n.mu.Unlock()
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		var timer *time.Timer
		var due <-chan time.Time
		metadata := false
		calibrate := false
		round := 0
		delay := nativeRefreshDelay
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-n.refreshWake:
				delay = nativeRefreshDelay
				metadata = true
				if due == nil {
					timer = time.NewTimer(delay)
					due = timer.C
				}
			case full := <-notices:
				metadata = metadata || full
				if due == nil {
					timer = time.NewTimer(delay)
					due = timer.C
				}
			case <-ticker.C:
				round++
				metadata = metadata || round%5 == 0
				calibrate = round%5 == 0
				if due == nil {
					timer = time.NewTimer(nativeRefreshDelay)
					due = timer.C
				}
			case <-due:
				due = nil
				if !n.mu.TryLock() {
					timer = time.NewTimer(nativeRefreshDelay)
					due = timer.C
					continue
				}
				if n.closed || n.current != r {
					n.mu.Unlock()
					return
				}
				metadata = metadata || metadataPending.Swap(false)
				if metadata && !calibrate {
					stamp, err := r.MetadataFingerprint()
					if err != nil {
						n.reportWatchError(err)
					} else if stamp == n.metadataStamp {
						metadata = false
					}
				}
				changed, err := n.refreshAndPublish(ctx, metadata)
				metadata = false
				calibrate = false
				if err != nil && !errors.Is(err, context.Canceled) {
					n.reportWatchError(err)
				}
				n.mu.Unlock()
				if changed {
					delay = nativeRefreshDelay
				} else {
					delay = min(nativeRefreshMaxDelay, max(nativeRefreshDelay, delay)*2)
				}
			}
		}
	}()
}

func (n *NativeOwner) reportWatchError(err error) {
	n.watchError = err
	if n.state.Workspace != nil && (n.state.Workspace.Banner == nil || n.state.Workspace.Banner.Type == "native-watch-error") {
		n.state.Workspace.Banner = &KernelWorkspaceBanner{ID: 2, Type: "native-watch-error", LabelKey: "native.refreshWarning", Actions: []string{"refresh"}, Dismissable: true}
		n.publish()
	}
}

func (n *NativeOwner) refreshAndPublish(ctx context.Context, full bool) (bool, error) {
	before := n.state.Revision
	changed, err := n.refresh(ctx, full)
	if err != nil {
		return false, err
	}
	// No presentation input changed, so avoid rebuilding every context menu and
	// comparing the full snapshot on idle reconciliation/watch notifications.
	if !changed && !full {
		return false, nil
	}
	n.publish()
	return n.state.Revision != before, nil
}
