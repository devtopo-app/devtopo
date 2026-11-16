package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// macOpener records every command as its argv, and answers ps with tty.
func macOpener(t *testing.T, tty string) (*Opener, *[][]string) {
	var argv [][]string
	o := &Opener{
		OS: "darwin", Code: "code", Cursor: "cursor", DesktopDir: t.TempDir(),
		Run: func(_ context.Context, bin string, args []string) error {
			argv = append(argv, append([]string{bin}, args...))
			return nil
		},
		RunIn: func(_ context.Context, dir, bin string, args []string) error {
			argv = append(argv, append([]string{"in " + dir, bin}, args...))
			return nil
		},
		Output: func(_ context.Context, bin string, args []string) ([]byte, error) {
			argv = append(argv, append([]string{bin}, args...))
			return []byte(tty), nil
		},
	}
	return o, &argv
}

func sameArgv(t *testing.T, got, want [][]string) {
	t.Helper()
	if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
		t.Fatalf("argv:\n%q\nwant:\n%q", got, want)
	}
}

// osascript runs a script given line by line, with args as the run handler's
// argv, so no value is ever spliced into AppleScript.
func osascript(script string, args ...string) []string {
	argv := []string{"osascript"}
	for _, l := range strings.Split(script, "\n") {
		argv = append(argv, "-e", l)
	}
	return append(argv, args...)
}

func activate(app string) []string {
	return []string{"osascript", "-e", `tell application "` + app + `" to activate`}
}

// On macOS the editor is brought forward, its CLI focuses the window with the
// folder open, and open hands the link to the editor.
func TestMacEditorActivatesTheAppFocusesTheFolderThenOpensTheSession(t *testing.T) {
	o, argv := macOpener(t, "")
	if err := o.Editor(context.Background(), VSCode, "/Users/me/my app", "s1", 0); err != nil {
		t.Fatal(err)
	}
	if err := o.Editor(context.Background(), Cursor, "/Users/me/my app", "s1", 0); err != nil {
		t.Fatal(err)
	}
	sameArgv(t, *argv, [][]string{
		activate("Visual Studio Code"),
		{"in /Users/me/my app", "code", "."},
		{"open", "vscode://anthropic.claude-code/open?session=s1"},
		activate("Cursor"),
		{"in /Users/me/my app", "cursor", "."},
		{"open", "cursor://anthropic.claude-code/open?session=s1"},
	})
}

// The link opens the session in the Claude app, which is then brought forward,
// running or not, as macOS raises an app by name.
func TestMacDesktopOpensTheSessionThenActivatesTheApp(t *testing.T) {
	o, argv := macOpener(t, "")
	dir := filepath.Join(o.DesktopDir, "acct", "org")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "local_x.json"), []byte(`{"sessionId":"local_x","cliSessionId":"s1"}`), 0o600)
	if err := o.Desktop(context.Background(), "s1", 0); err != nil {
		t.Fatal(err)
	}
	sameArgv(t, *argv, [][]string{
		{"open", "claude://code/continue?session=local_x"},
		activate("Claude"),
	})
}

const wantTerminalScript = `on run argv
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

const wantItermScript = `on run argv
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

// The session's tab is the one whose tty is its claude process's: Terminal
// selects it and raises its window, iTerm2 selects its window, tab and pane.
func TestMacTerminalSelectsTheTabShowingTheSessionsTty(t *testing.T) {
	o, argv := macOpener(t, "ttys003\n")
	if err := o.Terminal(context.Background(), Terminal, 43); err != nil {
		t.Fatal(err)
	}
	if err := o.Terminal(context.Background(), ITerm, 53); err != nil {
		t.Fatal(err)
	}
	sameArgv(t, *argv, [][]string{
		{"ps", "-o", "tty=", "-p", "43"},
		osascript(wantTerminalScript, "/dev/ttys003"),
		{"ps", "-o", "tty=", "-p", "53"},
		osascript(wantItermScript, "/dev/ttys003"),
	})
}

// No tab with the tty (or no tty at all, ps's "??") is a window not found.
func TestMacTerminalWithoutTheTabIsNotFound(t *testing.T) {
	o, argv := macOpener(t, "??\n")
	if err := o.Terminal(context.Background(), Terminal, 43); err == nil || err.Error() != "window not found" || len(*argv) != 1 {
		t.Fatalf("no tty: %v %q", err, *argv)
	}
	o, _ = macOpener(t, "ttys003\n")
	o.Run = func(context.Context, string, []string) error { return errors.New("exit status 1") }
	if err := o.Terminal(context.Background(), ITerm, 53); err == nil || err.Error() != "window not found" {
		t.Fatalf("no tab: %v", err)
	}
}

const wantResumeScript = `on run argv
tell application "Terminal"
do script item 1 of argv
activate
end tell
end run`

// A closed session resumes in a new Terminal window, cd'ing to its folder;
// the folder is shell quoted, whatever it holds.
func TestMacResumeRunsClaudeInANewTerminalWindow(t *testing.T) {
	o, argv := macOpener(t, "")
	if err := o.Resume(context.Background(), "/Users/me/it's my app", "s1"); err != nil {
		t.Fatal(err)
	}
	sameArgv(t, *argv, [][]string{
		osascript(wantResumeScript, `cd '/Users/me/it'\''s my app' && claude --resume 's1'`),
	})
}

func TestMacResumeCodexRunsCodexResumeInANewTerminalWindow(t *testing.T) {
	o, argv := macOpener(t, "")
	if err := o.ResumeCodex(context.Background(), "/Users/me/app", "c1"); err != nil {
		t.Fatal(err)
	}
	sameArgv(t, *argv, [][]string{
		osascript(wantResumeScript, `cd '/Users/me/app' && codex resume 'c1'`),
	})
}

// The quoting holds in a real shell: the folder comes back as it went in.
func TestShellQuoteRoundTrips(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for _, s := range []string{"/Users/me/it's my app", `a "b" $HOME \ ; ' '' x`} {
		out, err := exec.Command(sh, "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil || string(out) != s {
			t.Errorf("%q came back as %q (%v)", s, out, err)
		}
	}
}
