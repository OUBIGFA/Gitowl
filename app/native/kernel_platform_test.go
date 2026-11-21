package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

// ui.Tester renders the production MyGo controls without an OS window. Only OS
// theme/proxy facts are supplied by this test boundary; all stores/Git stay real.
func kernelTestPlatform(work string) func(context.Context, backend.KernelPlatformRequest) (any, error) {
	var mu sync.Mutex
	dark := false
	return func(_ context.Context, request backend.KernelPlatformRequest) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		switch request.Operation {
		case "set-native-theme-source":
			var source string
			if err := json.Unmarshal(request.Args[0], &source); err != nil {
				return nil, err
			}
			dark = source == "dark"
			return nil, nil
		case "should-use-dark-colors":
			return dark, nil
		case "resolve-proxy-url":
			return nil, nil
		case "get-notifications-permission":
			return "denied", nil
		case "get-path":
			return work, nil
		default:
			return nil, fmt.Errorf("unexpected OS operation in control test: %s", request.Operation)
		}
	}
}

func seedKernelTestSettings(t *testing.T, node, profile string, welcomed bool) {
	t.Helper()
	dir := filepath.Join(profile, "backend-data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := "localStorage.setItem('stats-opt-out','1');localStorage.setItem('has-sent-stats-opt-in-ping','1');"
	if welcomed {
		source += "localStorage.setItem('has-shown-welcome-flow','1');"
	}
	cmd := exec.Command(node, "--experimental-webstorage", "--localstorage-file="+filepath.Join(dir, "settings.sqlite"), "-e", source)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed isolated settings: %v\n%s", err, out)
	}
}
