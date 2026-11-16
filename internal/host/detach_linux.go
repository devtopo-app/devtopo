package host

import (
	"os/exec"
	"syscall"
)

// detach starts cmd in a session of its own, so a terminal window it opens
// is not tied to DevTopo's.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
