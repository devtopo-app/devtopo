package orca

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

const listJSON = `{"id":"x","ok":true,"result":{"terminals":[
{"handle":"term_a","worktreePath":"C:/w/app","branch":"refs/heads/main","title":"Fix it","lastOutputAt":1793274831623,"agentIdentity":"claude"},
{"handle":"term_b","worktreePath":"C:/w/other","branch":"","title":"shell","lastOutputAt":1793184490681}
],"totalCount":2,"truncated":false}}`

func TestListParsesTerminals(t *testing.T) {
	var gotBin string
	var gotArgs []string
	c := &Client{Bin: "orca", run: func(_ context.Context, bin string, args []string) ([]byte, error) {
		gotBin, gotArgs = bin, args
		return []byte(listJSON), nil
	}}
	ts, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotBin != "orca" || strings.Join(gotArgs, " ") != "terminal list --json" {
		t.Fatalf("command: %s %v", gotBin, gotArgs)
	}
	if len(ts) != 2 {
		t.Fatalf("want 2, got %d", len(ts))
	}
	a := ts[0]
	if a.Handle != "term_a" || a.WorktreePath != "C:/w/app" || a.Branch != "main" || a.Agent != "claude" || a.Title != "Fix it" {
		t.Fatalf("terminal: %+v", a)
	}
	if !a.LastOutputAt.Equal(time.UnixMilli(1793274831623)) {
		t.Fatalf("lastOutputAt: %v", a.LastOutputAt)
	}
}

func TestNoBinaryIsUnavailable(t *testing.T) {
	c := &Client{run: func(context.Context, string, []string) ([]byte, error) {
		t.Fatal("must not run without a binary")
		return nil, nil
	}}
	if _, err := c.List(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("list: %v", err)
	}
	if err := c.Switch(context.Background(), "term_a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("switch: %v", err)
	}
}

func TestSwitchThenRaisesWindowAndIgnoresRaiseFailure(t *testing.T) {
	var calls []string
	c := &Client{Bin: "orca", run: func(_ context.Context, bin string, args []string) ([]byte, error) {
		calls = append(calls, bin+" "+strings.Join(args, " "))
		if bin == "powershell" {
			return nil, errors.New("no window")
		}
		return nil, nil
	}}
	if err := c.Switch(context.Background(), "term_a"); err != nil {
		t.Fatalf("a failed raise is not fatal: %v", err)
	}
	if len(calls) != 2 || calls[0] != "orca terminal switch --terminal term_a" || !strings.Contains(calls[1], "AppActivate('Orca')") {
		t.Fatalf("calls: %q", calls)
	}
}

func TestSwitchRestoresWindowBeforeRaiseAndIgnoresRestoreFailure(t *testing.T) {
	var calls []string
	c := &Client{Bin: "orca",
		run: func(_ context.Context, bin string, args []string) ([]byte, error) {
			calls = append(calls, bin)
			return nil, nil
		},
		restore: func() error {
			calls = append(calls, "restore")
			return errors.New("no window")
		},
	}
	if err := c.Switch(context.Background(), "term_a"); err != nil {
		t.Fatalf("a failed restore is not fatal: %v", err)
	}
	if strings.Join(calls, " ") != "orca restore powershell" {
		t.Fatalf("calls: %q", calls)
	}
}

// On macOS Orca is brought forward by name, which also un-minimizes it.
func TestSwitchOnMacActivatesOrca(t *testing.T) {
	var calls []string
	c := &Client{Bin: "orca", OS: "darwin",
		run: func(_ context.Context, bin string, args []string) ([]byte, error) {
			calls = append(calls, fmt.Sprintf("%s %q", bin, args))
			return nil, nil
		},
		restore: func() error {
			calls = append(calls, "restore")
			return nil
		},
	}
	if err := c.Switch(context.Background(), "term_a"); err != nil {
		t.Fatal(err)
	}
	want := `orca ["terminal" "switch" "--terminal" "term_a"]|osascript ["-e" "tell application \"Orca\" to activate"]`
	if strings.Join(calls, "|") != want {
		t.Fatalf("calls: %s", strings.Join(calls, "|"))
	}
}

func TestSwitchFailureSkipsRaise(t *testing.T) {
	n := 0
	c := &Client{Bin: "orca", run: func(context.Context, string, []string) ([]byte, error) {
		n++
		return nil, errors.New("no such terminal")
	}}
	if err := c.Switch(context.Background(), "term_x"); err == nil || n != 1 {
		t.Fatalf("err %v after %d calls", err, n)
	}
}

