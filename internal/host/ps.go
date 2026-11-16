package host

import (
	"strconv"
	"strings"
	"time"
)

// psArgs lists every process as pid, parent, start and name. It runs with
// LC_ALL=C and TZ=UTC, as Claude Code runs ps for procStart, so both read alike.
var psArgs = []string{"-axo", "pid=,ppid=,lstart=,comm="}

// parsePS reads ps's output for psArgs. lstart is five words and comm, last,
// may hold spaces. A line that does not parse is skipped.
func parsePS(out []byte) []Proc {
	var procs []Proc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		start, err3 := lstart(strings.Join(f[2:7], " "))
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		name := line
		for i := 0; i < 7; i++ {
			_, name, _ = strings.Cut(strings.TrimLeft(name, " "), " ")
		}
		procs = append(procs, Proc{PID: pid, PPID: ppid, Name: strings.TrimSpace(name), Start: start})
	}
	return procs
}

// lstart reads ps's lstart in the C locale, "Thu Oct  1 08:00:00 2026", as UTC.
func lstart(s string) (time.Time, error) {
	return time.Parse("Mon Jan 2 15:04:05 2006", strings.Join(strings.Fields(s), " "))
}
