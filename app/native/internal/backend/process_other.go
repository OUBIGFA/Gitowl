//go:build !windows

package backend

import "os/exec"

func hideConsole(*exec.Cmd) {}

func configureCopilotConsole(*exec.Cmd) {}

func startOwnedCommand(cmd *exec.Cmd) (ownedProcess, error) {
	if err := cmd.Start(); err != nil {
		return ownedProcess{}, err
	}
	return ownedProcess{wait: cmd.Wait, prioritize: func() int { return 0 }}, nil
}
