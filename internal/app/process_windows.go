//go:build windows

package app

import (
	"os/exec"
	"syscall"
	"unsafe"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func openBrowser(url string) error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	prepareCommand(cmd)
	return cmd.Start()
}

// Job objects terminate owned Xray processes even if PortTide crashes or is killed.
// Layout: https://learn.microsoft.com/windows/win32/api/winnt/ns-winnt-jobobject_extended_limit_information
func guardProcess(cmd *exec.Cmd) (func(), error) {
	type basicLimits struct {
		ProcessTime, JobTime   int64
		Flags                  uint32
		MinWorking, MaxWorking uintptr
		Active                 uint32
		Affinity               uintptr
		Priority, Scheduling   uint32
	}
	type ioCounters struct{ ReadOps, WriteOps, OtherOps, ReadBytes, WriteBytes, OtherBytes uint64 }
	type extendedLimits struct {
		Basic                                          basicLimits
		IO                                             ioCounters
		ProcessMemory, JobMemory, PeakProcess, PeakJob uintptr
	}
	kernel := syscall.NewLazyDLL("kernel32.dll")
	job, _, e := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return nil, e
	}
	closeJob := func() { _ = syscall.CloseHandle(syscall.Handle(job)) }
	limits := extendedLimits{}
	limits.Basic.Flags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	ok, _, e := kernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		closeJob()
		return nil, e
	}
	process, e := syscall.OpenProcess(0x0101, false, uint32(cmd.Process.Pid))
	if e != nil {
		closeJob()
		return nil, e
	}
	defer syscall.CloseHandle(process)
	ok, _, e = kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
	if ok == 0 {
		closeJob()
		return nil, e
	}
	return closeJob, nil
}
