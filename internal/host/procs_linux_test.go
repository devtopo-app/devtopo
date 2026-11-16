package host

import (
	"os"
	"testing"
	"time"
)

// The real /proc lists this test's own process, under its parent, started a
// moment ago.
func TestSnapshotListsThisProcess(t *testing.T) {
	procs, err := Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		if p.PID != os.Getpid() {
			continue
		}
		if p.PPID != os.Getppid() || p.Name == "" {
			t.Fatalf("got %+v", p)
		}
		if age := time.Since(p.Start); age < -2*time.Second || age > time.Hour {
			t.Fatalf("started %v ago", age)
		}
		return
	}
	t.Fatalf("pid %d not among %d processes", os.Getpid(), len(procs))
}
