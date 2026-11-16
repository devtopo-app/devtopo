package proc

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the child gets a console, but no window.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// killTree makes cancelling cmd kill its whole process tree with taskkill,
// falling back to killing the direct child if taskkill fails.
func killTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), waitDelay)
		defer cancel()
		pid := strconv.Itoa(cmd.Process.Pid)
		if Hidden(exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", pid)).Run() == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
}
