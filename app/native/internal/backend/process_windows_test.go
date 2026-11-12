//go:build windows

package backend

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var getProcessInfo = kernel32.NewProc("GetProcessInformation")

func jobProcessIDs(t *testing.T, job uintptr) []uint32 {
	t.Helper()
	var list jobProcessList
	if result, _, err := queryJobInfo.Call(job, jobObjectBasicProcessList, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0); result == 0 {
		t.Fatal(err)
	}
	// The test host itself is a member, like Gitowl; only its children count.
	var ids []uint32
	for _, id := range list.IDs[:list.Listed] {
		if int(id) != os.Getpid() {
			ids = append(ids, uint32(id))
		}
	}
	return ids
}

func processScheduling(t *testing.T, pid uint32) (class uintptr, eco bool) {
	t.Helper()
	handle, err := syscall.OpenProcess(processQueryLimitedInformation|processSetInformation, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	class, _, _ = getPriorityClass.Call(uintptr(handle))
	state := powerThrottlingState{Version: 1}
	if result, _, err := getProcessInfo.Call(uintptr(handle), processPowerThrottling, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state)); result == 0 {
		t.Fatal(err)
	}
	return class, state.StateMask&powerThrottlingExecutionSpeed != 0
}

// The tree starts the way Chromium leaves a hidden renderer: idle priority
// class with EcoQoS. Children inherit the idle class from their parent.
func TestOwnedTreeIsRestoredToNormalSchedulingForShutdown(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "ping -n 30 127.0.0.1 >nul")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: idlePriorityClass}
	owned, err := startOwnedCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = owned.wait()
	}()
	deadline := time.Now().Add(10 * time.Second)
	var ids []uint32
	for {
		if ids = jobProcessIDs(t, owned.job); len(ids) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(ids) < 2 {
		t.Fatalf("child did not join the owned tree: %v", ids)
	}
	for _, pid := range ids {
		handle, err := syscall.OpenProcess(processSetInformation|processQueryLimitedInformation, false, pid)
		if err != nil {
			t.Fatal(err)
		}
		eco := powerThrottlingState{Version: 1, ControlMask: powerThrottlingExecutionSpeed, StateMask: powerThrottlingExecutionSpeed}
		if result, _, err := setProcessInfo.Call(uintptr(handle), processPowerThrottling, uintptr(unsafe.Pointer(&eco)), unsafe.Sizeof(eco)); result == 0 {
			t.Fatal(err)
		}
		syscall.CloseHandle(handle)
		if class, on := processScheduling(t, pid); class != idlePriorityClass || !on {
			t.Fatalf("fixture pid %d is not deprioritized: class=%#x eco=%v", pid, class, on)
		}
	}
	if adjusted := owned.prioritize(); adjusted < len(ids)+1 {
		t.Fatalf("adjusted %d of %d processes", adjusted, len(ids))
	}
	for _, pid := range ids {
		if class, on := processScheduling(t, pid); class != normalPriorityClass || on {
			t.Fatalf("pid %d still deprioritized: class=%#x eco=%v", pid, class, on)
		}
	}
}

