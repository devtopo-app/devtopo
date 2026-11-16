//go:build !windows && !darwin && !linux

package launch

import "errors"

// StartsAtLogin is always false where DevTopo has no startup support.
func StartsAtLogin() bool { return false }

// SetStartAtLogin is not supported on this system.
func SetStartAtLogin(on bool) error { return errors.New("start at login is not supported here") }
