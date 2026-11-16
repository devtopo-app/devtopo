package proc

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// A cmd.exe child whose own child keeps the output pipe open, as an npm .cmd
// shim does with node, must still stop soon after the context ends.
func TestKillTreeOnCancelEndsGrandchildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd := KillTreeOnCancel(Hidden(exec.CommandContext(ctx, "cmd", "/c", "ping -n 30 127.0.0.1")))
	start := time.Now()
	_, err := cmd.Output()
	if err == nil {
		t.Fatal("a cancelled command should fail")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("Output returned %v after start; the process tree outlived the context", d)
	}
}
