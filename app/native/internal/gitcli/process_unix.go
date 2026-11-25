//go:build !windows

package gitcli

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
