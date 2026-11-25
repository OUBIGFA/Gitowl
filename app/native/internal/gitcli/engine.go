// Package gitcli delegates repository semantics to the official Git executable.
// The engine owns bounded immutable-object caches shared by all repositories.
package gitcli

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultCacheBytes int64 = 32 << 20
const maxCachedObject = 8 << 20

var ErrOutputLimit = errors.New("Git output exceeds the native preview limit")
var ErrObjectMissing = errors.New("Git object does not exist")

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	remaining := b.limit - b.Len()
	if len(data) > remaining {
		if remaining > 0 {
			_, _ = b.Buffer.Write(data[:remaining])
		}
		return max(0, remaining), ErrOutputLimit
	}
	return b.Buffer.Write(data)
}

type cacheEntry struct {
	key  string
	data []byte
	cost int64
}
type cache struct {
	mu          sync.Mutex
	limit, used int64
	items       map[string]*list.Element
	lru         *list.List
}

func newCache(limit int64) *cache {
	return &cache{limit: limit, items: make(map[string]*list.Element), lru: list.New()}
}
func (c *cache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[key]
	if e == nil {
		return nil, false
	}
	c.lru.MoveToFront(e)
	return bytes.Clone(e.Value.(cacheEntry).data), true
}
func (c *cache) put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Charge keys and a conservative per-entry overhead as well as payloads.
	cost := int64(len(key) + len(data) + 128)
	if cost > c.limit || len(data) > maxCachedObject {
		return
	}
	if old := c.items[key]; old != nil {
		c.used -= old.Value.(cacheEntry).cost
		c.lru.Remove(old)
		delete(c.items, key)
	}
	for c.used+cost > c.limit {
		old := c.lru.Back()
		entry := old.Value.(cacheEntry)
		c.used -= entry.cost
		delete(c.items, entry.key)
		c.lru.Remove(old)
	}
	c.items[key] = c.lru.PushFront(cacheEntry{key, bytes.Clone(data), cost})
	c.used += cost
}

type Stats struct {
	Commands     int64
	CacheHits    int64
	CacheBytes   int64
	CacheEntries int
}
type Engine struct {
	git            string
	cache          *cache
	commands, hits atomic.Int64
	mu             sync.Mutex
	repositories   map[string]*Repository
	loads          map[string]*objectLoad
}
type objectLoad struct {
	done chan struct{}
	data []byte
	err  error
}

func New(git string, cacheBytes int64) *Engine {
	return &Engine{git: git, cache: newCache(max(0, cacheBytes)), repositories: make(map[string]*Repository), loads: make(map[string]*objectLoad)}
}

func (e *Engine) Global(ctx context.Context, args ...string) ([]byte, error) {
	return e.run(ctx, "", nil, nil, args...)
}
func (e *Engine) GlobalWithEnvironment(ctx context.Context, env []string, args ...string) ([]byte, error) {
	return e.run(ctx, "", nil, env, args...)
}
func (r *Repository) RunWithEnvironment(ctx context.Context, env []string, args ...string) ([]byte, error) {
	return r.engine.run(ctx, r.Path, nil, env, args...)
}
func (e *Engine) Init(ctx context.Context, path string) ([]byte, error) {
	return e.run(ctx, path, nil, nil, "init", "-b", "main")
}
func (e *Engine) Clone(ctx context.Context, remote, path string) ([]byte, error) {
	return e.run(ctx, "", nil, nil, "clone", "--", remote, path)
}

func (e *Engine) load(ctx context.Context, key string, read func() ([]byte, error)) ([]byte, error) {
	for {
		if data, ok := e.cache.get(key); ok {
			e.hits.Add(1)
			return data, nil
		}
		e.mu.Lock()
		// A load may finish between the first cache check and acquiring this
		// lock. Recheck before registering another process for the same object.
		if data, ok := e.cache.get(key); ok {
			e.mu.Unlock()
			e.hits.Add(1)
			return data, nil
		}
		if flight := e.loads[key]; flight != nil {
			e.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-flight.done:
			}
			if errors.Is(flight.err, context.Canceled) && ctx.Err() == nil {
				continue
			}
			return bytes.Clone(flight.data), flight.err
		}
		flight := &objectLoad{done: make(chan struct{})}
		e.loads[key] = flight
		e.mu.Unlock()
		flight.data, flight.err = read()
		if flight.err == nil {
			e.cache.put(key, flight.data)
		}
		e.mu.Lock()
		delete(e.loads, key)
		close(flight.done)
		e.mu.Unlock()
		return bytes.Clone(flight.data), flight.err
	}
}
func (e *Engine) Stats() Stats {
	e.cache.mu.Lock()
	defer e.cache.mu.Unlock()
	return Stats{e.commands.Load(), e.hits.Load(), e.cache.used, len(e.cache.items)}
}

