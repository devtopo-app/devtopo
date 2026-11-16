package proc

import (
	"os/exec"
	"testing"
)

func TestHiddenRuns(t *testing.T) {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	out, err := Hidden(exec.Command(bin, "--version")).Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("a hidden command should still run and return its output: %q, %v", out, err)
	}
}
