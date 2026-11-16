package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Turning start at login on writes the autostart entry under XDG_CONFIG_HOME,
// pointing at this program; turning it off removes it, twice without error.
func TestSetStartAtLoginWritesTheAutostartEntry(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	p := filepath.Join(xdg, "autostart", "devtopo.desktop")

	if StartsAtLogin() {
		t.Fatal("starts at login before it was turned on")
	}
	if err := SetStartAtLogin(true); err != nil {
		t.Fatal(err)
	}
	if !StartsAtLogin() {
		t.Fatal("not on after turning it on")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	if want := "Exec=" + desktopExec(exe, "tray", "--no-open") + "\n"; !strings.Contains(string(b), want) {
		t.Fatalf("missing %q in:\n%s", want, b)
	}

	for i := 0; i < 2; i++ {
		if err := SetStartAtLogin(false); err != nil {
			t.Fatal(err)
		}
	}
	if StartsAtLogin() {
		t.Fatal("still on after turning it off")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("entry left behind: %v", err)
	}
}
