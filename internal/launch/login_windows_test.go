package launch

import (
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// useTestKeys points start at login at its own keys, so the tests never touch
// the user's real startup list.
func useTestKeys(t *testing.T) {
	t.Helper()
	oldRun, oldApproved := runKey, approvedKey
	runKey = `Software\DevTopoTest\Run`
	approvedKey = `Software\DevTopoTest\Approved`
	t.Cleanup(func() { deleteTestKey(); runKey, approvedKey = oldRun, oldApproved })
}

// setTestEntry writes a Run entry directly, as an earlier start would have.
func setTestEntry(t *testing.T, entry string) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.SetStringValue(runValue, entry); err != nil {
		t.Fatal(err)
	}
}

// setTestApproved records what Windows' startup apps settings say about DevTopo.
func setTestApproved(t *testing.T, state byte) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.SetBinaryValue(runValue, append([]byte{state}, make([]byte, 11)...)); err != nil {
		t.Fatal(err)
	}
}

// Turning start at login on and off writes and removes the Run entry.
func TestStartAtLoginOnWindows(t *testing.T) {
	useTestKeys(t)

	if StartsAtLogin() {
		t.Fatal("off before it is turned on")
	}
	if err := SetStartAtLogin(true); err != nil || !StartsAtLogin() {
		t.Fatalf("on: %v", err)
	}
	// At login it starts in the tray, without opening the browser.
	k, _ := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	v, _, _ := k.GetStringValue(runValue)
	k.Close()
	if !strings.HasSuffix(v, `" tray --no-open`) {
		t.Fatalf("run entry: %q", v)
	}
	if err := SetStartAtLogin(false); err != nil || StartsAtLogin() {
		t.Fatalf("off: %v", err)
	}
}

// An entry for a program that has moved or gone cannot start anything, so it
// shows as off, and turning it on points it at this program.
func TestStartAtLoginEntryForMissingProgram(t *testing.T) {
	useTestKeys(t)
	setTestEntry(t, `"`+t.TempDir()+`\gone\devtopo.exe" tray --no-open`)

	if StartsAtLogin() {
		t.Fatal("shows as on for a program that does not exist")
	}
	if err := SetStartAtLogin(true); err != nil || !StartsAtLogin() {
		t.Fatalf("on: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	k, _ := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	v, _, _ := k.GetStringValue(runValue)
	k.Close()
	if !strings.HasPrefix(v, `"`+exe+`"`) {
		t.Fatalf("run entry: %q, want this program %q", v, exe)
	}
}

// An entry turned off in Windows' startup apps settings does not run at login,
// so it shows as off, and turning it on in DevTopo clears that.
func TestStartAtLoginDisabledInWindows(t *testing.T) {
	useTestKeys(t)
	if err := SetStartAtLogin(true); err != nil {
		t.Fatal(err)
	}

	setTestApproved(t, 0x02) // enabled
	if !StartsAtLogin() {
		t.Fatal("shows as off while Windows has it enabled")
	}
	setTestApproved(t, 0x03) // disabled
	if StartsAtLogin() {
		t.Fatal("shows as on while Windows has it disabled")
	}
	if err := SetStartAtLogin(true); err != nil || !StartsAtLogin() {
		t.Fatalf("on: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if _, _, err := k.GetBinaryValue(runValue); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("approval value still there: %v", err)
	}
}
