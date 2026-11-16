package window

import "errors"

const swRestore = 9

// user32 is the slice of Win32 a raise needs, so tests can check the sequence.
type user32 interface {
	IsIconic(h uintptr) bool
	ShowWindow(h uintptr, cmd int)
	ForegroundWindow() uintptr
	CurrentThread() uint32
	WindowThread(h uintptr) uint32
	AttachThreadInput(from, to uint32, attach bool)
	BringWindowToTop(h uintptr)
	PressAlt()
	SetForegroundWindow(h uintptr) bool
}

// raise restores h if minimized and brings it to the front. Windows refuses
// the foreground to a background process, so it first shares the input state
// of the foreground window's thread, and if still refused presses ALT, which
// makes this process the last to get input.
func raise(u user32, h uintptr) error {
	if u.IsIconic(h) {
		u.ShowWindow(h, swRestore)
	}
	if fg := u.ForegroundWindow(); fg != 0 {
		cur, other := u.CurrentThread(), u.WindowThread(fg)
		if other != cur {
			u.AttachThreadInput(cur, other, true)
			defer u.AttachThreadInput(cur, other, false)
		}
	}
	u.BringWindowToTop(h)
	if u.SetForegroundWindow(h) {
		return nil
	}
	u.PressAlt()
	if u.SetForegroundWindow(h) {
		return nil
	}
	return errors.New("window not raised")
}
