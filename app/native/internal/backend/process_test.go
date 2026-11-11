package backend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These helpers exercise an actual Node child. Pipe-only tests remain runnable
// without Node; the integrated service and real Git are verified by the parent.
func nodeService(t *testing.T, script string) *Service {
	t.Helper()
	node := os.Getenv("GITOWL_TEST_NODE")
	if node == "" {
		name := "node"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		node, _ = filepath.Abs(filepath.Join("..", "..", "..", "..", "output", "native-runtime", "service", name))
		if _, err := os.Stat(node); err != nil {
			node, _ = exec.LookPath("node")
		}
	}
	version, err := exec.Command(node, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "v24.22.1" {
		t.Skip("process tests require pinned Node 24.22.1; set GITOWL_TEST_NODE (pipe-only tests do not need it)")
	}
	root := t.TempDir()
	service := filepath.Join(root, "服务 with spaces", "service.js")
	if err := os.MkdirAll(filepath.Dir(service), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(service, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	config, err := ResolveConfig(root, filepath.Join(root, "用户 data"), service, node)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestResourceDefaults(t *testing.T) {
	root := t.TempDir()
	config, err := ResolveConfig(root, filepath.Join(root, "data"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	node := "node"
	if runtime.GOOS == "windows" {
		node += ".exe"
	}
	if config.Service != filepath.Join(root, "service", "service.js") || config.Node != filepath.Join(root, "service", node) {
		t.Fatalf("resource defaults: %+v", config)
	}
	if _, err := Start(config); err == nil {
		t.Fatal("missing service looked successful")
	}
}

func TestNodeArgumentsAndEOFShutdown(t *testing.T) {
	t.Setenv("LOCAL_GIT_DIRECTORY", "must be overridden")
	s := nodeService(t, `
const rl = require('node:readline').createInterface({input: process.stdin});
rl.on('line', line => {
  const request = JSON.parse(line);
  process.stdout.write(JSON.stringify({jsonrpc:'2.0', id:request.id, result:{
    git:process.env.LOCAL_GIT_DIRECTORY,
    storage:process.execArgv.find(arg => arg.startsWith('--localstorage-file=')),
    webstorage:process.execArgv.includes('--experimental-webstorage'),
    storageAvailable:typeof globalThis.localStorage === 'object'
  }}) + '\n');
});
rl.on('close', () => process.exit(0));
`)
	var out struct {
		Git, Storage                 string
		Webstorage, StorageAvailable bool
	}
	if err := s.Call(context.Background(), "probe", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(s.cmd.Args) != 4 || s.cmd.Args[1] != "--experimental-webstorage" || out.Git != filepath.Join(s.cmd.Dir, "git") || out.Storage != s.cmd.Args[2] || !strings.Contains(out.Storage, "localstorage.json") || !out.Webstorage || !out.StorageAvailable {
		t.Fatalf("child configuration: %+v, args %q", out, s.cmd.Args)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.cmd.ProcessState == nil || !s.cmd.ProcessState.Success() {
		t.Fatalf("child did not shut down cleanly: %v", s.cmd.ProcessState)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestNodeExitReleasesPendingWithDiagnostics(t *testing.T) {
	s := nodeService(t, `
require('node:readline').createInterface({input:process.stdin}).on('line', () => {
  process.stderr.write('service failed intentionally\n');
  process.exit(7);
});
`)
	call := callString(s.Client, context.Background(), "pending")
	err := receive(t, call).err
	if !errors.Is(err, ErrDisconnected) || !strings.Contains(err.Error(), "exit status 7") || !strings.Contains(err.Error(), "service failed intentionally") {
		t.Fatalf("exit diagnostics missing: %v", err)
	}
	receive(t, s.Done())
}

func TestNodeFinalResponseIsNotLostToExit(t *testing.T) {
	s := nodeService(t, `
require('node:readline').createInterface({input:process.stdin}).on('line', line => {
  const request = JSON.parse(line);
  process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:request.id,result:'last response'})+'\n', () => process.exit(0));
});
`)
	if got := receive(t, callString(s.Client, context.Background(), "final")); got.err != nil || got.value != "last response" {
		t.Fatalf("last response: %+v", got)
	}
	receive(t, s.Done())
	if !errors.Is(s.Err(), ErrDisconnected) {
		t.Fatalf("exit was hidden: %v", s.Err())
	}
}

func TestProtocolErrorStopsNode(t *testing.T) {
	s := nodeService(t, `
const rl = require('node:readline').createInterface({input:process.stdin});
rl.on('line', () => process.stdout.write('invalid protocol\n'));
rl.on('close', () => process.exit(0));
`)
	var protocol *ProtocolError
	if err := receive(t, callString(s.Client, context.Background(), "invalid")).err; !errors.As(err, &protocol) {
		t.Fatalf("protocol failure: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.cmd.ProcessState == nil {
		t.Fatal("protocol failure leaked child process")
	}
}
