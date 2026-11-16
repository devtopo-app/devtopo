// Package proc prepares the commands DevTopo runs in the background (git, gh,
// claude, codex, orca) so they never show a window.
package proc

import (
	"os/exec"
	"time"
)

// Hidden returns cmd set up to run without a console window. The tray build
// has no console of its own, so on Windows every console program it starts
// would otherwise flash a new console window on screen. Elsewhere it is cmd
// unchanged.
func Hidden(cmd *exec.Cmd) *exec.Cmd {
	hide(cmd)
	return cmd
}

// waitDelay bounds how long Wait keeps reading a command's output after its
// context ends; see KillTreeOnCancel.
const waitDelay = 5 * time.Second

// KillTreeOnCancel returns cmd, made by exec.CommandContext, set up so that
// cancelling its context really ends it. By default only the direct child is
// killed, and Wait blocks for as long as any grandchild holds the output pipes
// open: an npm-installed claude or codex on Windows is a .cmd shim, so cmd.exe
// is the child and node the grandchild that keeps running. On Windows it kills
// the whole process tree; everywhere Wait gives up on the pipes waitDelay
// after the context ends.
func KillTreeOnCancel(cmd *exec.Cmd) *exec.Cmd {
	cmd.WaitDelay = waitDelay
	killTree(cmd)
	return cmd
}
