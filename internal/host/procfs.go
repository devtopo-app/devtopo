package host

import (
	"bytes"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"
)

// The Linux process snapshot reads /proc. The parsing lives here, free of any
// build tag, so it is tested on every OS with made-up file contents; only
// procs_linux.go touches the real /proc.

// clockTicks is USER_HZ, the unit of a process's start time in
// /proc/<pid>/stat. The kernel fixes it at 100 on every architecture Go
// supports for Linux (it is what sysconf(_SC_CLK_TCK) returns).
const clockTicks = 100

// procFS is the part of /proc the snapshot reads. Paths are relative to /proc,
// such as "1234/stat". A file that cannot be read (the process exited, or
// belongs to another user) returns an error and the snapshot moves on.
type procFS struct {
	// pids lists /proc's numeric entries.
	pids     func() ([]string, error)
	readFile func(name string) ([]byte, error)
	readlink func(name string) (string, error)
}

// snapshotProc lists the processes in fs with their parents, names and start
// times. A process that vanishes or cannot be read midway is skipped; only a
// /proc that cannot be listed, or a boot time that cannot be read, fails.
func snapshotProc(fs procFS) ([]Proc, error) {
	stat, err := fs.readFile("stat")
	if err != nil {
		return nil, err
	}
	boot, err := parseBootTime(stat)
	if err != nil {
		return nil, err
	}
	pids, err := fs.pids()
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, name := range pids {
		pid, err := strconv.Atoi(name)
		if err != nil || pid <= 0 {
			continue
		}
		b, err := fs.readFile(name + "/stat")
		if err != nil {
			continue
		}
		st, err := parseProcStat(b)
		if err != nil {
			continue
		}
		exe, _ := fs.readlink(name + "/exe")
		cmdline, _ := fs.readFile(name + "/cmdline")
		out = append(out, Proc{
			PID: pid, PPID: st.ppid,
			Name:  procName(exe, cmdline, st.comm),
			Start: boot.Add(time.Duration(st.startTicks) * time.Second / clockTicks),
		})
	}
	return out, nil
}

// procStat is what the snapshot takes from /proc/<pid>/stat.
type procStat struct {
	comm       string
	ppid       int
	startTicks uint64
}

// parseProcStat reads /proc/<pid>/stat: "pid (comm) state ppid ... starttime
// ...", starttime being the 22nd field, in clock ticks since boot. comm may
// itself hold spaces and parentheses, so it runs to the last ')'.
func parseProcStat(b []byte) (procStat, error) {
	open, end := bytes.IndexByte(b, '('), bytes.LastIndexByte(b, ')')
	if open < 0 || end < open {
		return procStat{}, errors.New("stat: no comm")
	}
	// the fields after comm, from state (field 3) on
	f := strings.Fields(string(b[end+1:]))
	if len(f) < 20 {
		return procStat{}, errors.New("stat: short")
	}
	ppid, err := strconv.Atoi(f[1])
	if err != nil {
		return procStat{}, err
	}
	start, err := strconv.ParseUint(f[19], 10, 64)
	if err != nil {
		return procStat{}, err
	}
	return procStat{comm: string(b[open+1 : end]), ppid: ppid, startTicks: start}, nil
}

// parseBootTime reads the boot time from /proc/stat's "btime" line, in seconds
// since the epoch.
func parseBootTime(b []byte) (time.Time, error) {
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "btime" {
			s, err := strconv.ParseInt(f[1], 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(s, 0), nil
		}
	}
	return time.Time{}, errors.New("stat: no btime")
}

// procName names a process by its executable's file name (comm is cut to 15
// bytes, so gnome-terminal-server reads as "gnome-terminal-"), else by its
// argv[0], else by comm. The exe link is unreadable for another user's
// process, and a kernel thread has neither.
func procName(exe string, cmdline []byte, comm string) string {
	exe = strings.TrimSuffix(exe, " (deleted)")
	if exe != "" {
		return path.Base(exe)
	}
	if arg0, _, _ := bytes.Cut(cmdline, []byte{0}); len(arg0) > 0 {
		// a process may rewrite its argv into one space-separated string
		first, _, _ := strings.Cut(string(arg0), " ")
		if first != "" {
			return path.Base(first)
		}
	}
	return comm
}
