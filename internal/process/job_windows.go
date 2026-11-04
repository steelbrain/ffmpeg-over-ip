//go:build windows

package process

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// Windows has no parent-death signal. Instead, children are put in a job
// object created with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. This process holds
// the only handle to the job (it is not inheritable), so when this process
// exits for any reason (crash, taskkill /F, a service manager's hard stop)
// Windows closes the handle and terminates every process in the job.

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
)

const (
	jobObjectExtendedLimitInformationClass = 9
	jobObjectLimitKillOnJobClose           = 0x00002000

	processSetQuota  = 0x0100
	processTerminate = 0x0001
)

// jobObjectBasicLimitInformation mirrors JOBOBJECT_BASIC_LIMIT_INFORMATION.
type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// ioCounters mirrors IO_COUNTERS.
type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

// jobObjectExtendedLimitInformation mirrors
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION (144 bytes on 64-bit Windows).
type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

var (
	killJobOnce sync.Once
	killJob     syscall.Handle
	killJobErr  error
)

func createKillOnCloseJob() (syscall.Handle, error) {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	job := syscall.Handle(h)

	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	ok, _, err := procSetInformationJobObject.Call(
		uintptr(job),
		jobObjectExtendedLimitInformationClass,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if ok == 0 {
		syscall.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return job, nil
}

// KillOnExit makes Windows terminate the process pid when this process
// exits. The handle to the job is never closed, so it lives exactly as long
// as this process.
func KillOnExit(pid int) error {
	killJobOnce.Do(func() {
		killJob, killJobErr = createKillOnCloseJob()
	})
	if killJobErr != nil {
		return killJobErr
	}

	proc, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer syscall.CloseHandle(proc)

	ok, _, err := procAssignProcessToJobObject.Call(uintptr(killJob), uintptr(proc))
	if ok == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}
