package host

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/devtopo-app/devtopo/internal/window"
)

// Opener brings a session's app to the front, or resumes a closed session.
// Run and the window functions are injectable so tests never touch a real app
// or window. Every error is short and never carries a command's output.
type Opener struct {
	Run func(ctx context.Context, bin string, args []string) error
	// RunIn is Run with the command's working directory set to dir.
	RunIn func(ctx context.Context, dir, bin string, args []string) error
	// FindWindow finds a process's window, ConsoleWindow the window showing a
	// process's console, and RaiseWindow restores one and brings it forward.
	FindWindow    func(pid int) (uintptr, error)
	ConsoleWindow func(pid int) (uintptr, error)
	RaiseWindow   func(h uintptr) error
	// FindTitled finds pid's visible top-level window whose title holds part.
	FindTitled func(pid int, part string) (uintptr, error)
	// StartIn starts a command in dir and returns without waiting for it: a
	// Linux terminal window runs in the foreground until it is closed.
	StartIn func(dir, bin string, args []string) error
	// Code, Cursor and WT are the CLIs, "" when not installed.
	Code, Cursor, WT string
	// LookPath finds a program on PATH, and TerminalEnv is $TERMINAL: both
	// choose the terminal a session resumes in on Linux.
	LookPath    func(file string) (string, error)
	TerminalEnv string
	// DesktopDir holds the Claude desktop app's Code sessions.
	DesktopDir string
	// OS is runtime.GOOS: "windows", "darwin" or "linux" picks the commands.
	// Output runs a command for its stdout.
	OS     string
	Output func(ctx context.Context, bin string, args []string) ([]byte, error)
}

// NewOpener builds the production opener from the CLIs on PATH, else, on
// macOS, the CLI inside the app.
func NewOpener() *Opener {
	look := func(name, bundled string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		if _, err := os.Stat(bundled); bundled != "" && err == nil {
			return bundled
		}
		return ""
	}
	config, _ := os.UserConfigDir()
	return &Opener{
		Run:           execRun,
		RunIn:         execRunIn,
		StartIn:       execStartIn,
		LookPath:      exec.LookPath,
		TerminalEnv:   os.Getenv("TERMINAL"),
		Output:        execOutput,
		FindWindow:    window.FindPID,
		ConsoleWindow: window.ConsoleWindowOf,
		RaiseWindow:   window.Raise,
		FindTitled:    window.FindTitled,
		Code:          look("code", "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"),
		Cursor:        look("cursor", "/Applications/Cursor.app/Contents/Resources/app/bin/cursor"),
		WT:            look("wt", ""),
		// %APPDATA% on Windows, ~/Library/Application Support on macOS,
		// ~/.config on Linux
		DesktopDir: filepath.Join(config, "Claude", "claude-code-sessions"),
		OS:         runtime.GOOS,
	}
}

// Editor focuses the VS Code or Cursor window that has cwd open, then opens the
// session in it through the Claude Code extension's URI.
func (o *Opener) Editor(ctx context.Context, host, cwd, sessionID string, editorPID int) error {
	bin, app, scheme, titleApp := o.Code, "VS Code", "vscode", "Visual Studio Code"
	if host == Cursor {
		bin, app, scheme, titleApp = o.Cursor, "Cursor", "cursor", "Cursor"
	}
	if !validSessionID(sessionID) {
		return errors.New(app + " did not open: invalid session id")
	}
	link := scheme + "://anthropic.claude-code/open?session=" + sessionID
	// The editor's CLI cannot take focus from a background daemon, so the
	// window titled "... - <folder> - <app>" is raised directly when found;
	// the link then opens the session in that, now focused, window.
	if editorPID != 0 && o.FindTitled != nil {
		if h, err := o.FindTitled(editorPID, filepath.Base(cwd)+" - "+titleApp); err == nil {
			o.RaiseWindow(h)
			if o.openURL(ctx, link) != nil {
				return errors.New(app + " did not open")
			}
			return nil
		}
	}
	if bin == "" {
		return errors.New(app + " not available")
	}
	// on macOS the app comes forward by name, and its CLI then focuses the
	// window with the folder open
	if o.OS == "darwin" {
		o.activate(ctx, titleApp)
	}
	// The folder is the editor's working directory, opened as ".", never an
	// argument: on Windows the CLI is a .cmd script, so its arguments pass
	// through cmd.exe, which Go's quoting does not guard (& | ^ < > %).
	if o.RunIn == nil || o.RunIn(ctx, cwd, bin, []string{"."}) != nil || o.openURL(ctx, link) != nil {
		return errors.New(app + " did not open")
	}
	return nil
}

// Desktop opens the session in the Claude desktop app, by the app's own id for
// it, then raises the app's window at pid; with pid 0 (the app not running)
// the link alone starts it. On macOS the app is raised by name, running or not.
// A failed raise is not an error: the link went out.
func (o *Opener) Desktop(ctx context.Context, sessionID string, pid int) error {
	id, ok := o.DesktopSessions()[sessionID]
	if !ok {
		return errors.New("not found in the Claude app")
	}
	if o.openURL(ctx, "claude://code/continue?session="+id) != nil {
		return errors.New("the Claude app did not open")
	}
	if o.OS == "darwin" {
		o.activate(ctx, "Claude")
	} else if o.OS == "windows" && pid != 0 {
		if h, err := o.FindWindow(pid); err == nil {
			o.RaiseWindow(h)
		}
	}
	return nil
}

