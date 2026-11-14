package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config contains resolved paths. UserData is supplied by MyGo PathUserData;
// resources and command-line overrides never redirect persistent user data.
type Config struct {
	Node     string
	Service  string
	UserData string
}

// ResolveConfig uses the shipped service directory unless explicitly overridden
// for development. An overridden service also owns its adjacent git directory.
func ResolveConfig(resources, userData, service, node string) (Config, error) {
	if service == "" {
		service = filepath.Join(resources, "service", "service.js")
	}
	if node == "" {
		name := "node"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		node = filepath.Join(resources, "service", name)
	}
	service, err := filepath.Abs(service)
	if err != nil {
		return Config{}, fmt.Errorf("service path: %w", err)
	}
	if userData == "" {
		return Config{}, errors.New("missing MyGo user data directory")
	}
	userData, err = filepath.Abs(userData)
	if err != nil {
		return Config{}, fmt.Errorf("user data path: %w", err)
	}
	if strings.ContainsAny(node, `/\\`) && !filepath.IsAbs(node) {
		node, err = filepath.Abs(node)
		if err != nil {
			return Config{}, fmt.Errorf("Node path: %w", err)
		}
	}
	return Config{Node: node, Service: service, UserData: userData}, nil
}

// ownedProcess is a started child together with its whole process tree.
// prioritize restores normal scheduling for that tree and reports how many
// processes it adjusted; it is a no-op where the platform has no such state.
type ownedProcess struct {
	wait       func() error
	prioritize func() int
	job        uintptr // Windows job handle, for inspection only
}

// RunCopilotRuntime forwards the SDK's pipes unchanged. The launcher owns a
// nested kill-on-close job, so SDK cancellation also ends runtime descendants.
func RunCopilotRuntime(executable string, arguments []string) error {
	cmd := exec.Command(executable, arguments...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	configureCopilotConsole(cmd)
	owned, err := startOwnedCommand(cmd)
	if err != nil {
		return err
	}
	return owned.wait()
}

// Service owns the child and its transport. Closing stdin first gives Node its
// normal EOF shutdown; a bounded fallback kills an unresponsive child and reaps
// it. The UI calls Close after its event loop, never from a click handler.
type Service struct {
	*Client
	cmd         *exec.Cmd
	stderr      *tailBuffer
	waitOnce    sync.Once
	waitDone    chan struct{}
	waitErr     error
	owned       ownedProcess
	close       sync.Once
	closeErr    error
	processName string
}

// Start launches Node directly with stdin/stdout pipes, without a shell or port.
func Start(config Config) (*Service, error) {
	stat, err := os.Stat(config.Service)
	if err != nil {
		return nil, fmt.Errorf("repository service %s: %w", config.Service, err)
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("repository service is not a file: %s", config.Service)
	}
	if err := os.MkdirAll(config.UserData, 0o755); err != nil {
		return nil, fmt.Errorf("repository user data: %w", err)
	}
	// Node 24 requires the feature flag as well as a file path to expose its
	// native localStorage to upstream modules; a file flag alone is not enough.
	cmd := exec.Command(config.Node, "--experimental-webstorage", "--localstorage-file="+filepath.Join(config.UserData, "localstorage.json"), config.Service)
	cmd.Dir = filepath.Dir(config.Service)
	cmd.Env = append(os.Environ(), "LOCAL_GIT_DIRECTORY="+filepath.Join(cmd.Dir, "git"), "GITOWL_USER_DATA="+config.UserData)
	return startProcess(cmd, "Node", nil, true)
}

// Both launch modes use the same owned pipes and EOF-before-Wait ordering.
// The native mode changes executable/bootstrap, not Git or account behavior.
func startProcess(cmd *exec.Cmd, name string, notify func(string, json.RawMessage) error, cancelRequests bool) (*Service, error) {
	hideConsole(cmd)
	s := &Service{cmd: cmd, stderr: &tailBuffer{}, waitDone: make(chan struct{}), processName: name}
	cmd.Stderr = s.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("repository stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("repository stdout: %w", err), stdin.Close())
	}
	owned, err := startOwnedCommand(cmd)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("start %s %s: %w", name, cmd.Path, err), stdin.Close(), stdout.Close())
	}
	s.owned = owned
	s.Client = newRPCClient(stdout, stdin, func() error {
		// Read stdout to EOF before Wait closes the pipe, preserving the last
		// complete response even when the child exits immediately afterward.
		s.wait()
		return s.exitError()
	}, notify, cancelRequests)
	go func() {
		<-s.Client.readDone
		s.wait()
	}()
	return s, nil
}

func (s *Service) wait() {
	s.waitOnce.Do(func() {
		s.waitErr = s.owned.wait()
		close(s.waitDone)
	})
}

func (s *Service) exitError() error {
	err := fmt.Errorf("%w: %s exited", ErrDisconnected, s.processName)
	if s.waitErr != nil {
		err = fmt.Errorf("%w: %s %v", ErrDisconnected, s.processName, s.waitErr)
	}
	if text := s.stderr.String(); text != "" {
		err = fmt.Errorf("%w: %s", err, text)
	}
	return err
}

// Close waits for Node to exit and reports cleanup failures. Expected shutdown
// by EOF (or the fallback kill) is not reported as an application failure.
func (s *Service) Close() error {
	s.close.Do(func() {
		s.closeErr = s.Client.Close()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-s.waitDone:
		case <-timer.C:
			if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				s.closeErr = errors.Join(s.closeErr, fmt.Errorf("stop Node: %w", err))
			}
			<-s.waitDone
		}
	})
	return s.closeErr
}

// tailBuffer drains all stderr while retaining only the last 32 KiB for a
// visible process error. Node diagnostics cannot corrupt the JSON stdout pipe.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 32*1024 {
		b.data = append([]byte(nil), b.data[len(b.data)-32*1024:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
