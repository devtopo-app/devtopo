// Package host finds the app each running Claude Code session lives in (Orca,
// VS Code, a terminal, the Claude desktop app) and brings that app to the
// front. Claude Code keeps a registry of its running processes; the app is
// found by walking a session process's parents.
package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The hosts a session can run in. "" is an app DevTopo does not know.
const (
	Orca            = "orca"
	VSCode          = "vscode"
	Cursor          = "cursor"
	WindowsTerminal = "windows-terminal"
	Console         = "console"
	ClaudeDesktop   = "claude-desktop"
	// CodexApp is the Codex desktop app, known from a rollout's originator
	// rather than a running process.
	CodexApp = "codex-app"
	// the macOS terminals
	Terminal = "terminal"
	ITerm    = "iterm"
	// LinuxTerminal is any of the Linux terminal emulators in linuxTerminals.
	LinuxTerminal = "linux-terminal"
)

// linuxTerminals are the Linux terminal emulators' executable names, as the
// /proc snapshot names a process. GNOME Terminal's shells run under its
// server, and Ptyxis's under its agent.
var linuxTerminals = map[string]bool{
	"gnome-terminal-server": true, "gnome-terminal": true, "ptyxis": true, "ptyxis-agent": true,
	"konsole": true, "kitty": true, "alacritty": true, "wezterm-gui": true, "wezterm": true,
	"xterm": true, "uxterm": true, "xfce4-terminal": true, "mate-terminal": true, "lxterminal": true,
	"qterminal": true, "tilix": true, "terminator": true, "foot": true, "ghostty": true,
	"terminology": true, "urxvt": true, "rxvt": true, "kgx": true,
}

// Proc is one process of a snapshot of the machine.
type Proc struct {
	PID, PPID int
	Name      string
	Start     time.Time
}

// Live is a running session: its claude process, the app it runs in, and the
// process that owns that app's window.
type Live struct {
	PID     int
	Host    string
	HostPID int
}

// SessionsDir is where Claude Code registers its running processes.
func SessionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "sessions")
}

// Detect returns the live sessions by session id. A registry entry counts only
// when its pid is running and started when the entry says, since Windows
// reuses pids. Only the *.json entries are read.
func Detect(sessionsDir string, procs []Proc) map[string]Live {
	byPID := make(map[int]Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	files, _ := filepath.Glob(filepath.Join(sessionsDir, "*.json"))
	out := map[string]Live{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			ProcStart string `json:"procStart"`
		}
		if json.Unmarshal(b, &e) != nil || e.SessionID == "" {
			continue
		}
		p, ok := byPID[e.PID]
		start, err := procStart(e.ProcStart)
		if !ok || err != nil {
			continue
		}
		// the snapshot may be coarser than procStart
		if d := p.Start.Sub(start); d > time.Second || d < -time.Second {
			continue
		}
		l := Live{PID: p.PID}
		l.Host, l.HostPID = classify(p, byPID)
		out[e.SessionID] = l
	}
	return out
}

// procStart reads a registry entry's start time: a FILETIME (100ns since 1601)
// on Windows, ps's lstart in UTC on macOS and Linux.
func procStart(s string) (time.Time, error) {
	ft, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return lstart(s)
	}
	return time.Unix(0, (ft-116444736000000000)*100), nil
}

// EditorPID is the editor's main process, which owns its windows: the editor's
// exe whose parent is not the editor (helpers and the extension host run under
// it). 0 when not running.
func EditorPID(procs []Proc, editor string) int {
	exe := map[string]string{VSCode: "Code.exe", Cursor: "Cursor.exe"}[editor]
	name := map[int]string{}
	for _, p := range procs {
		name[p.PID] = p.Name
	}
	for _, p := range procs {
		if exe != "" && strings.EqualFold(p.Name, exe) && !strings.EqualFold(name[p.PPID], exe) {
			return p.PID
		}
	}
	return 0
}

// DesktopPID is the Claude desktop app's main process, which owns its window:
// a claude.exe with claude.exe children and no claude.exe parent. 0 when the
// app is not running.
func DesktopPID(procs []Proc) int {
	claude, parent := map[int]bool{}, map[int]int{}
	for _, p := range procs {
		claude[p.PID], parent[p.PID] = strings.EqualFold(p.Name, "claude.exe"), p.PPID
	}
	for _, p := range procs {
		if claude[p.PID] && claude[p.PPID] && !claude[parent[p.PPID]] {
			return p.PPID
		}
	}
	return 0
}

// CodexPID is the Codex desktop app's main process, which owns its window: the
// ChatGPT.exe that runs a codex.exe. 0 when the app is not running.
func CodexPID(procs []Proc) int {
	name := map[int]string{}
	for _, p := range procs {
		name[p.PID] = p.Name
	}
	for _, p := range procs {
		if strings.EqualFold(p.Name, "codex.exe") && strings.EqualFold(name[p.PPID], "ChatGPT.exe") {
			return p.PPID
		}
	}
	return 0
}

// classify walks p's parents to the first app it knows. A shell with no known
// app above it is a console window of its own, and a claude.exe above the
// session is the desktop app, whose window belongs to its topmost claude.exe.
func classify(p Proc, byPID map[int]Proc) (string, int) {
	host, hostPID, shell := "", 0, 0
	for i := 0; i < 32; i++ {
		parent, ok := byPID[p.PPID]
		// a parent started after its child is a reused pid, not the parent
		if !ok || p.PPID == p.PID || parent.Start.After(p.Start) {
			break
		}
		p = parent
		// on macOS ps names a process by its path, inside its app's bundle
		n := strings.ToLower(p.Name)
		in := func(bundle string) bool { return strings.Contains(n, "/"+bundle+".app/") }
		switch {
		case n == "orca.exe" || in("orca"):
			return Orca, p.PID
		// VS Code and its open builds run as code, code-insiders, code-oss or
		// codium on Linux
		case n == "code.exe" || in("visual studio code") || n == "code" || n == "code-insiders" || n == "code-oss" || n == "codium":
			return VSCode, p.PID
		case n == "cursor.exe" || in("cursor") || n == "cursor":
			return Cursor, p.PID
		case linuxTerminals[n]:
			return LinuxTerminal, p.PID
		case n == "windowsterminal.exe":
			return WindowsTerminal, p.PID
		case in("terminal"):
			return Terminal, p.PID
		case in("iterm") || strings.Contains(n, "/itermserver"):
			return ITerm, p.PID
		case n == "claude.exe" || in("claude"):
			host, hostPID = ClaudeDesktop, p.PID
		case n == "pwsh.exe" || n == "powershell.exe" || n == "cmd.exe":
			shell = p.PID
		}
	}
	if host == "" && shell != 0 {
		return Console, shell
	}
	return host, hostPID
}
