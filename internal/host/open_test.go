package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/devtopo-app/devtopo/internal/window"
)

// fakeOpener records every command and raise instead of touching the machine.
// windows maps a pid to its window, and consoles a pid to its console's window.
func fakeOpener(t *testing.T, windows, consoles map[int]uintptr) (*Opener, *[]string) {
	var calls []string
	lookup := func(m map[int]uintptr) func(int) (uintptr, error) {
		return func(pid int) (uintptr, error) {
			if h, ok := m[pid]; ok {
				return h, nil
			}
			return 0, window.ErrNotFound
		}
	}
	o := &Opener{
		OS:   "windows",
		Code: "code", Cursor: "cursor", WT: "wt", DesktopDir: t.TempDir(),
		Run: func(_ context.Context, bin string, args []string) error {
			calls = append(calls, bin+" "+strings.Join(args, " "))
			return nil
		},
		RunIn: func(_ context.Context, dir, bin string, args []string) error {
			calls = append(calls, "in "+dir+": "+bin+" "+strings.Join(args, " "))
			return nil
		},
		FindWindow:    lookup(windows),
		ConsoleWindow: lookup(consoles),
		RaiseWindow: func(h uintptr) error {
			calls = append(calls, "raise "+strconv.Itoa(int(h)))
			return nil
		},
	}
	return o, &calls
}

// A background daemon cannot hand focus to VS Code through its CLI, so the
// window showing the session's folder is raised directly, then the link opens
// the session in it (the link goes to the focused window).
func TestEditorRaisesTheWindowShowingTheFolderThenOpensTheSession(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("raising a window by its title is the Windows path")
	}
	o, calls := fakeOpener(t, nil, nil)
	var asked string
	o.FindTitled = func(pid int, part string) (uintptr, error) {
		asked = strconv.Itoa(pid) + " " + part
		return 1115226, nil
	}
	if err := o.Editor(context.Background(), VSCode, `C:\Users\me\My App`, "s1", 33564); err != nil {
		t.Fatal(err)
	}
	if asked != "33564 My App - Visual Studio Code" {
		t.Fatalf("looked for the window by %q", asked)
	}
	want := "raise 1115226|rundll32 url.dll,FileProtocolHandler vscode://anthropic.claude-code/open?session=s1"
	if strings.Join(*calls, "|") != want {
		t.Fatalf("calls: %s", strings.Join(*calls, "|"))
	}
}

// Without the editor's window (not running, or no window shows the folder),
// the CLI opens the folder and the link follows.
func TestEditorFocusesTheFolderThenOpensTheSession(t *testing.T) {
	o, calls := fakeOpener(t, nil, nil)
	if err := o.Editor(context.Background(), VSCode, `C:\w\app`, "s1", 0); err != nil {
		t.Fatal(err)
	}
	if err := o.Editor(context.Background(), Cursor, `C:\w\app`, "s1", 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`in C:\w\app: code .`,
		`rundll32 url.dll,FileProtocolHandler vscode://anthropic.claude-code/open?session=s1`,
		`in C:\w\app: cursor .`,
		`rundll32 url.dll,FileProtocolHandler cursor://anthropic.claude-code/open?session=s1`,
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(*calls, "\n"))
	}
	o.Code = ""
	if err := o.Editor(context.Background(), VSCode, `C:\w\app`, "s1", 0); err == nil || err.Error() != "VS Code not available" {
		t.Fatalf("no cli: %v", err)
	}
}

// A failed command is reported by app, without the command's own output.
func TestEditorFailureIsShort(t *testing.T) {
	o, _ := fakeOpener(t, nil, nil)
	o.RunIn = func(context.Context, string, string, []string) error { return errors.New("exit status 1: long output") }
	if err := o.Editor(context.Background(), VSCode, `C:\w`, "s1", 0); err == nil || err.Error() != "VS Code did not open" {
		t.Fatalf("err: %v", err)
	}
}

// Windows Terminal runs all its windows in one process, so the window is found
// from the session's own console, not from the terminal's pid.
func TestTerminalRaisesTheWindowShowingTheSessionsConsole(t *testing.T) {
	o, calls := fakeOpener(t, map[int]uintptr{40: 111}, map[int]uintptr{42: 203662})
	if err := o.Terminal(context.Background(), WindowsTerminal, 42); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, "|") != "raise 203662" {
		t.Fatalf("calls: %q", *calls)
	}
	*calls = nil
	if err := o.Terminal(context.Background(), WindowsTerminal, 43); err == nil || err.Error() != "window not found" || len(*calls) != 0 {
		t.Fatalf("no console: %v %q", err, *calls)
	}
}

