package host

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// Snapshot lists the running processes with their parents and start times,
// through ps.
func Snapshot() ([]Proc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ps", psArgs...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parsePS(out), nil
}
