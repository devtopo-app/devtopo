package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/orca"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/window"
)

var procStart = time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)

// liveIn registers session id as running under the processes given, child
// first, so its host is found by walking them.
func liveIn(t *testing.T, s *Server, id string, chain ...string) {
	t.Helper()
	procs, _ := s.snapshot()
	pid := 100 + len(procs)*10
	for i, name := range chain {
		procs = append(procs, host.Proc{PID: pid + i, PPID: pid + i + 1, Name: name, Start: procStart})
	}
	s.snapshot = func() ([]host.Proc, error) { return procs, nil }
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q,"procStart":"%d"}`, pid, id, procStart.UnixNano()/100+116444736000000000)
	if err := os.WriteFile(filepath.Join(s.sessionsDir, strconv.Itoa(pid)+".json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// recordOpener replaces the opener with one that records every command and
// raise; windows maps a pid to its window, consoles a pid to its console's.
func recordOpener(s *Server, windows, consoles map[int]uintptr) *[]string {
	var calls []string
	lookup := func(m map[int]uintptr) func(int) (uintptr, error) {
		return func(pid int) (uintptr, error) {
			if h, ok := m[pid]; ok {
				return h, nil
			}
			return 0, window.ErrNotFound
		}
	}
	s.opener = &host.Opener{
		OS:   "windows",
		Code: "code", Cursor: "cursor", WT: "wt", DesktopDir: s.opener.DesktopDir,
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
	return &calls
}

// ownedByDesktop records session id as one of the Claude desktop app's.
func ownedByDesktop(t *testing.T, s *Server, id string) {
	t.Helper()
	dir := filepath.Join(s.opener.DesktopDir, "acct", "org")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local_"+id+".json"), []byte(`{"sessionId":"local_`+id+`","cliSessionId":"`+id+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hostSession(tnow time.Time, id, cwd string) store.Session {
	return store.Session{ID: id, ProjectKey: "p", CWD: cwd, Branch: "main", StartedAt: tnow.Add(-time.Hour), LastActivityAt: tnow, LastEventAt: tnow}
}

func TestTodayCardsCarryTheHostAndWhetherTheSessionIsLive(t *testing.T) {
	tnow := time.Now().UTC()
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "code", `C:\w`), hostSession(tnow, "gone", `C:\w`), hostSession(tnow, "odd", `C:\w`), hostSession(tnow, "desk", `C:\w`)}, nil)
	liveIn(t, s, "code", "claude.exe", "Code.exe")
	liveIn(t, s, "odd", "claude.exe", "node.exe")
	ownedByDesktop(t, s, "desk")
	if err := s.store.SetSessionHost("gone", host.WindowsTerminal); err != nil {
		t.Fatal(err)
	}

	cards := map[string]Card{}
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	for _, l := range []Lane{v.Running, v.Waiting, v.Idle} {
		for _, c := range l.Cards {
			cards[c.ID] = c
		}
	}
	if c := cards["code"]; c.Host != host.VSCode || !c.Live {
		t.Errorf("live in vs code: %+v", c)
	}
	if c := cards["gone"]; c.Host != host.WindowsTerminal || c.Live {
		t.Errorf("a closed session keeps where it ran: %+v", c)
	}
	if c := cards["odd"]; c.Host != "" || !c.Live {
		t.Errorf("live in an unknown app: %+v", c)
	}
	// the desktop app runs claude only during a turn; its own files say it is its
	if c := cards["desk"]; c.Host != host.ClaudeDesktop || c.Live {
		t.Errorf("a desktop app session never seen running: %+v", c)
	}
	for id, c := range cards {
		if !c.CanFocus {
			t.Errorf("%s: off Linux an app can be focused", id)
		}
	}
	if hosts, _ := s.store.SessionHosts(); hosts["code"] != host.VSCode || hosts["desk"] != host.ClaudeDesktop || len(hosts) != 3 {
		t.Errorf("the detected host is remembered, an unknown one is not: %v", hosts)
	}
}

