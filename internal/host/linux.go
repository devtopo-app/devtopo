package host

import (
	"errors"
	"path/filepath"
	"strings"
)

// The Linux side of Opener. Raising another app's window is not supported
// there (no portable way across X11 and Wayland compositors), so DevTopo only
// resumes closed sessions, in a new terminal window.

// errFocus is the answer to bringing a window forward where that is not
// supported.
var errFocus = errors.New("focusing a window is not supported on this system")

// CanFocus reports whether the opener can bring a session's app to the front.
// It cannot on Linux; a closed session is still resumed.
func (o *Opener) CanFocus() bool { return o.OS != "linux" }

// linuxTerminalOrder is the order terminals are tried in when $TERMINAL is
// not set or not found: Debian's x-terminal-emulator alternative first, as
// the user's (or distribution's) choice.
var linuxTerminalOrder = []string{
	"x-terminal-emulator", "gnome-terminal", "konsole", "xfce4-terminal",
	"kitty", "alacritty", "wezterm", "xterm",
}

// pickTerminal is the terminal to resume in: $TERMINAL when set and found,
// else the first of linuxTerminalOrder on PATH. It returns the terminal's
// path and its kind, the name terminalArgv knows its flags by; "" when none.
func pickTerminal(env string, look func(string) (string, error)) (path, kind string) {
	if env = strings.TrimSpace(env); env != "" {
		if p, err := look(env); err == nil {
			return p, filepath.Base(env)
		}
	}
	for _, name := range linuxTerminalOrder {
		if p, err := look(name); err == nil {
			return p, name
		}
	}
	return "", ""
}

// terminalArgv is the arguments that make a terminal of kind open a new
// window in dir running command, each word its own argument: no shell parses
// them. A terminal not known here gets "-e command...", the convention
// xterm set and Debian's x-terminal-emulator requires; it starts in dir as
// the working directory it is started in.
func terminalArgv(kind, dir string, command []string) []string {
	var pre []string
	switch kind {
	case "gnome-terminal":
		pre = []string{"--working-directory=" + dir, "--"}
	case "konsole":
		pre = []string{"--workdir", dir, "-e"}
	case "xfce4-terminal":
		pre = []string{"--working-directory=" + dir, "-x"}
	case "kitty":
		pre = []string{"--directory", dir}
	case "alacritty":
		pre = []string{"--working-directory", dir, "-e"}
	case "wezterm":
		pre = []string{"start", "--cwd", dir, "--"}
	case "foot":
		pre = []string{"--working-directory=" + dir}
	default:
		pre = []string{"-e"}
	}
	return append(pre, command...)
}

// linuxResume runs command in cwd in a new window of the user's terminal. The
// folder is both the terminal's working directory and, for the terminals that
// take one, its working-directory flag.
func (o *Opener) linuxResume(cwd string, command ...string) error {
	if cwd == "" || o.StartIn == nil || o.LookPath == nil {
		return errors.New("could not resume")
	}
	bin, kind := pickTerminal(o.TerminalEnv, o.LookPath)
	if bin == "" {
		return errors.New("could not resume: no terminal found")
	}
	if o.StartIn(cwd, bin, terminalArgv(kind, cwd, command)) != nil {
		return errors.New("could not resume")
	}
	return nil
}
