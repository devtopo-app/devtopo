//go:build !windows

package launch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeShell writes an executable shell script and returns its path.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// A shell whose rc file leaves a program running in the background (an agent,
// a daemon) holds the output open after it exits; that must not stall the start.
func TestShellPATHDoesNotWaitForBackgroundPrograms(t *testing.T) {
	sh := fakeShell(t, "sleep 30 &\nprintf '"+pathMarker+"/opt/tools/bin'")
	start := time.Now()
	got := shellPATH(sh, time.Second)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %v", took)
	}
	if got != "/opt/tools/bin" {
		t.Fatalf("got %q", got)
	}
}

// A shell that hangs is given up on.
func TestShellPATHGivesUpOnAHangingShell(t *testing.T) {
	sh := fakeShell(t, "sleep 30")
	start := time.Now()
	if got := shellPATH(sh, time.Second); got != "" {
		t.Fatalf("got %q", got)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %v", took)
	}
}
