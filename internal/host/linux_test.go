package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// linuxOpener is an opener for Linux whose PATH holds the programs given, and
// which records every terminal it starts and every command it runs.
func linuxOpener(t *testing.T, onPath ...string) (*Opener, *[]string) {
	o, calls := fakeOpener(t, nil, nil)
	o.OS = "linux"
	o.LookPath = func(f string) (string, error) {
		for _, p := range onPath {
			if p == f {
				return "/usr/bin/" + f, nil
			}
		}
		return "", errors.New("not found")
	}
	o.StartIn = func(dir, bin string, args []string) error {
		*calls = append(*calls, fmt.Sprintf("in %s: %q", dir, append([]string{bin}, args...)))
		return nil
	}
	return o, calls
}

// Each terminal gets its own flags for the folder and the command, each word a
// separate argument; a terminal not known gets xterm's -e.
func TestTerminalArgvPerTerminal(t *testing.T) {
	cmd := []string{"claude", "--resume", "s1"}
	dir := "/home/me/my app"
	for kind, want := range map[string][]string{
		"x-terminal-emulator": {"-e", "claude", "--resume", "s1"},
		"gnome-terminal":      {"--working-directory=/home/me/my app", "--", "claude", "--resume", "s1"},
		"konsole":             {"--workdir", dir, "-e", "claude", "--resume", "s1"},
		"xfce4-terminal":      {"--working-directory=/home/me/my app", "-x", "claude", "--resume", "s1"},
		"kitty":               {"--directory", dir, "claude", "--resume", "s1"},
		"alacritty":           {"--working-directory", dir, "-e", "claude", "--resume", "s1"},
		"wezterm":             {"start", "--cwd", dir, "--", "claude", "--resume", "s1"},
		"xterm":               {"-e", "claude", "--resume", "s1"},
		"foot":                {"--working-directory=/home/me/my app", "claude", "--resume", "s1"},
		"some-new-terminal":   {"-e", "claude", "--resume", "s1"},
	} {
		if got := terminalArgv(kind, dir, cmd); fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
			t.Errorf("%s: %q", kind, got)
		}
	}
}

// $TERMINAL wins when it is on PATH; else the first of the list found.
func TestPickTerminalHonorsTERMINALThenTheList(t *testing.T) {
	on := func(names ...string) func(string) (string, error) {
		return func(f string) (string, error) {
			for _, n := range names {
				if n == f {
					return "/opt/bin/" + f, nil
				}
			}
			return "", os.ErrNotExist
		}
	}
	for _, c := range []struct {
		env       string
		path      []string
		bin, kind string
	}{
		{"", []string{"xterm", "konsole", "x-terminal-emulator"}, "/opt/bin/x-terminal-emulator", "x-terminal-emulator"},
		{"", []string{"xterm", "konsole"}, "/opt/bin/konsole", "konsole"},
		{"", []string{"xterm", "alacritty", "kitty"}, "/opt/bin/kitty", "kitty"},
		{"", []string{"xterm"}, "/opt/bin/xterm", "xterm"},
		{"kitty", []string{"kitty", "gnome-terminal"}, "/opt/bin/kitty", "kitty"},
		{"/usr/local/alacritty", []string{"/usr/local/alacritty", "xterm"}, "/opt/bin//usr/local/alacritty", "alacritty"},
		{"missing-term", []string{"wezterm"}, "/opt/bin/wezterm", "wezterm"},
		{"", nil, "", ""},
	} {
		bin, kind := pickTerminal(c.env, on(c.path...))
		if bin != c.bin || kind != c.kind {
			t.Errorf("TERMINAL=%q on %v: %q %q", c.env, c.path, bin, kind)
		}
	}
}

// On Linux a closed session resumes in a new terminal window started in its
// folder, without a shell or cmd.exe in between.
func TestResumeOnLinuxOpensATerminalInTheFolder(t *testing.T) {
	o, calls := linuxOpener(t, "gnome-terminal", "xterm")
	if err := o.Resume(context.Background(), "/home/me/app", "s1"); err != nil {
		t.Fatal(err)
	}
	if err := o.ResumeCodex(context.Background(), "/home/me/app", "s1"); err != nil {
		t.Fatal(err)
	}
	o.TerminalEnv = "xterm"
	if err := o.Resume(context.Background(), "/home/me/app", "s1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`in /home/me/app: ["/usr/bin/gnome-terminal" "--working-directory=/home/me/app" "--" "claude" "--resume" "s1"]`,
		`in /home/me/app: ["/usr/bin/gnome-terminal" "--working-directory=/home/me/app" "--" "codex" "resume" "s1"]`,
		`in /home/me/app: ["/usr/bin/xterm" "-e" "claude" "--resume" "s1"]`,
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(*calls, "\n"))
	}
}

func TestResumeOnLinuxWithoutATerminalFails(t *testing.T) {
	o, calls := linuxOpener(t)
	if err := o.Resume(context.Background(), "/home/me/app", "s1"); err == nil || err.Error() != "could not resume: no terminal found" || len(*calls) != 0 {
		t.Fatalf("%v %q", err, *calls)
	}
	o, _ = linuxOpener(t, "xterm")
	o.StartIn = func(string, string, []string) error { return errors.New("exec: long output") }
	if err := o.Resume(context.Background(), "/home/me/app", "s1"); err == nil || err.Error() != "could not resume" {
		t.Fatalf("start failed: %v", err)
	}
}

// Links open through xdg-open on Linux, never rundll32, and no window is
// raised there.
func TestLinuxOpensLinksWithXdgOpenAndDoesNotFocus(t *testing.T) {
	o, calls := linuxOpener(t)
	if err := o.Codex(context.Background(), "s1", 70); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, "|") != "xdg-open codex://threads/s1" {
		t.Fatalf("calls: %q", *calls)
	}
	if o.CanFocus() {
		t.Fatal("Linux cannot focus")
	}
	*calls = nil
	if err := o.Terminal(context.Background(), LinuxTerminal, 42); err == nil || len(*calls) != 0 {
		t.Fatalf("terminal: %v %q", err, *calls)
	}
	for _, osName := range []string{"windows", "darwin"} {
		if o.OS = osName; !o.CanFocus() {
			t.Errorf("%s can focus", osName)
		}
	}
}
