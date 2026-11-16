package host

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/devtopo-app/devtopo/internal/window"
)

// The macOS side of Opener. Apps are raised by name through AppleScript, and
// a value a script needs is passed as its argv, never spliced into its text.

// terminalScript selects Terminal's tab whose tty is argv's and raises its
// window; with no such tab it fails.
const terminalScript = `on run argv
tell application "Terminal"
repeat with w in windows
repeat with t in tabs of w
if tty of t is item 1 of argv then
set selected tab of w to t
set frontmost of w to true
activate
return
end if
end repeat
end repeat
end tell
error "not found"
end run`

// itermScript selects iTerm2's window, tab and pane whose tty is argv's.
const itermScript = `on run argv
tell application "iTerm2"
repeat with w in windows
repeat with t in tabs of w
repeat with s in sessions of t
if tty of s is item 1 of argv then
select w
select t
select s
activate
return
end if
end repeat
end repeat
end repeat
end tell
error "not found"
end run`

// resumeScript runs argv's command line in a new Terminal window.
const resumeScript = `on run argv
tell application "Terminal"
do script item 1 of argv
activate
end tell
end run`

// osascript runs script, one -e per line, with args as its argv.
func (o *Opener) osascript(ctx context.Context, script string, args ...string) error {
	var argv []string
	for _, l := range strings.Split(script, "\n") {
		argv = append(argv, "-e", l)
	}
	return o.Run(ctx, "osascript", append(argv, args...))
}

// activate brings the app named app to the front, starting it if need be.
func (o *Opener) activate(ctx context.Context, app string) error {
	return o.Run(ctx, "osascript", []string{"-e", `tell application "` + app + `" to activate`})
}

// macTerminal selects the Terminal or iTerm2 tab showing the console of the
// claude process at pid, found by its tty.
func (o *Opener) macTerminal(ctx context.Context, host string, pid int) error {
	out, err := o.Output(ctx, "ps", []string{"-o", "tty=", "-p", strconv.Itoa(pid)})
	tty := strings.TrimSpace(string(out))
	if err != nil || tty == "" || tty == "??" {
		return window.ErrNotFound
	}
	script := terminalScript
	if host == ITerm {
		script = itermScript
	}
	if o.osascript(ctx, script, "/dev/"+tty) != nil {
		return window.ErrNotFound
	}
	return nil
}

// macResume runs command (claude --resume, or codex resume) in a new Terminal
// window, in cwd.
func (o *Opener) macResume(ctx context.Context, cwd string, command ...string) error {
	// The command words are fixed; only the last, the session id, comes from
	// outside, so it is quoted like the folder.
	last := len(command) - 1
	line := "cd " + shellQuote(cwd) + " && " + strings.Join(command[:last], " ") + " " + shellQuote(command[last])
	if o.osascript(ctx, resumeScript, line) != nil {
		return errors.New("could not resume")
	}
	return nil
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