func TestFocusOpensTheSessionInItsApp(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ids := []string{"vs", "cur", "wt", "con", "desk", "odd", "vsidle", "deskidle", "wtclosed", "closed"}
	var sessions []store.Session
	for _, id := range ids {
		sessions = append(sessions, hostSession(tnow, id, `C:\w\app`))
	}
	s := newSeededServer(t, tnow, sessions, nil)
	liveIn(t, s, "vs", "claude.exe", "Code.exe")                        // 100
	liveIn(t, s, "cur", "claude.exe", "Cursor.exe")                     // 120
	liveIn(t, s, "wt", "claude.exe", "pwsh.exe", "WindowsTerminal.exe") // 140
	liveIn(t, s, "con", "claude.exe", "cmd.exe")                        // 170
	liveIn(t, s, "desk", "claude.exe", "claude.exe")                    // 190, the app is 191
	liveIn(t, s, "odd", "claude.exe", "node.exe")                       // 210
	ownedByDesktop(t, s, "desk")
	ownedByDesktop(t, s, "deskidle")
	s.store.SetSessionHost("vsidle", host.VSCode)
	s.store.SetSessionHost("wtclosed", host.WindowsTerminal)
	calls := recordOpener(s, map[int]uintptr{191: 1910}, map[int]uintptr{140: 1400, 170: 1700})

	cases := []struct {
		id   string
		code int
		want []string
	}{
		{"vs", 200, []string{`in C:\w\app: code .`, "rundll32 url.dll,FileProtocolHandler vscode://anthropic.claude-code/open?session=vs"}},
		{"cur", 200, []string{`in C:\w\app: cursor .`, "rundll32 url.dll,FileProtocolHandler cursor://anthropic.claude-code/open?session=cur"}},
		// the window showing the session's console, whatever process owns it
		{"wt", 200, []string{"raise 1400"}},
		{"con", 200, []string{"raise 1700"}},
		{"desk", 200, []string{"rundll32 url.dll,FileProtocolHandler claude://code/continue?session=local_desk", "raise 1910"}},
		{"odd", 404, nil},
		// an app keeps a session open between turns, with no claude running
		{"vsidle", 200, []string{`in C:\w\app: code .`, "rundll32 url.dll,FileProtocolHandler vscode://anthropic.claude-code/open?session=vsidle"}},
		{"deskidle", 200, []string{"rundll32 url.dll,FileProtocolHandler claude://code/continue?session=local_deskidle", "raise 1910"}},
		{"wtclosed", 409, nil},
		{"closed", 409, nil},
		{"ghost", 404, nil},
	}
	for _, c := range cases {
		*calls = nil
		rec := do(t, s, "POST", "/api/sessions/"+c.id+"/focus", "")
		if rec.Code != c.code || strings.Join(*calls, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s: %d %s\n%s", c.id, rec.Code, rec.Body.String(), strings.Join(*calls, "\n"))
		}
	}

	*calls = nil
	s.opener.Code = ""
	if rec := do(t, s, "POST", "/api/sessions/vs/focus", ""); rec.Code != 502 || strings.TrimSpace(rec.Body.String()) != "VS Code not available" {
		t.Fatalf("no cli: %d %s", rec.Code, rec.Body.String())
	}
}

