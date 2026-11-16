package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/jira"
	"github.com/devtopo-app/devtopo/internal/store"
)

func TestConnectLinearStoresKey(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return nil } // key verifies
	rec := do(t, s, "POST", "/api/connectors/linear", `{"apiKey":"lin_api_test"}`)
	if rec.Code != 200 {
		t.Fatalf("connect status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "lin_api_test") {
		t.Fatal("the connect response must never echo the key")
	}
	if v, ok, _ := s.store.GetSetting("linear_api_key"); !ok || v != "lin_api_test" {
		t.Fatalf("key not stored: %q ok=%v", v, ok)
	}
	rec = do(t, s, "GET", "/api/connectors", "")
	if strings.Contains(rec.Body.String(), "lin_api_test") {
		t.Fatal("the connectors endpoint must never expose the key")
	}
	var conns []connectorStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &conns); err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		if c.Name == "Linear" && c.Status != "connected" {
			t.Fatalf("Linear should be connected after a key is set: %+v", c)
		}
	}
}

func TestConnectLinearRejectsEmpty(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	if rec := do(t, s, "POST", "/api/connectors/linear", `{"apiKey":""}`); rec.Code != 400 {
		t.Fatalf("empty key -> 400, got %d", rec.Code)
	}
}

func TestConnectCalendarStoresURL(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.calendarValidate = func(context.Context, string) error { return nil }
	rec := do(t, s, "POST", "/api/connectors/calendar", `{"icalUrl":"https://cal/secret.ics"}`)
	if rec.Code != 200 {
		t.Fatalf("connect status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("the connect response must never echo the url")
	}
	if feeds, _ := s.store.CalendarFeeds(); len(feeds) != 1 || feeds[0].URL != "https://cal/secret.ics" {
		t.Fatalf("url not stored as a feed: %+v", feeds)
	}
	rec = do(t, s, "GET", "/api/connectors", "")
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("the connectors endpoint must never expose the url")
	}
	var conns []connectorStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &conns)
	for _, c := range conns {
		if c.Name == "Calendar" && c.Status != "connected" {
			t.Fatalf("Calendar should be connected: %+v", c)
		}
	}
}

func TestConnectCalendarRejectsBadURL(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.calendarValidate = func(context.Context, string) error { return errors.New("status 404") }
	rec := do(t, s, "POST", "/api/connectors/calendar", `{"icalUrl":"https://cal/bad.ics"}`)
	if rec.Code == 200 {
		t.Fatal("an unreachable calendar URL must not connect")
	}
	if feeds, _ := s.store.CalendarFeeds(); len(feeds) != 0 {
		t.Fatal("a failing URL must not be stored")
	}
}

func TestConnectLinearRejectsBadKey(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.linearValidate = func(context.Context, string) error { return errors.New("status 401") }
	rec := do(t, s, "POST", "/api/connectors/linear", `{"apiKey":"lin_api_bad"}`)
	if rec.Code == 200 {
		t.Fatal("a key that fails verification must not connect")
	}
	if _, ok, _ := s.store.GetSetting("linear_api_key"); ok {
		t.Fatal("a key that fails verification must not be stored")
	}
	if strings.Contains(rec.Body.String(), "lin_api_bad") {
		t.Fatal("the error must not echo the key")
	}
}

func TestConnectorsStatus(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	rec := do(t, s, "GET", "/api/connectors", "")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var conns []connectorStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &conns); err != nil {
		t.Fatal(err)
	}
	if len(conns) != 6 {
		t.Fatalf("want 6 connectors, got %d", len(conns))
	}
	status := map[string]string{}
	for _, c := range conns {
		if c.Name == "" {
			t.Fatalf("connector with no name: %+v", conns)
		}
		status[c.Name] = c.Status
	}
	if status["Linear"] != "not configured" || status["Calendar"] != "not configured" || status["Jira"] != "not configured" {
		t.Fatalf("linear/calendar/jira should be not configured: %+v", conns)
	}
}

func TestConnectJiraStoresCredentials(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	var gotSite string
	s.jiraValidate = func(_ context.Context, site, _, _ string) error { gotSite = site; return nil }
	rec := do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net/","email":"me@example.com","apiToken":"jira_test_token"}`)
	if rec.Code != 200 {
		t.Fatalf("connect status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "jira_test_token") {
		t.Fatal("the connect response must never echo the token")
	}
	if gotSite != "https://example.atlassian.net" {
		t.Fatalf("the site must be normalised before validating: %q", gotSite)
	}
	for k, want := range map[string]string{
		"jira_site": "https://example.atlassian.net", "jira_email": "me@example.com", "jira_api_token": "jira_test_token",
	} {
		if v, ok, _ := s.store.GetSetting(k); !ok || v != want {
			t.Fatalf("%s not stored: %q ok=%v", k, v, ok)
		}
	}
	rec = do(t, s, "GET", "/api/connectors", "")
	if strings.Contains(rec.Body.String(), "jira_test_token") {
		t.Fatal("the connectors endpoint must never expose the token")
	}
	var conns []connectorStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &conns)
	for _, c := range conns {
		if c.Name == "Jira" && c.Status != "connected" {
			t.Fatalf("Jira should be connected: %+v", c)
		}
	}
}

func TestConnectorsShowJiraSiteAndEmailButNotTheToken(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.jiraValidate = func(context.Context, string, string, string) error { return nil }
	do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":"jira_test_token"}`)
	rec := do(t, s, "GET", "/api/connectors", "")
	var conns []connectorStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &conns)
	for _, c := range conns {
		if c.Name == "Jira" && (c.Values["site"] != "https://example.atlassian.net" || c.Values["email"] != "me@example.com") {
			t.Fatalf("Jira must show its stored site and email: %+v", c)
		}
	}
	if strings.Contains(rec.Body.String(), "jira_test_token") {
		t.Fatal("the token must never be shown")
	}
}