// Helper mode: behave like the Gitowl host, start an owned child, report it,
// then stay alive until the parent test force-kills this process.
func TestOwnedTreeHelperHost(t *testing.T) {
	if os.Getenv("GITOWL_OWNED_TREE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	child := exec.Command("ping", "-n", "120", "127.0.0.1")
	if _, err := startOwnedCommand(child); err != nil {
		t.Fatal(err)
	}
	os.Stdout.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
	time.Sleep(time.Minute)
}

func TestForcedHostExitEndsOwnedTree(t *testing.T) {
	host := exec.Command(os.Args[0], "-test.run=^TestOwnedTreeHelperHost$", "-test.v")
	host.Env = append(os.Environ(), "GITOWL_OWNED_TREE_HELPER=1")
	// The helper nests its own job inside this test's job, as Windows allows;
	// its handle is the only owner of the child, like a separately run Gitowl.
	out, err := host.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(out)
	pid := 0
	for pid == 0 && lines.Scan() {
		pid, _ = strconv.Atoi(strings.TrimSpace(lines.Text()))
	}
	if pid == 0 {
		t.Fatal("helper did not start its owned child")
	}
	child, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(child)
	if event, _ := syscall.WaitForSingleObject(child, 0); event != uint32(syscall.WAIT_TIMEOUT) {
		t.Fatal("owned child ended before the host was killed")
	}
	if err := host.Process.Kill(); err != nil { // TerminateProcess, like Task Manager
		t.Fatal(err)
	}
	_ = host.Wait()
	if event, _ := syscall.WaitForSingleObject(child, 10_000); event != syscall.WAIT_OBJECT_0 {
		t.Fatal("owned child survived a forced host exit")
	}
}

var (
	getThreadInfo = kernel32.NewProc("GetThreadInformation")
	currentThread = kernel32.NewProc("GetCurrentThread")
	currentTID    = kernel32.NewProc("GetCurrentThreadId")
)

// Helper mode: park one OS thread exactly like a Chromium background pool
// thread (background mode, base priority 4, thread EcoQoS), report its ID and
// wait to be inspected.
func TestBackgroundThreadHelper(t *testing.T) {
	if os.Getenv("GITOWL_BACKGROUND_THREAD_HELPER") != "1" {
		t.Skip("helper process only")
	}
	runtime.LockOSThread()
	me, _, _ := currentThread.Call()
	if result, _, err := setThreadPri.Call(me, 0x00010000); result == 0 { // THREAD_MODE_BACKGROUND_BEGIN
		t.Fatal(err)
	}
	eco := powerThrottlingState{Version: 1, ControlMask: powerThrottlingExecutionSpeed, StateMask: powerThrottlingExecutionSpeed}
	if result, _, err := setThreadInfo.Call(me, threadPowerThrottling, uintptr(unsafe.Pointer(&eco)), unsafe.Sizeof(eco)); result == 0 {
		t.Fatal(err)
	}
	tid, _, _ := currentTID.Call()
	os.Stdout.WriteString(strconv.Itoa(int(tid)) + "\n")
	time.Sleep(time.Minute)
}

func TestOwnedTreeLeavesNoBackgroundThreadForShutdown(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestBackgroundThreadHelper$", "-test.v")
	cmd.Env = append(os.Environ(), "GITOWL_BACKGROUND_THREAD_HELPER=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	owned, err := startOwnedCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = owned.wait()
	}()
	lines := bufio.NewScanner(out)
	tid := 0
	for tid == 0 && lines.Scan() {
		tid, _ = strconv.Atoi(strings.TrimSpace(lines.Text()))
	}
	if tid == 0 {
		t.Fatal("helper did not park a background thread")
	}
	inspect := func() (int32, powerThrottlingState) {
		thread, _, err := openThread.Call(threadQueryInformation, 0, uintptr(tid))
		if thread == 0 {
			t.Fatal(err)
		}
		defer syscall.CloseHandle(syscall.Handle(thread))
		priority, _, _ := getThreadPri.Call(thread)
		state := powerThrottlingState{Version: 1}
		if result, _, err := getThreadInfo.Call(thread, threadPowerThrottling, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state)); result == 0 {
			t.Fatal(err)
		}
		return int32(priority), state
	}
	if priority, state := inspect(); priority >= threadPriorityNormal || state.StateMask == 0 {
		t.Fatalf("fixture thread is not in Chromium's background state: priority=%d qos=%+v", priority, state)
	}
	owned.prioritize()
	if priority, state := inspect(); priority < threadPriorityNormal || state.ControlMask&powerThrottlingExecutionSpeed == 0 || state.StateMask != 0 {
		t.Fatalf("background thread still deprioritized: priority=%d qos=%+v", priority, state)
	}
}
