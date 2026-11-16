package host

import (
	"os"
	"path/filepath"
)

// Snapshot lists the running processes with their parents and start times,
// from /proc. A process that exits while it is read, or whose files another
// user owns, is skipped or named by what can be read.
func Snapshot() ([]Proc, error) {
	return snapshotProc(procFS{
		pids: func() ([]string, error) {
			d, err := os.Open("/proc")
			if err != nil {
				return nil, err
			}
			defer d.Close()
			return d.Readdirnames(-1)
		},
		readFile: func(name string) ([]byte, error) { return os.ReadFile(filepath.Join("/proc", name)) },
		readlink: func(name string) (string, error) { return os.Readlink(filepath.Join("/proc", name)) },
	})
}