// On macOS a session in Terminal or iTerm2 is opened in its tab, which the
// opener finds by the tty of the session's claude process.
func TestFocusOpensAMacTerminalSessionInItsTab(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "term", "/w/app"), hostSession(tnow, "iterm", "/w/app")}, nil)
	liveIn(t, s, "term", "claude", "-zsh", "/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal") // 100
	liveIn(t, s, "iterm", "claude", "-zsh", "/Applications/iTerm.app/Contents/MacOS/iTerm2")                      // 130
	var calls []string
	s.opener = &host.Opener{OS: "darwin",
		Run: func(_ context.Context, bin string, args []string) error {
			calls = append(calls, fmt.Sprintf("%s %t %s", bin, strings.Contains(strings.Join(args, " "), `"iTerm2"`), args[len(args)-1]))
			return nil
		},
		Output: func(_ context.Context, bin string, args []string) ([]byte, error) {
			calls = append(calls, bin+" "+strings.Join(args, " "))
			return []byte("ttys001\n"), nil
		},
	}
	for _, id := range []string{"term", "iterm"} {
		if rec := do(t, s, "POST", "/api/sessions/"+id+"/focus", ""); rec.Code != 200 {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	want := "ps -o tty= -p 100|osascript false /dev/ttys001|ps -o tty= -p 130|osascript true /dev/ttys001"
	if strings.Join(calls, "|") != want {
		t.Fatalf("calls: %s", strings.Join(calls, "|"))
	}
}

func TestFocusSwitchesToTheSessionsOrcaTerminal(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "s1", `C:\w\app`)}, nil)
	liveIn(t, s, "s1", "claude.exe", "pwsh.exe", "Orca.exe")
	s.orcaList = func(context.Context) ([]orca.Terminal, error) {
		return []orca.Terminal{{Handle: "term_a", WorktreePath: "C:/w/app", Branch: "main", Agent: "claude", LastOutputAt: tnow}}, nil
	}
	var switched []string
	s.orcaSwitch = func(_ context.Context, h string) error { switched = append(switched, h); return nil }

	if rec := do(t, s, "POST", "/api/sessions/s1/focus", ""); rec.Code != 200 || len(switched) != 1 || switched[0] != "term_a" {
		t.Fatalf("focus: %d %s %v", rec.Code, rec.Body.String(), switched)
	}

	s.orcaList = func(context.Context) ([]orca.Terminal, error) { return nil, nil }
	if rec := do(t, s, "POST", "/api/sessions/s1/focus", ""); rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != `not open in Orca: no Orca terminal in C:\w\app` {
		t.Fatalf("no terminal: %d %s", rec.Code, rec.Body.String())
	}

	s.orcaList = func(context.Context) ([]orca.Terminal, error) { return nil, orca.ErrUnavailable }
	if rec := do(t, s, "POST", "/api/sessions/s1/focus", ""); rec.Code != 503 || !strings.Contains(rec.Body.String(), "Orca not available") {
		t.Fatalf("no cli: %d %s", rec.Code, rec.Body.String())
	}
}

func TestFailedOrcaOpenIsLoggedWithTheFoldersOrcaListed(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "s1", `C:\w\app`)}, nil)
	liveIn(t, s, "s1", "claude.exe", "pwsh.exe", "Orca.exe")
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	s.orcaList = func(context.Context) ([]orca.Terminal, error) {
		return []orca.Terminal{{Handle: "term_a", WorktreePath: "C:/w/other", Agent: "claude"}}, nil
	}
	do(t, s, "POST", "/api/sessions/s1/focus", "")
	if got := logged.String(); !strings.Contains(got, `s1: no Orca terminal in C:\w\app`) || !strings.Contains(got, "C:/w/other") {
		t.Fatalf("miss not logged: %q", got)
	}

	logged.Reset()
	s.orcaList = func(context.Context) ([]orca.Terminal, error) { return nil, orca.ErrUnavailable }
	do(t, s, "POST", "/api/sessions/s1/focus", "")
	if got := logged.String(); !strings.Contains(got, "s1: "+orca.ErrUnavailable.Error()) {
		t.Fatalf("list failure not logged: %q", got)
	}

	logged.Reset()
	s.orcaList = func(context.Context) ([]orca.Terminal, error) {
		return []orca.Terminal{{Handle: "term_a", WorktreePath: "C:/w/app", Agent: "claude"}}, nil
	}
	s.orcaSwitch = func(context.Context, string) error { return errors.New("switch broke") }
	do(t, s, "POST", "/api/sessions/s1/focus", "")
	if got := logged.String(); !strings.Contains(got, "s1: switch broke") {
		t.Fatalf("switch failure not logged: %q", got)
	}
}

