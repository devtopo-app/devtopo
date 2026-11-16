package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Connecting a source must sync it right away, so the user sees items without
// waiting for the slow background loop. These tests stub the sync funcs so no
// network is touched; they assert only that the connect path invokes them.

func TestConnectLinearTriggersSync(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return nil }
	called := 0
	s.syncLinear = func(context.Context) (int, error) { called++; return 3, nil }
	if rec := do(t, s, "POST", "/api/connectors/linear", `{"apiKey":"lin_api_test"}`); rec.Code != 200 {
		t.Fatalf("connect status %d: %s", rec.Code, rec.Body.String())
	}
	if called != 1 {
		t.Fatalf("connecting Linear must sync once immediately, synced %d times", called)
	}
}

// A failing immediate sync must not fail the connect: the key is validated and
// stored, and the background loop will catch up.
func TestConnectLinearSyncFailureStillConnects(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return nil }
	s.syncLinear = func(context.Context) (int, error) { return 0, context.DeadlineExceeded }
	if rec := do(t, s, "POST", "/api/connectors/linear", `{"apiKey":"lin_api_test"}`); rec.Code != 200 {
		t.Fatalf("a transient sync failure must not fail connect, got %d", rec.Code)
	}
	if v, ok, _ := s.store.GetSetting("linear_api_key"); !ok || v != "lin_api_test" {
		t.Fatal("the key must still be stored")
	}
}

// Opening the planner refreshes every configured connector in one call.
func TestRefreshRunsAllSyncs(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	gh, lin, cal, jira := 0, 0, 0, 0
	s.syncGitHub = func(context.Context) (int, error) { gh++; return 1, nil }
	s.syncLinear = func(context.Context) (int, error) { lin++; return 2, nil }
	s.syncCalendar = func(context.Context) (int, error) { cal++; return 3, nil }
	s.syncJira = func(context.Context) (int, error) { jira++; return 4, nil }
	rec := do(t, s, "POST", "/api/refresh", "")
	if rec.Code != 200 {
		t.Fatalf("refresh status %d: %s", rec.Code, rec.Body.String())
	}
	if gh != 1 || lin != 1 || cal != 1 || jira != 1 {
		t.Fatalf("refresh must sync each source once: gh=%d lin=%d cal=%d jira=%d", gh, lin, cal, jira)
	}
	var out map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("refresh should return a JSON summary: %v", err)
	}
	if out["linear"] != 2 || out["jira"] != 4 {
		t.Fatalf("refresh summary should carry per-source counts: %+v", out)
	}
}

// A sync that errors during refresh must not fail the whole refresh.
func TestRefreshToleratesOneSyncError(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.syncGitHub = func(context.Context) (int, error) { return 0, context.DeadlineExceeded }
	s.syncLinear = func(context.Context) (int, error) { return 2, nil }
	s.syncCalendar = func(context.Context) (int, error) { return 0, nil }
	rec := do(t, s, "POST", "/api/refresh", "")
	if rec.Code != 200 {
		t.Fatalf("one failing source must not fail refresh, got %d", rec.Code)
	}
}

func TestConnectJiraTriggersSync(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.jiraValidate = func(context.Context, string, string, string) error { return nil }
	called := 0
	s.syncJira = func(context.Context) (int, error) { called++; return 0, context.DeadlineExceeded }
	if rec := do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":"jira_test_token"}`); rec.Code != 200 {
		t.Fatalf("a transient sync failure must not fail connect, got %d", rec.Code)
	}
	if called != 1 {
		t.Fatalf("connecting Jira must sync once immediately, synced %d times", called)
	}
}
