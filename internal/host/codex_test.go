package host

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The Codex app runs as ChatGPT.exe with its codex.exe engine under it; a
// codex CLI under a shell is not the app.
func TestCodexPIDFindsTheAppsMainProcess(t *testing.T) {
	procs := []Proc{
		{PID: 1, Name: "explorer.exe"},
		{PID: 5, PPID: 1, Name: "pwsh.exe"},
		{PID: 6, PPID: 5, Name: "codex.exe"},
		{PID: 70, PPID: 1, Name: "ChatGPT.exe"},
		{PID: 71, PPID: 70, Name: "ChatGPT.exe"},
		{PID: 72, PPID: 70, Name: "codex.exe"},
	}
	if got := CodexPID(procs); got != 70 {
		t.Fatalf("got %d", got)
	}
	if got := CodexPID(procs[:3]); got != 0 {
		t.Fatalf("no app running: got %d", got)
	}
}

// The Codex app opens a thread by its link, then its window is raised; with
// the app not running the link alone starts it.
func TestCodexOpensTheThreadThenRaisesTheApp(t *testing.T) {
	o, calls := fakeOpener(t, map[int]uintptr{70: 77}, nil)
	if err := o.Codex(context.Background(), "s1", 70); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*calls, "|") != "rundll32 url.dll,FileProtocolHandler codex://threads/s1|raise 77" {
		t.Fatalf("calls: %q", *calls)
	}
	*calls = nil
	if err := o.Codex(context.Background(), "s1", 0); err != nil || strings.Join(*calls, "|") != "rundll32 url.dll,FileProtocolHandler codex://threads/s1" {
		t.Fatalf("app not running: %v %q", err, *calls)
	}
}

func TestResumeCodexRunsCodexResumeInTheFolder(t *testing.T) {
	o, _ := fakeOpener(t, nil, nil)
	var argv [][]string
	o.RunIn = func(_ context.Context, dir, bin string, args []string) error {
		argv = append(argv, append([]string{dir, bin}, args...))
		return nil
	}
	if err := o.ResumeCodex(context.Background(), `C:\w\my app`, "s1"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{`C:\w\my app`, "wt", "-d", ".", "cmd", "/k", "codex", "resume", "s1"}}
	if fmt.Sprintf("%q", argv) != fmt.Sprintf("%q", want) {
		t.Fatalf("argv:\n%q", argv)
	}
}
