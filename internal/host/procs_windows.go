package host

import (
	"syscall"
	"time"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

// Snapshot lists the running processes with their parents and start times. A
// process whose start time cannot be read (another user's) keeps a zero Start.
func Snapshot() ([]Proc, error) {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(snap)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []Proc
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		out = append(out, Proc{
			PID: int(e.ProcessID), PPID: int(e.ParentProcessID),
			Name: syscall.UTF16ToString(e.ExeFile[:]), Start: startTime(e.ProcessID),
		})
	}
	return out, nil
}

func startTime(pid uint32) time.Time {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return time.Time{}
	}
	defer syscall.CloseHandle(h)
	var created, exited, kernel, user syscall.Filetime
	if syscall.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return time.Time{}
	}
	return time.Unix(0, created.Nanoseconds())
}