// Codex opens the session's thread in the Codex app, then raises the app's
// window at pid; with pid 0 (the app not running) the link alone starts it.
func (o *Opener) Codex(ctx context.Context, sessionID string, pid int) error {
	if o.openURL(ctx, "codex://threads/"+sessionID) != nil {
		return errors.New("the Codex app did not open")
	}
	if o.OS == "windows" && pid != 0 {
		if h, err := o.FindWindow(pid); err == nil {
			o.RaiseWindow(h)
		}
	}
	return nil
}

// DesktopSessions maps the Claude Code session ids the desktop app owns to the
// app's own ids for them.
func (o *Opener) DesktopSessions() map[string]string {
	files, _ := filepath.Glob(filepath.Join(o.DesktopDir, "*", "*", "local_*.json"))
	out := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			SessionID    string `json:"sessionId"`
			CLISessionID string `json:"cliSessionId"`
		}
		if json.Unmarshal(b, &s) == nil && s.CLISessionID != "" && s.SessionID != "" {
			out[s.CLISessionID] = s.SessionID
		}
	}
	return out
}

// Terminal raises the window showing the console of the session's claude
// process at pid. Windows Terminal runs all its windows in one process, so the
// terminal's own pid cannot tell which window holds the session. On macOS host
// (Terminal or iTerm2) selects the tab instead.
func (o *Opener) Terminal(ctx context.Context, host string, pid int) error {
	switch o.OS {
	case "darwin":
		return o.macTerminal(ctx, host, pid)
	case "windows":
	default:
		return errFocus
	}
	h, err := o.ConsoleWindow(pid)
	if err != nil {
		return window.ErrNotFound
	}
	return o.RaiseWindow(h)
}

// Resume reopens a closed session in its folder, in a new Windows Terminal
// window, else a console window. claude runs through cmd, since it is often an
// npm .cmd shim that wt and start cannot run by bare name. On macOS it opens a
// new Terminal window, and on Linux a new window of the user's terminal.
func (o *Opener) Resume(ctx context.Context, cwd, sessionID string) error {
	return o.resumeIn(ctx, cwd, "claude", "--resume", sessionID)
}

// ResumeCodex reopens a closed Codex CLI session in its folder, like Resume.
func (o *Opener) ResumeCodex(ctx context.Context, cwd, sessionID string) error {
	return o.resumeIn(ctx, cwd, "codex", "resume", sessionID)
}

// resumeIn runs command (its last word the session id) in cwd, in a new
// terminal window.
//
// On Windows the command line passes through cmd.exe (and wt's own parser,
// which splits on ';'), and Go's argument quoting does not escape cmd's
// metacharacters (& | ^ < > % and parentheses). So nothing from outside goes
// into that line: the session id must be a plain id (validSessionID), and the
// folder is never an argument; it is the launching process's working
// directory, which start passes to the new console and wt picks up through
// "-d .".
func (o *Opener) resumeIn(ctx context.Context, cwd string, command ...string) error {
	if !validSessionID(command[len(command)-1]) {
		return errors.New("could not resume: invalid session id")
	}
	switch o.OS {
	case "darwin":
		return o.macResume(ctx, cwd, command...)
	case "linux":
		return o.linuxResume(cwd, command...)
	case "windows":
	default:
		return errors.New("could not resume: not supported on " + o.OS)
	}
	if cwd == "" || o.RunIn == nil {
		return errors.New("could not resume")
	}
	command = append([]string{"cmd", "/k"}, command...)
	bin, args := o.WT, append([]string{"-d", "."}, command...)
	if bin == "" {
		bin, args = "cmd", append([]string{"/c", "start", ""}, command...)
	}
	if o.RunIn(ctx, cwd, bin, args) != nil {
		return errors.New("could not resume")
	}
	return nil
}

// validSessionID reports whether id is safe to put on a command line: 1 to 128
// ASCII letters, digits, '-' or '_' (session ids are UUIDs).
func validSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (o *Opener) openURL(ctx context.Context, url string) error {
	switch o.OS {
	case "darwin":
		return o.Run(ctx, "open", []string{url})
	case "linux":
		return o.Run(ctx, "xdg-open", []string{url})
	case "windows":
		return o.Run(ctx, "rundll32", []string{"url.dll,FileProtocolHandler", url})
	}
	return errors.New("cannot open links on " + o.OS)
}

// execRun runs a command with a timeout, dropping its output.
func execRun(ctx context.Context, bin string, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, args...).Run()
}

// execRunIn runs a command in dir with a timeout, dropping its output.
func execRunIn(ctx context.Context, dir, bin string, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	return cmd.Run()
}

// execStartIn starts a command in dir and reaps it in the background, so a
// terminal window outlives the request that opened it.
func execStartIn(dir, bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// execOutput runs a command with a timeout and returns its stdout.
func execOutput(ctx context.Context, bin string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, args...).Output()
}
