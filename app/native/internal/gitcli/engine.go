// Package gitcli delegates repository semantics to the official Git executable.
// The engine owns bounded immutable-object caches shared by all repositories.
package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
