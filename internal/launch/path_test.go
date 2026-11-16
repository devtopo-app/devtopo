package launch

import "testing"

// A shell may print greetings before the PATH; only what follows the marker counts.
func TestShellPathReadsAfterTheMarker(t *testing.T) {
	if got := shellPath("Welcome back!\n" + pathMarker + "/opt/homebrew/bin:/usr/bin\n"); got != "/opt/homebrew/bin:/usr/bin" {
		t.Fatalf("got %q", got)
	}
	if got := shellPath("no marker here"); got != "" {
		t.Fatalf("got %q", got)
	}
}

// The folders join the PATH once each, the ones already there keeping their place.
func TestMergePath(t *testing.T) {
	got := mergePath("/usr/bin:/bin", "/opt/homebrew/bin:/usr/bin", "/Users/me/.local/bin", "/bin")
	if want := "/usr/bin:/bin:/opt/homebrew/bin:/Users/me/.local/bin"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