func TestResumeReopensAClosedSessionInItsFolder(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "closed", `C:\w\app`), hostSession(tnow, "open", `C:\w\app`), hostSession(tnow, "nowhere", "")}, nil)
	liveIn(t, s, "open", "claude.exe", "Code.exe")
	calls := recordOpener(s, nil, nil)

	if rec := do(t, s, "POST", "/api/sessions/closed/resume", ""); rec.Code != 200 || strings.Join(*calls, "\n") != `in C:\w\app: wt -d . cmd /k claude --resume closed` {
		t.Fatalf("resume: %d %s %q", rec.Code, rec.Body.String(), *calls)
	}
	*calls = nil
	for id, code := range map[string]int{"open": 409, "nowhere": 409, "ghost": 404} {
		if rec := do(t, s, "POST", "/api/sessions/"+id+"/resume", ""); rec.Code != code || len(*calls) != 0 {
			t.Errorf("%s: %d %s %q", id, rec.Code, rec.Body.String(), *calls)
		}
	}
}

// Linux cannot bring another app's window forward: every card says so, focus
// is refused before any command runs, and a closed session still resumes in
// a new terminal window in its folder.
func TestOnLinuxFocusIsRefusedAndResumeOpensATerminal(t *testing.T) {
	tnow := time.Now().UTC()
	s := newSeededServer(t, tnow, []store.Session{hostSession(tnow, "live", "/home/me/app"), hostSession(tnow, "closed", "/home/me/app"), hostSession(tnow, "idle", "/home/me/app")}, nil)
	liveIn(t, s, "live", "2.1.0", "bash", "gnome-terminal-server")
	s.store.SetSessionHost("idle", host.VSCode)
	var calls []string
	s.opener = &host.Opener{OS: "linux", DesktopDir: s.opener.DesktopDir,
		Run: func(_ context.Context, bin string, args []string) error {
			calls = append(calls, bin+" "+strings.Join(args, " "))
			return nil
		},
		LookPath: func(f string) (string, error) {
			if f == "konsole" {
				return "/usr/bin/konsole", nil
			}
			return "", os.ErrNotExist
		},
		StartIn: func(dir, bin string, args []string) error {
			calls = append(calls, "in "+dir+": "+bin+" "+strings.Join(args, " "))
			return nil
		},
	}

	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range []Lane{v.Running, v.Waiting, v.Idle} {
		for _, c := range l.Cards {
			n++
			if c.CanFocus {
				t.Errorf("%s: Linux cannot focus", c.ID)
			}
			if c.ID == "live" && (c.Host != host.LinuxTerminal || !c.Live) {
				t.Errorf("live in a terminal: %+v", c)
			}
		}
	}
	if n != 3 {
		t.Fatalf("%d cards", n)
	}
	for _, id := range []string{"live", "idle"} {
		if rec := do(t, s, "POST", "/api/sessions/"+id+"/focus", ""); rec.Code != 501 || !strings.Contains(rec.Body.String(), "not supported") || len(calls) != 0 {
			t.Errorf("%s: %d %s %q", id, rec.Code, rec.Body.String(), calls)
		}
	}
	for _, id := range []string{"closed", "idle"} {
		calls = nil
		if rec := do(t, s, "POST", "/api/sessions/"+id+"/resume", ""); rec.Code != 200 || strings.Join(calls, "|") != "in /home/me/app: /usr/bin/konsole --workdir /home/me/app -e claude --resume "+id {
			t.Errorf("resume %s: %d %s %q", id, rec.Code, rec.Body.String(), calls)
		}
	}
}
