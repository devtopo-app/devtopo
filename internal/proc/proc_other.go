//go:build !windows

package proc

import "os/exec"

func hide(*exec.Cmd) {}

// killTree keeps exec's default cancel, killing the direct child; WaitDelay
// covers any grandchild still holding the pipes.
func killTree(*exec.Cmd) {}
