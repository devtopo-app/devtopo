package host

import (
	"fmt"
	"testing"
	"time"
)

// ps pads its columns and lstart pads a one-digit day; a path keeps its
// spaces. A line that does not parse is skipped.
func TestParsePS(t *testing.T) {
	out := "    1     0 Thu Oct  1 08:00:00 2026     /sbin/launchd\n" +
		"  420     1 Thu Oct 29 09:00:05 2026     /Applications/Visual Studio Code.app/Contents/MacOS/Code\n" +
		"  777   420 Thu Oct 29 09:00:06 2026     -zsh\n" +
		"garbage\n"
	got := parsePS([]byte(out))
	want := []Proc{
		{PID: 1, PPID: 0, Name: "/sbin/launchd", Start: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)},
		{PID: 420, PPID: 1, Name: "/Applications/Visual Studio Code.app/Contents/MacOS/Code", Start: time.Date(2026, 10, 29, 9, 0, 5, 0, time.UTC)},
		{PID: 777, PPID: 420, Name: "-zsh", Start: time.Date(2026, 10, 29, 9, 0, 6, 0, time.UTC)},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}
