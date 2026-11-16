package launch

import (
	"strings"
	"testing"
)

// The macOS LaunchAgent runs the app at login, with the path XML-escaped.
func TestLaunchAgentRunsTheAppAtLogin(t *testing.T) {
	got := launchAgent("/Applications/R&D/DevTopo.app/Contents/MacOS/devtopo")
	for _, want := range []string{
		"<string>" + agentLabel + "</string>",
		"<string>/Applications/R&amp;D/DevTopo.app/Contents/MacOS/devtopo</string>\n\t\t<string>tray</string>\n\t\t<string>--no-open</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