func TestMatch(t *testing.T) {
	last := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ts := []Terminal{
		{Handle: "shell", WorktreePath: "C:/w/app", Branch: "main", LastOutputAt: last},
		{Handle: "old", WorktreePath: "C:/w/app", Branch: "main", Agent: "claude", LastOutputAt: last.Add(-3 * time.Hour)},
		{Handle: "near", WorktreePath: "C:/w/app", Branch: "main", Agent: "claude", LastOutputAt: last.Add(time.Minute)},
		{Handle: "otherbranch", WorktreePath: "C:/w/app", Branch: "dev", Agent: "claude", LastOutputAt: last},
		{Handle: "elsewhere", WorktreePath: "C:/w/apple", Branch: "main", Agent: "claude", LastOutputAt: last},
	}
	cases := []struct {
		name, cwd, branch, want string
	}{
		{"same branch, closest output wins", `C:\w\app`, "main", "near"},
		{"branch preferred over time", `C:\w\app`, "dev", "otherbranch"},
		{"unknown branch falls back to closest", `C:\w\app`, "feat/x", "otherbranch"},
		{"cwd inside the worktree", `C:\w\app\sub\dir`, "main", "near"},
		{"a sibling with a common prefix is not inside", `C:\w\ap`, "main", ""},
		{"no terminal there", `C:\w\none`, "main", ""},
	}
	for _, c := range cases {
		got, miss := Match(ts, c.cwd, c.branch, "claude", last)
		if (miss == "") != (c.want != "") || got.Handle != c.want {
			t.Errorf("%s: got %q miss=%q, want %q", c.name, got.Handle, miss, c.want)
		}
	}
	if runtime.GOOS == "windows" {
		if got, miss := Match(ts, `c:\W\App`, "main", "claude", last); miss != "" || got.Handle != "near" {
			t.Errorf("case-insensitive on windows: got %q", got.Handle)
		}
	}
}

func TestMatchPrefersTheSessionsAgent(t *testing.T) {
	last := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ts := []Terminal{
		{Handle: "shell", WorktreePath: "C:/w/a", Branch: "main", LastOutputAt: last},
		{Handle: "claude", WorktreePath: "C:/w/a", Branch: "dev", Agent: "claude", LastOutputAt: last.Add(-5 * time.Hour)},
	}
	for _, agent := range []string{"claude", ""} {
		if got, miss := Match(ts, "C:/w/a", "main", agent, last); miss != "" || got.Handle != "claude" {
			t.Fatalf("agent %q: a labelled terminal beats a closer plain one: %q %q", agent, got.Handle, miss)
		}
	}
}

func TestMatchFallsBackToAnUnlabelledTerminal(t *testing.T) {
	ts := []Terminal{{Handle: "shell", WorktreePath: "C:/w/a"}}
	if got, miss := Match(ts, "C:/w/a", "main", "claude", time.Now()); miss != "" || got.Handle != "shell" {
		t.Fatalf("got %q %q", got.Handle, miss)
	}
}

func TestMatchCodexSession(t *testing.T) {
	both := []Terminal{{Handle: "shell", WorktreePath: "C:/w/a"}, {Handle: "codex", WorktreePath: "C:/w/a", Agent: "codex"}}
	if got, miss := Match(both, "C:/w/a", "main", "codex", time.Now()); miss != "" || got.Handle != "codex" {
		t.Fatalf("codex terminal: %q %q", got.Handle, miss)
	}
	if got, miss := Match(both[:1], "C:/w/a", "main", "codex", time.Now()); miss != "" || got.Handle != "shell" {
		t.Fatalf("plain terminal: %q %q", got.Handle, miss)
	}
}

func TestMatchNeverTakesAnotherAgentsTerminal(t *testing.T) {
	ts := []Terminal{{Handle: "claude", WorktreePath: "C:/w/a", Agent: "claude"}}
	got, miss := Match(ts, "C:/w/a", "main", "codex", time.Now())
	if got.Handle != "" || miss != "no Codex terminal in C:/w/a" {
		t.Fatalf("got %q %q", got.Handle, miss)
	}
	ts = []Terminal{{Handle: "codex", WorktreePath: "C:/w/a", Agent: "codex"}}
	if _, miss := Match(ts, "C:/w/a", "main", "", time.Now()); miss != "no Claude terminal in C:/w/a" {
		t.Fatalf("an empty agent is Claude: %q", miss)
	}
}

func TestMatchSaysWhenNoTerminalIsInTheFolder(t *testing.T) {
	ts := []Terminal{{Handle: "x", WorktreePath: "C:/w/b", Agent: "claude"}}
	if got, miss := Match(ts, "C:/w/a", "main", "claude", time.Now()); got.Handle != "" || miss != "no Orca terminal in C:/w/a" {
		t.Fatalf("got %q %q", got.Handle, miss)
	}
	if _, miss := Match(nil, `C:\W\A`, "main", "claude", time.Now()); miss != `no Orca terminal in C:\W\A` {
		t.Fatalf("the folder prints as stored: %q", miss)
	}
}