func TestDesktopOpensTheAppsOwnSessionIdThenRaisesIt(t *testing.T) {
	o, calls := fakeOpener(t, map[int]uintptr{6: 66}, nil)
	dir := filepath.Join(o.DesktopDir, "acct", "org")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "local_x.json"), []byte(`{"sessionId":"local_x","cliSessionId":"s1"}`), 0o600)
	os.WriteFile(filepath.Join(dir, "local_y.json"), []byte(`{"sessionId":"local_y","cliSessionId":"s2"}`), 0o600)

	if err := o.Desktop(context.Background(), "s1", 6); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, "|") != "rundll32 url.dll,FileProtocolHandler claude://code/continue?session=local_x|raise 66" {
		t.Fatalf("calls: %q", *calls)
	}
	// with the app not running the link alone starts it
	*calls = nil
	if err := o.Desktop(context.Background(), "s1", 0); err != nil || strings.Join(*calls, "|") != "rundll32 url.dll,FileProtocolHandler claude://code/continue?session=local_x" {
		t.Fatalf("app not running: %v %q", err, *calls)
	}
	if got := o.DesktopSessions(); len(got) != 2 || got["s2"] != "local_y" {
		t.Fatalf("desktop sessions: %v", got)
	}
	*calls = nil
	if err := o.Desktop(context.Background(), "s9", 6); err == nil || err.Error() != "not found in the Claude app" || len(*calls) != 0 {
		t.Fatalf("unknown session: %v %q", err, *calls)
	}
}

// claude is often an npm .cmd shim, which wt and start cannot run by bare
// name, so it runs through cmd. The folder is the working directory, never an
// argument cmd or wt would parse.
func TestResumeOpensAWindowsTerminalTabElseAConsole(t *testing.T) {
	o, _ := fakeOpener(t, nil, nil)
	var argv [][]string
	o.RunIn = func(_ context.Context, dir, bin string, args []string) error {
		argv = append(argv, append([]string{dir, bin}, args...))
		return nil
	}
	if err := o.Resume(context.Background(), `C:\w\my app`, "s1"); err != nil {
		t.Fatal(err)
	}
	o.WT = ""
	if err := o.Resume(context.Background(), `C:\w\my app`, "s1"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{`C:\w\my app`, "wt", "-d", ".", "cmd", "/k", "claude", "--resume", "s1"},
		{`C:\w\my app`, "cmd", "/c", "start", "", "cmd", "/k", "claude", "--resume", "s1"}, // "" is start's window title
	}
	if fmt.Sprintf("%q", argv) != fmt.Sprintf("%q", want) {
		t.Fatalf("argv:\n%q", argv)
	}
}

// cmd.exe treats & | ^ < > % ( ) specially and wt splits its line at ';', so a
// folder holding them must never reach either as an argument.
func TestResumeKeepsShellCharactersInTheFolderOutOfTheCommandLine(t *testing.T) {
	for _, cwd := range []string{`C:\R&D\app`, `C:\w\a;b`, `C:\w\100%^ (x) | y`} {
		for _, wt := range []string{"wt", ""} {
			o, _ := fakeOpener(t, nil, nil)
			o.WT = wt
			var gotDir string
			var gotArgs []string
			o.RunIn = func(_ context.Context, dir, bin string, args []string) error {
				gotDir, gotArgs = dir, append([]string{bin}, args...)
				return nil
			}
			if err := o.Resume(context.Background(), cwd, "0b1c2d3e-aaaa-4bbb-8ccc-1234567890ab"); err != nil {
				t.Fatal(err)
			}
			if gotDir != cwd {
				t.Errorf("%q via %q: dir %q", cwd, wt, gotDir)
			}
			for _, a := range gotArgs {
				if strings.ContainsAny(a, `&;%^()|<>\`) {
					t.Errorf("%q via %q: argument %q carries a shell character", cwd, wt, a)
				}
			}
		}
	}
}

// The session id comes from transcripts on disk, so anything but a plain id is
// refused before a command runs.
func TestResumeRefusesAnUnsafeSessionID(t *testing.T) {
	for _, id := range []string{"", "s1&calc", "s1 & calc", `s1"`, "s1;calc", "s1|x", "%PATH%", "s1\nx", strings.Repeat("a", 129)} {
		for _, osName := range []string{"windows", "darwin", "linux"} {
			o, calls := fakeOpener(t, nil, nil)
			o.OS = osName
			o.Output = func(context.Context, string, []string) ([]byte, error) { return nil, nil }
			o.LookPath = func(f string) (string, error) { return "/usr/bin/" + f, nil }
			o.StartIn = func(dir, bin string, args []string) error {
				*calls = append(*calls, bin)
				return nil
			}
			if err := o.Resume(context.Background(), `C:\w\app`, id); err == nil || len(*calls) != 0 {
				t.Errorf("%s %q: %v %q", osName, id, err, *calls)
			}
			if err := o.ResumeCodex(context.Background(), `C:\w\app`, id); err == nil || len(*calls) != 0 {
				t.Errorf("codex %s %q: %v %q", osName, id, err, *calls)
			}
		}
	}
}

// A folder name with cmd.exe's metacharacters never reaches a command line:
// the editor runs in the folder and opens ".".
func TestEditorNeverPassesTheFolderAsAnArgument(t *testing.T) {
	o, calls := fakeOpener(t, nil, nil)
	if err := o.Editor(context.Background(), VSCode, `C:\src\x&calc`, "s1", 0); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0] != `in C:\src\x&calc: code .` {
		t.Fatalf("calls: %v", *calls)
	}
	if err := o.Editor(context.Background(), VSCode, `C:\w`, "s1&calc", 0); err == nil {
		t.Fatal("a session id that is not a plain id must be refused")
	}
}
