package host

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// statLine is a /proc/<pid>/stat line for a process with the comm, parent and
// start time (clock ticks since boot) given.
func statLine(pid int, comm string, ppid int, ticks uint64) string {
	return fmt.Sprintf("%d (%s) S %d %d %d 0 -1 4194304 100 0 0 0 1 2 0 0 20 0 1 0 %d 1000 100 18446744073709551615 0 0 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n", pid, comm, ppid, pid, pid, ticks)
}

func TestParseProcStatReadsCommParentAndStart(t *testing.T) {
	got, err := parseProcStat([]byte(statLine(77, "my (odd) name", 12, 123456)))
	if err != nil || got.comm != "my (odd) name" || got.ppid != 12 || got.startTicks != 123456 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"", "77 nocomm S 1", "77 (x) S 1 2 3", statLine(77, "x", 12, 1)[:40]} {
		if _, err := parseProcStat([]byte(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestParseBootTime(t *testing.T) {
	got, err := parseBootTime([]byte("cpu  1 2 3\nintr 5\nbtime 1793260800\nprocesses 9\n"))
	if err != nil || !got.Equal(time.Unix(1793260800, 0)) {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := parseBootTime([]byte("cpu 1\n")); err == nil {
		t.Fatal("no btime parsed")
	}
}

// The exe's file name beats comm, which the kernel cuts to 15 bytes; without
// the exe (another user's process) argv[0] names it, and a kernel thread,
// with neither, keeps its comm.
func TestProcNamePrefersTheExecutable(t *testing.T) {
	for _, c := range []struct {
		exe, cmdline, comm, want string
	}{
		{"/usr/libexec/gnome-terminal-server", "/usr/libexec/gnome-terminal-server\x00", "gnome-terminal-", "gnome-terminal-server"},
		{"/usr/share/code/code (deleted)", "", "code", "code"},
		{"", "/usr/bin/konsole\x00--workdir\x00/w\x00", "konsole", "konsole"},
		{"", "-bash\x00", "bash", "-bash"},
		{"", "/opt/app/bin/kitty --single-instance\x00", "kitty", "kitty"},
		{"", "", "kworker/0:1", "kworker/0:1"},
	} {
		if got := procName(c.exe, []byte(c.cmdline), c.comm); got != c.want {
			t.Errorf("%q %q %q: %q", c.exe, c.cmdline, c.comm, got)
		}
	}
}

// fakeProc is a /proc held in maps; a pid listed with no files vanished
// between the listing and the read.
func fakeProc(pids []string, files, links map[string]string) procFS {
	return procFS{
		pids: func() ([]string, error) { return pids, nil },
		readFile: func(name string) ([]byte, error) {
			if b, ok := files[name]; ok {
				return []byte(b), nil
			}
			return nil, os.ErrNotExist
		},
		readlink: func(name string) (string, error) {
			if l, ok := links[name]; ok {
				return l, nil
			}
			return "", os.ErrPermission
		},
	}
}

// A process that vanishes or cannot be read is skipped; one whose exe link is
// another user's is named by its argv[0]. Start is the boot time plus its
// ticks, at 100 a second.
func TestSnapshotProcReadsEveryProcessItCan(t *testing.T) {
	boot := time.Unix(1793260800, 0)
	fs := fakeProc(
		[]string{"1", "self", "500", "501", "502", "503", "504"},
		map[string]string{
			"stat":        "cpu 1\nbtime 1793260800\n",
			"1/stat":      statLine(1, "systemd", 0, 1),
			"500/stat":    statLine(500, "gnome-terminal-", 1, 100050),
			"500/cmdline": "/usr/libexec/gnome-terminal-server\x00",
			"501/stat":    statLine(501, "bash", 500, 100100),
			"501/cmdline": "bash\x00",
			"502/stat":    statLine(502, "claude", 501, 100150),
			"503/stat":    "garbage",
			// 504 exited after the listing
		},
		map[string]string{
			"501/exe": "/usr/bin/bash",
			"502/exe": "/home/me/.local/share/claude/versions/2.1.0",
		},
	)
	got, err := snapshotProc(fs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Proc{
		{PID: 1, PPID: 0, Name: "systemd", Start: boot.Add(10 * time.Millisecond)},
		{PID: 500, PPID: 1, Name: "gnome-terminal-server", Start: boot.Add(1000500 * time.Millisecond)},
		{PID: 501, PPID: 500, Name: "bash", Start: boot.Add(1001 * time.Second)},
		{PID: 502, PPID: 501, Name: "2.1.0", Start: boot.Add(1001500 * time.Millisecond)},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}

	if _, err := snapshotProc(fakeProc(nil, map[string]string{"stat": "cpu 1\n"}, nil)); err == nil {
		t.Fatal("no boot time: no error")
	}
	fs.pids = func() ([]string, error) { return nil, errors.New("no /proc") }
	if _, err := snapshotProc(fs); err == nil {
		t.Fatal("no listing: no error")
	}
}

// A session under a Linux terminal or editor is classified by the /proc
// names. Claude Code writes procStart as ps's lstart in UTC on Linux, as on
// macOS.
func TestDetectClassifiesLinuxHosts(t *testing.T) {
	dir := t.TempDir()
	procs := []Proc{
		{PID: 1, Name: "systemd", Start: t0},
		{PID: 10, PPID: 1, Name: "gnome-terminal-server", Start: t0},
		{PID: 11, PPID: 10, Name: "bash", Start: t0},
		{PID: 12, PPID: 11, Name: "2.1.0", Start: t0},

		{PID: 20, PPID: 1, Name: "code", Start: t0},
		{PID: 21, PPID: 20, Name: "code", Start: t0},
		{PID: 22, PPID: 21, Name: "node", Start: t0},

		{PID: 30, PPID: 1, Name: "cursor", Start: t0},
		{PID: 31, PPID: 30, Name: "zsh", Start: t0},
		{PID: 32, PPID: 31, Name: "codex", Start: t0},

		{PID: 40, PPID: 1, Name: "kitty", Start: t0},
		{PID: 41, PPID: 40, Name: "fish", Start: t0},
		{PID: 42, PPID: 41, Name: "node", Start: t0},

		{PID: 50, PPID: 1, Name: "tmux: server", Start: t0},
		{PID: 51, PPID: 50, Name: "bash", Start: t0},
		{PID: 52, PPID: 51, Name: "2.1.0", Start: t0},
	}
	for _, e := range []struct {
		pid     int
		session string
	}{{12, "gnome"}, {22, "code"}, {32, "cursor"}, {42, "kitty"}, {52, "tmux"}} {
		writeProcStart(t, dir, e.pid, e.session, "Thu Oct 29 09:00:00 2026")
	}
	got := Detect(dir, procs)
	want := map[string]Live{
		"gnome":  {PID: 12, Host: LinuxTerminal, HostPID: 10},
		"code":   {PID: 22, Host: VSCode, HostPID: 21},
		"cursor": {PID: 32, Host: Cursor, HostPID: 30},
		"kitty":  {PID: 42, Host: LinuxTerminal, HostPID: 40},
		"tmux":   {PID: 52},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}