// CommandError preserves Git's exit status and diagnostic output. Callers must
// distinguish a rejected operation from cancellation or a failed process start.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), strings.TrimSpace(e.Stderr))
}
func (e *CommandError) Unwrap() error { return e.Err }

type Repository struct {
	Path, GitDir, CommonDir string
	engine                  *Engine
	mutation                sync.Mutex
	Environment             []string
}

func (e *Engine) run(ctx context.Context, path string, input io.Reader, env []string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.commands.Add(1)
	cmd := exec.CommandContext(ctx, e.git, append([]string{"--literal-pathspecs"}, args...)...)
	cmd.Dir = path
	cmd.Stdin = input
	cmd.WaitDelay = 2 * time.Second
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_EXEC_PATH", "GIT_COMMON_DIR", "GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_GRAFT_FILE", "GIT_REPLACE_REF_BASE", "GIT_IMPLICIT_WORK_TREE":
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "TERM=dumb", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0", "GIT_MERGE_AUTOEDIT=no")
	// The published Git distribution owns its helper/layout conventions. Add
	// its command and SSH directories without changing the user's Git config.
	gitRoot := filepath.Dir(filepath.Dir(e.git))
	cmd.Env = append(cmd.Env, "PATH="+filepath.Join(gitRoot, "cmd")+string(os.PathListSeparator)+filepath.Join(gitRoot, "usr", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	hideWindow(cmd)
	out := boundedOutput{limit: 64 << 20}
	errout := boundedOutput{limit: 256 << 10}
	cmd.Stdout = &out
	cmd.Stderr = &errout
	if err := cmd.Run(); err != nil {
		if errors.Is(err, ErrOutputLimit) {
			return nil, ErrOutputLimit
		}
		if ctx.Err() != nil {
			return out.Bytes(), ctx.Err()
		}
		code := -1
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			code = exited.ExitCode()
		}
		return out.Bytes(), &CommandError{append([]string{}, args...), code, errout.String(), err}
	}
	return out.Bytes(), nil
}
func (r *Repository) Run(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	return r.engine.run(ctx, r.Path, input, r.Environment, args...)
}

func (e *Engine) Open(ctx context.Context, path string) (*Repository, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	physical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	data, err := e.run(ctx, physical, nil, nil, "rev-parse", "--path-format=absolute", "--show-toplevel", "--absolute-git-dir", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		return nil, errors.New("Git did not identify a working repository")
	}
	for i := range lines {
		lines[i] = filepath.Clean(strings.TrimSuffix(lines[i], "\r"))
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if existing := e.repositories[lines[0]]; existing != nil && existing.GitDir == lines[1] && existing.CommonDir == lines[2] {
		return existing, nil
	}
	r := &Repository{Path: lines[0], GitDir: lines[1], CommonDir: lines[2], engine: e}
	e.repositories[r.Path] = r
	return r, nil
}

func objectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Blob resolves mutable references every time and caches bytes only under the
// actual immutable object ID. Full commit:path resolution is itself immutable.
// Worktrees share the common Git identity; independent repositories remain
// isolated so a cached object cannot manufacture access to a missing object.
func (r *Repository) Blob(ctx context.Context, revision, path string, limit int) ([]byte, error) {
	if strings.ContainsRune(revision+path, 0) {
		return nil, errors.New("invalid Git object path")
	}
	key := "resolve\x00" + r.CommonDir + "\x00" + revision + ":" + path
	read := func() ([]byte, error) {
		// Read size before allocating content. NUL framing preserves filenames
		// containing newlines, and 'missing' is distinct from process failure.
		data, err := r.Run(ctx, strings.NewReader(revision+":"+path+"\x00"), "cat-file", "--batch-check", "-Z")
		if err == nil && strings.HasSuffix(string(data), " missing\x00") {
			return nil, ErrObjectMissing
		}
		return data, err
	}
	var metadata []byte
	var err error
	if objectID(revision) {
		metadata, err = r.engine.load(ctx, key, read)
	} else {
		metadata, err = read()
	}
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(string(metadata), " missing\x00") {
		return nil, ErrObjectMissing
	}
	fields := strings.Fields(strings.TrimSuffix(string(metadata), "\x00"))
	if len(fields) != 3 || !objectID(fields[0]) || fields[1] != "blob" {
		return nil, errors.New("Git returned invalid blob metadata")
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, errors.New("Git returned an invalid object size")
	}
	if size > 64<<20 || limit > 0 && size > int64(limit) {
		return nil, ErrOutputLimit
	}
	oid := fields[0]
	objectKey := "blob\x00" + r.CommonDir + "\x00" + oid
	if data, ok := r.engine.cache.get(objectKey); ok {
		r.engine.hits.Add(1)
		return data, nil
	}
	data, err := r.engine.load(ctx, objectKey, func() ([]byte, error) { return r.Run(ctx, nil, "cat-file", "blob", oid) })
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, errors.New("Git returned an incomplete blob")
	}
	return data, nil
}
