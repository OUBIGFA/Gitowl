//go:build !windows

package backend

import "os/exec"

func hideConsole(*exec.Cmd) {}
