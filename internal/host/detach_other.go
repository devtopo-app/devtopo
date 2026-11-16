//go:build !linux

package host

import "os/exec"

// detach does nothing off Linux, where no terminal is started by execStartIn.
func detach(*exec.Cmd) {}
