package launch

import (
	"path/filepath"
	"strings"
	"testing"
)

// The autostart entry runs the app in the tray at login, without the browser.
func TestAutostartEntryRunsTheAppAtLogin(t *testing.T) {
	got := autostartEntry("/opt/devtopo/devtopo")
	for _, want := range []string{
		"[Desktop Entry]\n",
		"\nType=Application\n",
		"\nExec=/opt/devtopo/devtopo tray --no-open\n",
		"\nTerminal=false\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// Paths with spaces, quotes, backslashes, $ and % are quoted and escaped as
// the Desktop Entry spec asks, string escaping included.
func TestDesktopExecQuotesPerTheSpec(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"/usr/bin/devtopo", `/usr/bin/devtopo`},
		{"/home/a user/bin/devtopo", `"/home/a user/bin/devtopo"`},
		{`/home/u/say "hi"/devtopo`, `"/home/u/say \\"hi\\"/devtopo"`},
		{`/home/u/back\slash/devtopo`, `"/home/u/back\\\\slash/devtopo"`},
		{"/home/u/$HOME/devtopo", `"/home/u/\\$HOME/devtopo"`},
		{"/home/u/100%/devtopo", `/home/u/100%%/devtopo`},
		{"/home/u/it's/devtopo", `"/home/u/it's/devtopo"`},
		{"/home/u/tick`/devtopo", "\"/home/u/tick\\\\`/devtopo\""},
		{"", `""`},
	} {
		if got := desktopExec(c.in); got != c.want {
			t.Errorf("desktopExec(%q) = %s, want %s", c.in, got, c.want)
		}
	}
	if got := desktopExec("/a b/devtopo", "tray", "--no-open"); got != `"/a b/devtopo" tray --no-open` {
		t.Errorf("got %s", got)
	}
}

// XDG_CONFIG_HOME is used when absolute; otherwise ~/.config.
func TestAutostartDir(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	if got, want := autostartDir(xdg, home), filepath.Join(xdg, "autostart"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, x := range []string{"", "relative/config"} {
		if got, want := autostartDir(x, home), filepath.Join(home, ".config", "autostart"); got != want {
			t.Errorf("XDG_CONFIG_HOME=%q: got %q, want %q", x, got, want)
		}
	}
}
