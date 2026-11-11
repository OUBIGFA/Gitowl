//go:build windows

package backend

import (
	"os/exec"
	"syscall"
)

// The stdio helper is not a second application window.
func hideConsole(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
