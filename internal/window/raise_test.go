package window

import (
	"fmt"
	"strings"
	"testing"
)

// fakeUser32 records the calls; its windows live on threads 1 (ours), 2 (the
// foreground app) and 3 (the target).
type fakeUser32 struct {
	calls    []string
	iconic   bool
	refusals int // how many SetForegroundWindow calls fail first
	fg       uintptr
}

func (f *fakeUser32) log(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}
func (f *fakeUser32) IsIconic(h uintptr) bool       { return f.iconic }
func (f *fakeUser32) ShowWindow(h uintptr, cmd int) { f.log("show %d %d", h, cmd) }
func (f *fakeUser32) ForegroundWindow() uintptr     { return f.fg }
func (f *fakeUser32) CurrentThread() uint32         { return 1 }
func (f *fakeUser32) WindowThread(h uintptr) uint32 {
	if h == f.fg {
		return 2
	}
	return 3
}
func (f *fakeUser32) AttachThreadInput(from, to uint32, attach bool) {
	f.log("attach %d %d %v", from, to, attach)
}
func (f *fakeUser32) BringWindowToTop(h uintptr) { f.log("top %d", h) }
func (f *fakeUser32) PressAlt()                  { f.log("alt") }
func (f *fakeUser32) SetForegroundWindow(h uintptr) bool {
	f.log("foreground %d", h)
	if f.refusals > 0 {
		f.refusals--
		return false
	}
	return true
}

func TestRaiseRestoresAMinimizedWindowAndBorrowsTheForegroundThreadsInput(t *testing.T) {
	f := &fakeUser32{iconic: true, fg: 50}
	if err := raise(f, 9); err != nil {
		t.Fatal(err)
	}
	want := "show 9 9|attach 1 2 true|top 9|foreground 9|attach 1 2 false"
	if got := strings.Join(f.calls, "|"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// When Windows still refuses, a synthetic ALT press makes this process the
// last to get input, which lifts the foreground lock.
func TestRaisePressesAltWhenRefused(t *testing.T) {
	f := &fakeUser32{fg: 50, refusals: 1}
	if err := raise(f, 9); err != nil {
		t.Fatal(err)
	}
	want := "attach 1 2 true|top 9|foreground 9|alt|foreground 9|attach 1 2 false"
	if got := strings.Join(f.calls, "|"); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	f = &fakeUser32{fg: 50, refusals: 2}
	if err := raise(f, 9); err == nil {
		t.Fatal("a window that never came forward is an error")
	}
}

// No input to borrow when nothing is in the foreground, or it is our own.
func TestRaiseAttachesOnlyToAnotherThread(t *testing.T) {
	f := &fakeUser32{}
	if err := raise(f, 9); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls, "|"); got != "top 9|foreground 9" {
		t.Fatalf("got %s", got)
	}
}
