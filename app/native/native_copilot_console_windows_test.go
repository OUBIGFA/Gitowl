//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

type copilotConsoleProbe struct {
	Window  uintptr `json:"window"`
	Visible bool    `json:"visible"`
}

func currentCopilotConsole() copilotConsoleProbe {
	window, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	visible, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("IsWindowVisible").Call(window)
	return copilotConsoleProbe{Window: window, Visible: visible != 0}
}

// An absent console is not sufficient: PowerShell's NativeCommandProcessor
// calls AllocConsole before hiding it when a hook starts another console tool.
// The real launcher must give the runtime and that hook one already-hidden
// console while keeping the SDK's stdin/stdout transport intact.
func TestNativeCopilotLauncherPreservesPipesAndHiddenConsoleForHooks(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestNativeCopilotLauncherHost$")
	cmd.Env = append(os.Environ(), "GITOWL_COPILOT_CONSOLE_TEST=host")
	cmd.Stdin = strings.NewReader("SDK input: 中文 and spaces\n")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native Copilot launcher failed: %v\n%s", err, output)
	}
	var probes []copilotConsoleProbe
	for _, line := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(line, "CONSOLE:"); ok {
			var probe copilotConsoleProbe
			if err := json.Unmarshal([]byte(value), &probe); err != nil {
				t.Fatal(err)
			}
			probes = append(probes, probe)
		}
	}
	if len(probes) != 2 || !strings.Contains(string(output), "SDK input: 中文 and spaces") || !strings.Contains(string(output), "SDK stderr intact") {
		t.Fatalf("runtime/hook or SDK pipes missing: %s", output)
	}
	for i, probe := range probes {
		if probe.Window == 0 || probe.Visible {
			t.Errorf("runtime/hook %d has no preexisting hidden console: %+v", i, probe)
		}
	}
	if probes[0].Window != probes[1].Window {
		t.Error("PowerShell's child did not inherit the runtime's hidden console")
	}
}

func TestNativeCopilotLauncherHost(t *testing.T) {
	if os.Getenv("GITOWL_COPILOT_CONSOLE_TEST") != "host" {
		t.Skip("subprocess entry only")
	}
	os.Setenv("GITOWL_COPILOT_CONSOLE_TEST", "runtime")
	os.Args = []string{os.Args[0], "--gitowl-copilot-runtime", os.Args[0], "-test.run=^TestNativeCopilotConsoleRuntime$"}
	if os.Getenv("GITOWL_COPILOT_WAIT_TEST") == "1" {
		os.Args[3] = "-test.run=^TestNativeCopilotWaitingRuntime$"
	}
	main()
}

func TestNativeCopilotCancellationEndsRuntimeAndHook(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	host := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeCopilotLauncherHost$")
	host.Env = append(os.Environ(), "GITOWL_COPILOT_CONSOLE_TEST=host", "GITOWL_COPILOT_WAIT_TEST=1")
	host.Stdin = strings.NewReader("")
	host.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := host.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	var runtimePID, hookPID uint32
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		if _, err := fmt.Sscanf(lines.Text(), "WAIT:%d,%d", &runtimePID, &hookPID); err == nil {
			break
		}
	}
	if runtimePID == 0 || hookPID == 0 {
		t.Fatal("launcher did not start the runtime and hook")
	}
	var handles []syscall.Handle
	for _, pid := range []uint32{runtimePID, hookPID} {
		handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, pid)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.CloseHandle(handle)
		handles = append(handles, handle)
	}
	// This is how SDK forceStop ends its cliProcess. Descendants must not
	// survive merely because the process it owns now supplies console setup.
	if err := host.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	for _, handle := range handles {
		if event, _ := syscall.WaitForSingleObject(handle, 10_000); event != syscall.WAIT_OBJECT_0 {
			t.Fatal("Copilot runtime or hook survived SDK cancellation")
		}
	}
}

func TestNativeCopilotWaitingRuntime(t *testing.T) {
	if os.Getenv("GITOWL_COPILOT_CONSOLE_TEST") != "runtime" || os.Getenv("GITOWL_COPILOT_WAIT_TEST") != "1" {
		t.Skip("subprocess entry only")
	}
	hook := exec.Command("cmd.exe", "/d", "/c", "ping -n 120 127.0.0.1 >nul")
	if err := hook.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("WAIT:%d,%d\n", os.Getpid(), hook.Process.Pid)
	_ = hook.Wait()
}

func TestNativeCopilotConsoleRuntime(t *testing.T) {
	if os.Getenv("GITOWL_COPILOT_CONSOLE_TEST") != "runtime" {
		t.Skip("subprocess entry only")
	}
	value, _ := json.Marshal(currentCopilotConsole())
	fmt.Printf("CONSOLE:%s\n", value)
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Print(string(input))
	fmt.Fprintln(os.Stderr, "SDK stderr intact")
	// This is the nesting used by a global PowerShell hook. Deliberately let
	// PowerShell start its child normally; the console must already be there.
	command := "& '" + strings.ReplaceAll(os.Args[0], "'", "''") + "' '-test.run=^TestNativeCopilotConsoleLeaf$'"
	child := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command)
	child.Env = append(os.Environ(), "GITOWL_COPILOT_CONSOLE_TEST=leaf")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeCopilotConsoleLeaf(t *testing.T) {
	if os.Getenv("GITOWL_COPILOT_CONSOLE_TEST") != "leaf" {
		t.Skip("subprocess entry only")
	}
	value, _ := json.Marshal(currentCopilotConsole())
	fmt.Printf("CONSOLE:%s\n", value)
}
