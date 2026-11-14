package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/OUBIGFA/Gitowl/app/native/internal/backend"
)

// The SDK still owns the original runtime and its JSON-RPC pipes. This internal
// entry supplies Windows console inheritance only, before any MyGo UI starts.
func runCopilotRuntime(arguments []string) int {
	input, err := os.Stdin.Stat()
	if len(arguments) == 0 || !filepath.IsAbs(arguments[0]) || err != nil || input.Mode()&os.ModeNamedPipe == 0 {
		fmt.Fprintln(os.Stderr, "Copilot runtime requires an absolute executable and the SDK input pipe")
		return 2
	}
	if err := backend.RunCopilotRuntime(arguments[0], arguments[1:]); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "Copilot runtime:", err)
		return 1
	}
	return 0
}
