package launch

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user list of programs Windows starts at login.
var runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// approvedKey is where Windows' startup apps settings record which Run entries
// the user has turned off.
var approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`

const runValue = "DevTopo"

// StartsAtLogin reports whether Windows will start DevTopo at login: it is in
// the user's startup list, the program it names still exists, and the user has
// not turned it off in Windows' startup apps settings.
func StartsAtLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	entry, _, err := k.GetStringValue(runValue)
	return err == nil && !programMissing(entry) && !disabledInWindows(runValue)
}

// programMissing reports whether a Run entry's quoted program is gone, as when
// the program has moved since the entry was written.
func programMissing(entry string) bool {
	_, rest, ok := strings.Cut(entry, `"`)
	if !ok {
		return false
	}
	exe, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return false
	}
	_, err := os.Stat(exe)
	return errors.Is(err, os.ErrNotExist)
}

// disabledInWindows reports whether the user turned DevTopo off in Windows'
// startup apps settings. Windows records that as an odd first byte (3 or 7,
// against 2 or 6 for enabled), and it overrides the Run entry.
func disabledInWindows(value string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(value)
	return err == nil && len(b) > 0 && b[0]&1 == 1
}

// clearDisabledInWindows drops the record of DevTopo being turned off in
// Windows' startup apps settings, so that the Run entry takes effect.
func clearDisabledInWindows(value string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(value); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// SetStartAtLogin adds this program to the user's startup list, or removes it.
// Turning it on is the user's explicit choice, so it also replaces an entry for
// a program that has moved and clears a block from Windows' startup settings.
func SetStartAtLogin(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// At login: in the tray, without opening the browser.
	if err := k.SetStringValue(runValue, `"`+exe+`" tray --no-open`); err != nil {
		return err
	}
	return clearDisabledInWindows(runValue)
}

// deleteTestKey removes the keys a test made (runKey, approvedKey and their
// parent).
func deleteTestKey() {
	_ = registry.DeleteKey(registry.CURRENT_USER, runKey)
	_ = registry.DeleteKey(registry.CURRENT_USER, approvedKey)
	_ = registry.DeleteKey(registry.CURRENT_USER, `Software\DevTopoTest`)
}
