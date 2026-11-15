//go:build windows

package backend

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

const (
	createNoWindow            = 0x08000000
	jobObjectBasicProcessList = 3
	jobObjectExtendedLimit    = 9
	jobObjectKillOnClose      = 0x2000

	processSetInformation          = 0x0200
	processQueryLimitedInformation = 0x1000
	processPowerThrottling         = 4
	powerThrottlingExecutionSpeed  = 0x1
	idlePriorityClass              = 0x40
	belowNormalPriorityClass       = 0x4000
	normalPriorityClass            = 0x20

	snapThread               = 0x00000004
	threadSetInformation     = 0x0020
	threadQueryInformation   = 0x0040
	threadPowerThrottling    = 3
	threadPriorityNormal     = 0
	threadPriorityErrorValue = 0x7fffffff
)

// THREADENTRY32 for CreateToolhelp32Snapshot thread enumeration.
type threadEntry struct {
	Size, Usage, ThreadID, OwnerProcessID uint32
	BasePriority, DeltaPriority           int32
	Flags                                 uint32
}

type jobBasicLimits struct {
	PerProcessUserTime, PerJobUserTime int64
	Flags                              uint32
	MinWorkingSet, MaxWorkingSet       uintptr
	ActiveProcessLimit                 uint32
	Affinity                           uintptr
	PriorityClass, SchedulingClass     uint32
}

type jobExtendedLimits struct {
	Basic                                                      jobBasicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

// PROCESS_POWER_THROTTLING_STATE. A control bit with a clear state bit is an
// explicit HighQoS request that also opts out of the system's heuristics.
type powerThrottlingState struct {
	Version, ControlMask, StateMask uint32
}

// JOBOBJECT_BASIC_PROCESS_ID_LIST with room for the whole Electron tree.
type jobProcessList struct {
	Assigned, Listed uint32
	IDs              [256]uintptr
}

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	createJob        = kernel32.NewProc("CreateJobObjectW")
	setJobInfo       = kernel32.NewProc("SetInformationJobObject")
	queryJobInfo     = kernel32.NewProc("QueryInformationJobObject")
	assignJob        = kernel32.NewProc("AssignProcessToJobObject")
	closeWinHandle   = kernel32.NewProc("CloseHandle")
	setProcessInfo   = kernel32.NewProc("SetProcessInformation")
	getPriorityClass = kernel32.NewProc("GetPriorityClass")
	setPriorityClass = kernel32.NewProc("SetPriorityClass")
	snapshot         = kernel32.NewProc("CreateToolhelp32Snapshot")
	threadFirst      = kernel32.NewProc("Thread32First")
	threadNext       = kernel32.NewProc("Thread32Next")
	openThread       = kernel32.NewProc("OpenThread")
	setThreadInfo    = kernel32.NewProc("SetThreadInformation")
	getThreadPri     = kernel32.NewProc("GetThreadPriority")
	setThreadPri     = kernel32.NewProc("SetThreadPriority")
)

// The host joins one kill-on-close job before it starts any kernel. Children
// inherit job membership at creation, so Electron and every renderer/utility
// it spawns belong to the job from their first instruction: no race between
// start and assignment. The host holds the only handle for its whole life;
// when it exits for any reason, including a forced kill, Windows ends the
// remaining tree instead of leaving a kernel that holds the data directory.
var ownedJob struct {
	once   sync.Once
	handle uintptr
	err    error
}

func hostJob() (uintptr, error) {
	ownedJob.once.Do(func() {
		job, _, callErr := createJob.Call(0, 0)
		if job == 0 {
			ownedJob.err = fmt.Errorf("create Gitowl process job: %w", callErr)
			return
		}
		limits := jobExtendedLimits{Basic: jobBasicLimits{Flags: jobObjectKillOnClose}}
		if result, _, err := setJobInfo.Call(job, jobObjectExtendedLimit, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); result == 0 {
			_, _, _ = closeWinHandle.Call(job)
			ownedJob.err = fmt.Errorf("configure Gitowl process job: %w", err)
			return
		}
		self, _ := syscall.GetCurrentProcess()
		if result, _, err := assignJob.Call(job, uintptr(self)); result == 0 {
			_, _, _ = closeWinHandle.Call(job)
			ownedJob.err = fmt.Errorf("join Gitowl process job: %w", err)
			return
		}
		ownedJob.handle = job
	})
	return ownedJob.handle, ownedJob.err
}

