package host

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)

// fileTime is t as a Windows FILETIME, the unit of procStart in the registry.
func fileTime(t time.Time) int64 { return t.UnixNano()/100 + 116444736000000000 }

func writeEntry(t *testing.T, dir string, pid int, session string, start time.Time) {
	t.Helper()
	writeProcStart(t, dir, pid, session, fmt.Sprint(fileTime(start)))
}

// writeProcStart writes a registry entry with procStart as given: a FILETIME
// on Windows, ps's lstart in UTC on macOS.
func writeProcStart(t *testing.T, dir string, pid int, session, procStart string) {
	t.Helper()
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"cwd":"C:\\w","procStart":%q,"entrypoint":"cli"}`, pid, session, procStart)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprint(pid)+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDetectClassifiesTheHostOfEachLiveSession(t *testing.T) {
	dir := t.TempDir()
	procs := []Proc{
		{PID: 1, Name: "explorer.exe", Start: t0},
		{PID: 10, PPID: 1, Name: "Orca.exe", Start: t0},
		{PID: 11, PPID: 10, Name: "Orca.exe", Start: t0},
		{PID: 12, PPID: 11, Name: "pwsh.exe", Start: t0},
		{PID: 13, PPID: 12, Name: "claude.exe", Start: t0},

		{PID: 20, PPID: 1, Name: "Code.exe", Start: t0},
		{PID: 21, PPID: 20, Name: "Code.exe", Start: t0},
		{PID: 22, PPID: 21, Name: "claude.exe", Start: t0},

		{PID: 30, PPID: 1, Name: "Cursor.exe", Start: t0},
		{PID: 31, PPID: 30, Name: "claude.exe", Start: t0},

		{PID: 40, PPID: 1, Name: "WindowsTerminal.exe", Start: t0},
		{PID: 41, PPID: 40, Name: "pwsh.exe", Start: t0},
		{PID: 42, PPID: 41, Name: "claude.exe", Start: t0},

		{PID: 50, PPID: 1, Name: "powershell.exe", Start: t0},
		{PID: 51, PPID: 50, Name: "cmd.exe", Start: t0},
		{PID: 52, PPID: 51, Name: "claude.exe", Start: t0},

		{PID: 60, PPID: 1, Name: "claude.exe", Start: t0},
		{PID: 61, PPID: 60, Name: "claude.exe", Start: t0},
		{PID: 62, PPID: 61, Name: "claude.exe", Start: t0},

		{PID: 70, PPID: 1, Name: "node.exe", Start: t0},
		{PID: 71, PPID: 70, Name: "claude.exe", Start: t0},
	}
	for _, e := range []struct {
		pid     int
		session string
	}{{13, "orca"}, {22, "vscode"}, {31, "cursor"}, {42, "wt"}, {52, "console"}, {62, "desktop"}, {71, "unknown"}} {
		writeEntry(t, dir, e.pid, e.session, t0)
	}

	got := Detect(dir, procs)
	want := map[string]Live{
		"orca":    {PID: 13, Host: Orca, HostPID: 11},
		"vscode":  {PID: 22, Host: VSCode, HostPID: 21},
		"cursor":  {PID: 31, Host: Cursor, HostPID: 30},
		"wt":      {PID: 42, Host: WindowsTerminal, HostPID: 40},
		"console": {PID: 52, Host: Console, HostPID: 50},
		"desktop": {PID: 62, Host: ClaudeDesktop, HostPID: 60},
		"unknown": {PID: 71},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %+v, want %+v", id, got[id], w)
		}
	}
}

// On macOS ps names a process by its path, so an app is known by its bundle,
// helpers included, and procStart is ps's lstart in UTC. iTerm2 runs its
// shells under an iTermServer outside its bundle.
func TestDetectClassifiesTheHostOfEachLiveSessionOnMac(t *testing.T) {
	dir := t.TempDir()
	const vsc = "/Applications/Visual Studio Code.app/Contents/"
	procs := []Proc{
		{PID: 1, Name: "/sbin/launchd", Start: t0},
		{PID: 10, PPID: 1, Name: "/Applications/Orca.app/Contents/MacOS/Orca", Start: t0},
		{PID: 11, PPID: 10, Name: "/Applications/Orca.app/Contents/Frameworks/Orca Helper.app/Contents/MacOS/Orca Helper", Start: t0},
		{PID: 12, PPID: 11, Name: "/bin/zsh", Start: t0},
		{PID: 13, PPID: 12, Name: "claude", Start: t0},

		{PID: 20, PPID: 1, Name: vsc + "MacOS/Code", Start: t0},
		{PID: 21, PPID: 20, Name: vsc + "Frameworks/Code Helper (Plugin).app/Contents/MacOS/Code Helper (Plugin)", Start: t0},
		{PID: 22, PPID: 21, Name: "/bin/zsh", Start: t0},
		{PID: 23, PPID: 22, Name: "claude", Start: t0},

		{PID: 30, PPID: 1, Name: "/Applications/Cursor.app/Contents/MacOS/Cursor", Start: t0},
		{PID: 31, PPID: 30, Name: "claude", Start: t0},

		{PID: 40, PPID: 1, Name: "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal", Start: t0},
		{PID: 41, PPID: 40, Name: "login", Start: t0},
		{PID: 42, PPID: 41, Name: "-zsh", Start: t0},
		{PID: 43, PPID: 42, Name: "claude", Start: t0},

		{PID: 50, PPID: 1, Name: "/Users/me/Library/Application Support/iTerm2/iTermServer-3.5.4", Start: t0},
		{PID: 51, PPID: 50, Name: "login", Start: t0},
		{PID: 52, PPID: 51, Name: "-zsh", Start: t0},
		{PID: 53, PPID: 52, Name: "claude", Start: t0},

		{PID: 55, PPID: 1, Name: "/Applications/iTerm.app/Contents/MacOS/iTerm2", Start: t0},
		{PID: 56, PPID: 55, Name: "-zsh", Start: t0},
		{PID: 57, PPID: 56, Name: "claude", Start: t0},

		{PID: 60, PPID: 1, Name: "/Applications/Claude.app/Contents/MacOS/Claude", Start: t0},
		{PID: 61, PPID: 60, Name: "/Users/me/Library/Application Support/Claude/claude-code/2.1.312/claude", Start: t0},

		{PID: 70, PPID: 1, Name: "/usr/sbin/sshd", Start: t0},
		{PID: 71, PPID: 70, Name: "-zsh", Start: t0},
		{PID: 72, PPID: 71, Name: "claude", Start: t0},
	}
	for _, e := range []struct {
		pid     int
		session string
	}{{13, "orca"}, {23, "vscode"}, {31, "cursor"}, {43, "terminal"}, {53, "itermserver"}, {57, "iterm"}, {61, "desktop"}, {72, "unknown"}} {
		writeProcStart(t, dir, e.pid, e.session, "Thu Oct 29 09:00:00 2026")
	}

	got := Detect(dir, procs)
	want := map[string]Live{
		"orca":        {PID: 13, Host: Orca, HostPID: 11},
		"vscode":      {PID: 23, Host: VSCode, HostPID: 21},
		"cursor":      {PID: 31, Host: Cursor, HostPID: 30},
		"terminal":    {PID: 43, Host: Terminal, HostPID: 40},
		"itermserver": {PID: 53, Host: ITerm, HostPID: 50},
		"iterm":       {PID: 57, Host: ITerm, HostPID: 55},
		"desktop":     {PID: 61, Host: ClaudeDesktop, HostPID: 60},
		"unknown":     {PID: 72},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: got %+v, want %+v", id, got[id], w)
		}
	}
}

// Windows reuses pids: an entry whose pid now belongs to a process started at
// another time is a closed session, and so is one whose pid is gone.
func TestDetectRejectsReusedAndMissingPids(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, 13, "reused", t0)
	writeEntry(t, dir, 14, "gone", t0)
	writeEntry(t, dir, 15, "close", t0)
	procs := []Proc{
		{PID: 13, Name: "claude.exe", Start: t0.Add(time.Hour)},
		{PID: 15, Name: "claude.exe", Start: t0.Add(500 * time.Microsecond)},
	}
	got := Detect(dir, procs)
	if _, ok := got["reused"]; ok {
		t.Errorf("a reused pid is not live: %v", got)
	}
	if _, ok := got["gone"]; ok {
		t.Errorf("a missing pid is not live: %v", got)
	}
	if _, ok := got["close"]; !ok {
		t.Errorf("the start time is compared at a coarser grain than the registry's: %v", got)
	}
}

// A parent started after its child is a reused pid, not the real parent, so
// the walk stops there instead of naming the wrong app.
func TestDetectStopsAtAReusedParentPid(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, 13, "s", t0)
	procs := []Proc{
		{PID: 10, Name: "Code.exe", Start: t0.Add(time.Hour)},
		{PID: 13, PPID: 10, Name: "claude.exe", Start: t0},
	}
	if got := Detect(dir, procs)["s"]; got.Host != "" {
		t.Fatalf("got %+v", got)
	}
}

// The desktop app's window belongs to its main process: a claude.exe with
// claude.exe children and no claude.exe parent. A CLI claude.exe, even one the
// app started, is not it.
// The editor's windows belong to its main process, the one whose parent is not
// the editor itself (its helpers and extension host run under it).
func TestEditorPIDFindsTheMainProcess(t *testing.T) {
	procs := []Proc{
		{PID: 1, Name: "explorer.exe"},
		{PID: 33564, PPID: 1, Name: "Code.exe"},
		{PID: 43900, PPID: 33564, Name: "Code.exe"},
		{PID: 46328, PPID: 43900, Name: "claude.exe"},
	}
	if got := EditorPID(procs, VSCode); got != 33564 {
		t.Fatalf("got %d", got)
	}
	if got := EditorPID(procs, Cursor); got != 0 {
		t.Fatalf("cursor not running: got %d", got)
	}
}

func TestDesktopPIDFindsTheAppsMainProcess(t *testing.T) {
	procs := []Proc{
		{PID: 1, Name: "explorer.exe"},
		{PID: 5, PPID: 1, Name: "pwsh.exe"},
		{PID: 6, PPID: 5, Name: "claude.exe"},
		{PID: 60, PPID: 1, Name: "Claude.exe"},
		{PID: 61, PPID: 60, Name: "claude.exe"},
		{PID: 62, PPID: 61, Name: "claude.exe"},
	}
	if got := DesktopPID(procs); got != 60 {
		t.Fatalf("got %d", got)
	}
	if got := DesktopPID(procs[:3]); got != 0 {
		t.Fatalf("no app running: got %d", got)
	}
}