func TestDisconnectJiraForgetsCredentialsAndTickets(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.jiraValidate = func(context.Context, string, string, string) error { return nil }
	do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":"jira_test_token"}`)
	_ = s.store.ReplaceExternalItems("jira", []store.ExternalItem{{Source: "jira", Kind: "task", ExternalID: "OPS-1", Title: "Fix"}})

	if rec := do(t, s, "DELETE", "/api/connectors/jira", ""); rec.Code != 200 {
		t.Fatalf("disconnect status %d: %s", rec.Code, rec.Body.String())
	}
	for _, k := range []string{"jira_site", "jira_email", "jira_api_token"} {
		if v, _, _ := s.store.GetSetting(k); v != "" {
			t.Fatalf("%s must be forgotten", k)
		}
	}
	if items, _ := s.store.ListExternalItems("jira"); len(items) != 0 {
		t.Fatalf("Jira tickets must go with the connection: %+v", items)
	}
	if s.jiraStatus() != "not configured" {
		t.Fatal("Jira must show as not configured")
	}
}

func TestDisconnectCalendarForgetsTheEvents(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, now, nil, nil)
	feed, _ := s.store.AddCalendarFeed("", "https://cal/secret.ics")
	_ = s.store.ReplaceFeedEvents(feed.ID, []store.CalendarEvent{{UID: "e1", Title: "Standup", Start: now, End: now.Add(time.Hour)}})

	if rec := do(t, s, "DELETE", "/api/connectors/calendar", ""); rec.Code != 200 {
		t.Fatalf("disconnect status %d", rec.Code)
	}
	if s.calendarStatus() != "not configured" {
		t.Fatal("the calendar must show as not configured")
	}
	if evs, _ := s.store.EventsInWindow(now.Add(-time.Hour), now.Add(2*time.Hour)); len(evs) != 0 {
		t.Fatalf("calendar events must go with the connection: %+v", evs)
	}
}

func TestConnectJiraSaysWhichPartFailed(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	for err, want := range map[error]string{jira.ErrNotJira: "not a Jira site", jira.ErrRefused: "Jira refused the email and token"} {
		s.jiraValidate = func(context.Context, string, string, string) error { return err }
		rec := do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":"t"}`)
		if rec.Code == 200 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%v: want %q, got %d %s", err, want, rec.Code, rec.Body.String())
		}
	}
}

func TestConnectJiraRejectsMissingField(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	if rec := do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":""}`); rec.Code != 400 {
		t.Fatalf("missing token -> 400, got %d", rec.Code)
	}
}

func TestConnectJiraRejectsBadCredentials(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	s.jiraValidate = func(context.Context, string, string, string) error { return errors.New("status 401") }
	rec := do(t, s, "POST", "/api/connectors/jira", `{"site":"example.atlassian.net","email":"me@example.com","apiToken":"jira_bad_token"}`)
	if rec.Code == 200 {
		t.Fatal("credentials that fail verification must not connect")
	}
	if _, ok, _ := s.store.GetSetting("jira_api_token"); ok {
		t.Fatal("a token that fails verification must not be stored")
	}
	if strings.Contains(rec.Body.String(), "jira_bad_token") {
		t.Fatal("the error must not echo the token")
	}
}

func TestGitHubStatusComesFromGh(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), nil, nil)
	state, synced := github.StateLoggedOut, 0
	s.ghStatus = func(context.Context) (string, string) {
		if state == github.StateConnected {
			return state, "octocat"
		}
		return state, ""
	}
	s.syncGitHub = func(context.Context) (int, error) { synced++; return 0, nil }
	ghConn := func(rec *httptest.ResponseRecorder) connectorStatus {
		t.Helper()
		var conns []connectorStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &conns); err != nil {
			t.Fatal(err)
		}
		for _, c := range conns {
			if c.Name == "GitHub" {
				return c
			}
		}
		t.Fatal("no GitHub connector")
		return connectorStatus{}
	}

	if c := ghConn(do(t, s, "GET", "/api/connectors", "")); c.Status != "not logged in" || c.Account != "" {
		t.Fatalf("before login: %+v", c)
	}
	// the user logs in to gh; the cached answer stays until Check again
	state = "connected"
	if c := ghConn(do(t, s, "GET", "/api/connectors", "")); c.Status != "not logged in" {
		t.Fatalf("the status is cached until a check: %+v", c)
	}
	rec := do(t, s, "POST", "/api/connectors/github/check", "")
	var checked connectorStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &checked)
	if rec.Code != 200 || checked.Status != "connected" || checked.Account != "octocat" {
		t.Fatalf("check: %d %+v", rec.Code, checked)
	}
	if synced != 1 {
		t.Fatalf("a check that finds gh connected syncs at once, synced %d", synced)
	}
	// connected with nothing synced is still connected
	if c := ghConn(do(t, s, "GET", "/api/connectors", "")); c.Status != "connected" || c.Account != "octocat" {
		t.Fatalf("after check: %+v", c)
	}
}

func TestCalendarConnectorSaysCalendarsPlainly(t *testing.T) {
	s := newSeededServer(t, time.Now(), nil, nil)
	var all []connectorStatus
	if err := json.Unmarshal(get(t, s, "/api/connectors").Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.Name == "Calendar" {
			if strings.Contains(strings.ToLower(c.Detail), "maker") || !strings.HasPrefix(c.Detail, "Calendars") {
				t.Fatalf("detail: %q", c.Detail)
			}
			return
		}
	}
	t.Fatalf("no Calendar in %+v", all)
}