func startOwnedCommand(cmd *exec.Cmd) (ownedProcess, error) {
	job, err := hostJob()
	if err != nil {
		return ownedProcess{}, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	if err := cmd.Start(); err != nil {
		return ownedProcess{}, err
	}
	return ownedProcess{
		job:        job,
		wait:       cmd.Wait,
		prioritize: func() int { return prioritizeJob(job) },
	}, nil
}

// prioritizeJob restores normal scheduling for every process of the owned
// tree: no idle/below-normal priority class and no EcoQoS. It returns how many
// processes were adjusted. Failures are skipped: a process may exit meanwhile.
func prioritizeJob(job uintptr) int {
	var list jobProcessList
	if result, _, _ := queryJobInfo.Call(job, jobObjectBasicProcessList, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0); result == 0 {
		return 0
	}
	adjusted := 0
	members := make(map[uint32]bool, list.Listed)
	for _, pid := range list.IDs[:min(int(list.Listed), len(list.IDs))] {
		members[uint32(pid)] = true
		handle, err := syscall.OpenProcess(processSetInformation|processQueryLimitedInformation, false, uint32(pid))
		if err != nil {
			continue
		}
		state := powerThrottlingState{Version: 1, ControlMask: powerThrottlingExecutionSpeed}
		changed, _, _ := setProcessInfo.Call(uintptr(handle), processPowerThrottling, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
		if class, _, _ := getPriorityClass.Call(uintptr(handle)); class == idlePriorityClass || class == belowNormalPriorityClass {
			if result, _, _ := setPriorityClass.Call(uintptr(handle), normalPriorityClass); result != 0 {
				changed = 1
			}
		}
		if changed != 0 {
			adjusted++
		}
		syscall.CloseHandle(handle)
	}
	return adjusted + prioritizeThreads(members)
}

// prioritizeThreads gives every thread of the owned tree HighQoS and at least
// normal priority. Chromium parks background pool threads in background mode
// (base priority 4) with thread EcoQoS; Windows then schedules them only on
// efficient cores. Ending a process needs each of its threads to run its exit
// path, so with those cores saturated a quit or even a forced job kill waits
// for minutes. Shutdown is user-blocking work; above-normal threads keep their
// priority. Threads that ended meanwhile are skipped.
func prioritizeThreads(members map[uint32]bool) int {
	snap, _, _ := snapshot.Call(snapThread, 0)
	if snap == uintptr(syscall.InvalidHandle) {
		return 0
	}
	defer syscall.CloseHandle(syscall.Handle(snap))
	adjusted := 0
	entry := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	for ok, _, _ := threadFirst.Call(snap, uintptr(unsafe.Pointer(&entry))); ok != 0; ok, _, _ = threadNext.Call(snap, uintptr(unsafe.Pointer(&entry))) {
		if !members[entry.OwnerProcessID] {
			continue
		}
		thread, _, _ := openThread.Call(threadSetInformation|threadQueryInformation, 0, uintptr(entry.ThreadID))
		if thread == 0 {
			continue
		}
		state := powerThrottlingState{Version: 1, ControlMask: powerThrottlingExecutionSpeed}
		changed, _, _ := setThreadInfo.Call(thread, threadPowerThrottling, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
		if priority, _, _ := getThreadPri.Call(thread); int32(priority) != threadPriorityErrorValue && int32(priority) < threadPriorityNormal {
			if result, _, _ := setThreadPri.Call(thread, threadPriorityNormal); result != 0 {
				changed = 1
			}
		}
		if changed != 0 {
			adjusted++
		}
		syscall.CloseHandle(syscall.Handle(thread))
	}
	return adjusted
}

func hideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
