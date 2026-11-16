// Package window finds an app's top-level window and brings it to the front,
// restoring it when minimized.
package window

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ErrNotFound means the app has no visible top-level window.
var ErrNotFound = errors.New("window not found")

var (
	user32dll                    = syscall.NewLazyDLL("user32.dll")
	kernel32dll                  = syscall.NewLazyDLL("kernel32.dll")
	procFindWindowW              = user32dll.NewProc("FindWindowW")
	procEnumWindows              = user32dll.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32dll.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible          = user32dll.NewProc("IsWindowVisible")
	procGetWindowTextW           = user32dll.NewProc("GetWindowTextW")
	procGetWindow                = user32dll.NewProc("GetWindow")
	procIsIconic                 = user32dll.NewProc("IsIconic")
	procShowWindow               = user32dll.NewProc("ShowWindow")
	procGetForegroundWindow      = user32dll.NewProc("GetForegroundWindow")
	procAttachThreadInput        = user32dll.NewProc("AttachThreadInput")
	procBringWindowToTop         = user32dll.NewProc("BringWindowToTop")
	procSetForegroundWindow      = user32dll.NewProc("SetForegroundWindow")
	procKeybdEvent               = user32dll.NewProc("keybd_event")
	procGetCurrentThreadId       = kernel32dll.NewProc("GetCurrentThreadId")
	procFreeConsole              = kernel32dll.NewProc("FreeConsole")
	procAttachConsole            = kernel32dll.NewProc("AttachConsole")
	procGetConsoleWindow         = kernel32dll.NewProc("GetConsoleWindow")
)

const (
	gwOwner         = 4
	vkMenu          = 0x12
	keyeventfKeyup  = 0x2
	createNoWindow  = 0x08000000
	consoleHelperOp = "console-window"
)

// RestoreTitle un-minimizes the window titled title.
func RestoreTitle(title string) error {
	t, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(t)))
	if hwnd == 0 {
		return ErrNotFound
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	return nil
}

// FindPID returns the first visible, unowned top-level window of pid.
func FindPID(pid int) (uintptr, error) {
	return find(pid, false, "")
}

// FindTitled returns pid's first visible, unowned top-level window whose title
// contains part.
func FindTitled(pid int, part string) (uintptr, error) {
	return find(pid, true, part)
}

// enumState is what enumProc looks for during one EnumWindows call. Callbacks
// made by syscall.NewCallback are never freed and the runtime allows only a
// couple of thousand, so enumProc is made once and reads its search from
// here; enumMu keeps concurrent searches apart. EnumWindows runs the callback
// on the calling thread before it returns.
var (
	enumMu    sync.Mutex
	enumState struct {
		pid      int
		byTitle  bool
		part     string
		found    uintptr
		titleBuf [512]uint16
	}
	enumProc = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		st := &enumState
		var owner uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if int(owner) != st.pid {
			return 1
		}
		if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
			return 1
		}
		if o, _, _ := procGetWindow.Call(hwnd, gwOwner); o != 0 {
			return 1
		}
		if st.byTitle {
			buf := st.titleBuf[:]
			n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if !strings.Contains(syscall.UTF16ToString(buf[:n]), st.part) {
				return 1
			}
		}
		st.found = hwnd
		return 0
	})
)

// find runs one EnumWindows search through the shared enumProc.
func find(pid int, byTitle bool, part string) (uintptr, error) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumState.pid, enumState.byTitle, enumState.part, enumState.found = pid, byTitle, part, 0
	procEnumWindows.Call(enumProc, 0)
	found := enumState.found
	enumState.part = ""
	if found == 0 {
		return 0, ErrNotFound
	}
	return found, nil
}

// Raise restores h if minimized and brings it to the front.
func Raise(h uintptr) error {
	// AttachThreadInput works on the calling OS thread, which must not change.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return raise(win32{}, h)
}

// ConsoleWindowOf finds the window showing pid's console: the Windows Terminal
// window hosting it, or the classic console window. Attaching to another
// console means giving up our own, so a short-lived, windowless copy of this
// binary does it (see ConsoleWindow).
func ConsoleWindowOf(pid int) (uintptr, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, consoleHelperOp, strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return 0, ErrNotFound
	}
	h, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || h == 0 {
		return 0, ErrNotFound
	}
	return uintptr(h), nil
}

// ConsoleWindow is the helper side of ConsoleWindowOf, run in its own process:
// it attaches to pid's console and returns the window that shows it. A console
// Windows Terminal hosts has a hidden pseudo window owned by the terminal
// window; a classic console's window is its own.
func ConsoleWindow(pid int) (uintptr, error) {
	procFreeConsole.Call()
	if ok, _, _ := procAttachConsole.Call(uintptr(pid)); ok == 0 {
		return 0, ErrNotFound
	}
	h, _, _ := procGetConsoleWindow.Call()
	if h == 0 {
		return 0, ErrNotFound
	}
	if owner, _, _ := procGetWindow.Call(h, gwOwner); owner != 0 {
		return owner, nil
	}
	return h, nil
}

// win32 is the real user32 behind raise.
type win32 struct{}

func (win32) IsIconic(h uintptr) bool {
	r, _, _ := procIsIconic.Call(h)
	return r != 0
}
func (win32) ShowWindow(h uintptr, cmd int) { procShowWindow.Call(h, uintptr(cmd)) }
func (win32) ForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}
func (win32) CurrentThread() uint32 {
	r, _, _ := procGetCurrentThreadId.Call()
	return uint32(r)
}
func (win32) WindowThread(h uintptr) uint32 {
	r, _, _ := procGetWindowThreadProcessId.Call(h, 0)
	return uint32(r)
}
func (win32) AttachThreadInput(from, to uint32, attach bool) {
	a := uintptr(0)
	if attach {
		a = 1
	}
	procAttachThreadInput.Call(uintptr(from), uintptr(to), a)
}
func (win32) BringWindowToTop(h uintptr) { procBringWindowToTop.Call(h) }
func (win32) PressAlt() {
	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procKeybdEvent.Call(vkMenu, 0, keyeventfKeyup, 0)
}
func (win32) SetForegroundWindow(h uintptr) bool {
	r, _, _ := procSetForegroundWindow.Call(h)
	return r != 0
}
