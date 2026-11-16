package main

import (
	"os"
	"syscall"
)

// attachConsole lets the release build, which is a GUI program with no console
// of its own, print to the terminal it was started from (for --version and the
// usage text). Output that is already redirected (to a file or a pipe) stays
// where it is; started by a double-click there is no terminal and it does
// nothing.
func attachConsole() {
	stdoutOK := validHandle(os.Stdout)
	stderrOK := validHandle(os.Stderr)
	if stdoutOK && stderrOK {
		return // a console build, or output already redirected
	}
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)
	if r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole").Call(attachParentProcess); r == 0 {
		return // no parent console
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	if !stdoutOK {
		os.Stdout = f
	}
	if !stderrOK {
		os.Stderr = f
	}
}

func validHandle(f *os.File) bool {
	if f == nil || f.Fd() == uintptr(syscall.InvalidHandle) || f.Fd() == 0 {
		return false
	}
	_, err := f.Stat()
	return err == nil
}
