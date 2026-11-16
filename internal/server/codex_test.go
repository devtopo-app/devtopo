package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/usage"
)

func codexSession(tnow time.Time, id, cwd string) store.Session {
	se := hostSession(tnow, id, cwd)
	se.Agent = "codex"
	return se
}

// A Codex card says it is Codex's and carries the title Codex gave the
// thread; a Claude card carries neither.
func TestTodayCodexCardsCarryAgentAndTitle(t *testing.T) {
	tnow := time.Now().UTC()
	claude := hostSession(tnow, "cl", `C:\w`)
	claude.Agent = "claude"
	s := newSeededServer(t, tnow, []store.Session{codexSession(tnow, "cx", `C:\w`), claude}, nil)
	s.codexTitles = func() map[string]string { return map[string]string{"cx": "Add a readme"} }

	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	cards := map[string]Card{}
	for _, c := range v.Running.Cards {
		cards[c.ID] = c
	}
	if c := cards["cx"]; c.Agent != "codex" || c.Title != "Add a readme" {
		t.Fatalf("codex card: %+v", c)
	}
	if c := cards["cl"]; c.Agent != "claude" || c.Title != "" {
		t.Fatalf("claude card: %+v", c)
	}
}

func TestAPIUsageCarriesCodexWindows(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	s.usage = func(_ context.Context) usage.Result { return usage.Result{Status: usage.StatusSignedOut} }
	if body := get(t, s, "/api/usage").Body.String(); !strings.Contains(body, `"Codex":[]`) {
		t.Fatalf("no codex data: %s", body)
	}
	s.codexUsage = func() []usage.Window { return []usage.Window{{Label: "30d", Percent: 3, ResetsAt: tnow}} }
	var got struct {
		Status string
		Codex  []usage.Window
	}
	if err := json.Unmarshal(get(t, s, "/api/usage").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != usage.StatusSignedOut || len(got.Codex) != 1 || got.Codex[0].Label != "30d" {
		t.Fatalf("payload: %+v", got)
	}
}

// A Codex app session opens its thread in the app and raises the app; a Codex
// CLI session resumes with codex resume.
func TestFocusAndResumeCodexSessions(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{codexSession(tnow, "app", `C:\w`), codexSession(tnow, "cli", `C:\w\cli`)}, nil)
	if err := s.store.SetSessionHost("app", host.CodexApp); err != nil {
		t.Fatal(err)
	}
	s.snapshot = func() ([]host.Proc, error) {
		return []host.Proc{{PID: 70, PPID: 1, Name: "ChatGPT.exe"}, {PID: 72, PPID: 70, Name: "codex.exe"}}, nil
	}
	calls := recordOpener(s, map[int]uintptr{70: 77}, nil)

	if rec := do(t, s, "POST", "/api/sessions/app/focus", ""); rec.Code != 200 || strings.Join(*calls, "|") != "rundll32 url.dll,FileProtocolHandler codex://threads/app|raise 77" {
		t.Fatalf("focus: %d %s %q", rec.Code, rec.Body.String(), *calls)
	}
	*calls = nil
	if rec := do(t, s, "POST", "/api/sessions/cli/resume", ""); rec.Code != 200 || strings.Join(*calls, "|") != `in C:\w\cli: wt -d . cmd /k codex resume cli` {
		t.Fatalf("resume: %d %s %q", rec.Code, rec.Body.String(), *calls)
	}
}
