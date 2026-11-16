package usage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const fakeToken = "test-access-token"

var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func fakeResp(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

// writeCreds writes a fake Claude Code credentials file expiring at exp.
func writeCreds(t *testing.T, token string, exp time.Time) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".credentials.json")
	b, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken": token, "refreshToken": "test-refresh-token", "expiresAt": exp.UnixMilli(),
	}})
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestClient(path string, do func(*http.Request) (*http.Response, error)) *Client {
	return &Client{Endpoint: endpoint, CredentialsPath: path, httpDo: do, clock: func() time.Time { return now }}
}

const okBody = `{
  "five_hour": {"utilization": 1.0, "resets_at": "2026-10-29T16:40:00.123+00:00"},
  "seven_day": {"utilization": 12, "resets_at": "2026-11-02T15:00:00+00:00"},
  "seven_day_opus": null,
  "seven_day_oauth_apps": {"utilization": 3, "resets_at": null},
  "limits": [{"kind": "weekly_scoped", "percent": 0, "resets_at": 1793030400, "scope": {"model": {"display_name": "Fable"}}}]
}`

func TestGetParsesWindows(t *testing.T) {
	var gotAuth, gotBeta, gotURL string
	c := newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), func(r *http.Request) (*http.Response, error) {
		gotAuth, gotBeta, gotURL = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta"), r.URL.String()
		return fakeResp(200, okBody), nil
	})
	res := c.Get(context.Background())
	if res.Status != "" {
		t.Fatalf("status: %q", res.Status)
	}
	if gotAuth != "Bearer "+fakeToken || gotBeta != "oauth-2025-04-20" || gotURL != "https://api.anthropic.com/api/oauth/usage" {
		t.Fatalf("request: auth %q beta %q url %q", gotAuth, gotBeta, gotURL)
	}
	want := []Window{
		{Label: "5h", Percent: 1, ResetsAt: time.Date(2026, 10, 29, 16, 40, 0, 123e6, time.UTC)},
		{Label: "week", Percent: 12, ResetsAt: time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)},
		{Label: "Fable", Percent: 0, ResetsAt: time.Unix(1793030400, 0)},
	}
	if len(res.Windows) != len(want) {
		t.Fatalf("windows: %+v", res.Windows)
	}
	for i, w := range want {
		g := res.Windows[i]
		if g.Label != w.Label || g.Percent != w.Percent || !g.ResetsAt.Equal(w.ResetsAt) {
			t.Fatalf("window %d: got %+v want %+v", i, g, w)
		}
	}
}

func TestGetReadsPerModelWeekKeys(t *testing.T) {
	c := newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), func(r *http.Request) (*http.Response, error) {
		return fakeResp(200, `{"seven_day": {"utilization": 5, "resets_at": null}, "seven_day_sonnet": {"utilization": 40, "resets_at": null}}`), nil
	})
	res := c.Get(context.Background())
	if len(res.Windows) != 2 || res.Windows[1].Label != "Sonnet" || res.Windows[1].Percent != 40 {
		t.Fatalf("windows: %+v", res.Windows)
	}
}

func TestGetWithoutCredentials(t *testing.T) {
	called := false
	c := newTestClient(filepath.Join(t.TempDir(), "missing.json"), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})
	res := c.Get(context.Background())
	if res.Status != StatusSignedOut || called || len(res.Windows) != 0 {
		t.Fatalf("got %+v, called %v", res, called)
	}
}

func TestGetExpiredTokenSkipsTheCall(t *testing.T) {
	called := false
	c := newTestClient(writeCreds(t, fakeToken, now.Add(-time.Minute)), func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})
	res := c.Get(context.Background())
	if res.Status != StatusExpired || called {
		t.Fatalf("got %+v, called %v", res, called)
	}
}

func TestGetErrorsNeverCarryTheToken(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{401, StatusExpired}, {429, StatusUnavailable}, {500, StatusUnavailable}} {
		c := newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), func(r *http.Request) (*http.Response, error) {
			return fakeResp(tc.code, `{"error":"bad token `+fakeToken+`"}`), nil
		})
		res := c.Get(context.Background())
		b, _ := json.Marshal(res)
		if res.Status != tc.want || strings.Contains(string(b), fakeToken) {
			t.Fatalf("%d: got %s", tc.code, b)
		}
	}
}

// On macOS Claude Code keeps its credentials in the login Keychain, read
// through security; the file is the fallback. The keychain's token is never in
// the result either.
func TestGetReadsTheKeychainElseTheFile(t *testing.T) {
	const keychainToken = "test-keychain-token"
	var gotAuth []string
	do := func(r *http.Request) (*http.Response, error) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		return fakeResp(500, `{"error":"bad token `+keychainToken+`"}`), nil
	}
	var argv []string
	c := newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), do)
	c.run = func(_ context.Context, bin string, args []string) ([]byte, error) {
		argv = append([]string{bin}, args...)
		b, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{"accessToken": keychainToken, "expiresAt": now.Add(time.Hour).UnixMilli()}})
		return append(b, '\n'), nil
	}
	c.Keychain = "Claude Code-credentials"
	res := c.Get(context.Background())
	b, _ := json.Marshal(res)
	if strings.Join(argv, "|") != "security|find-generic-password|-s|Claude Code-credentials|-w" || strings.Contains(string(b), keychainToken) {
		t.Fatalf("argv %q, result %s", argv, b)
	}

	// no keychain item: the file
	c = newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), do)
	c.run = func(context.Context, string, []string) ([]byte, error) { return nil, errors.New("exit status 44") }
	c.Keychain = "Claude Code-credentials"
	c.Get(context.Background())
	if strings.Join(gotAuth, "|") != "Bearer "+keychainToken+"|Bearer "+fakeToken {
		t.Fatalf("auth: %q", gotAuth)
	}
}

func TestGetCachesForFiveMinutes(t *testing.T) {
	calls := 0
	c := newTestClient(writeCreds(t, fakeToken, now.Add(time.Hour)), func(r *http.Request) (*http.Response, error) {
		calls++
		return fakeResp(429, ``), nil
	})
	at := now
	c.clock = func() time.Time { return at }
	c.Get(context.Background())
	at = at.Add(4 * time.Minute)
	c.Get(context.Background())
	if calls != 1 {
		t.Fatalf("calls inside 5 minutes: %d", calls)
	}
	at = at.Add(2 * time.Minute)
	c.Get(context.Background())
	if calls != 2 {
		t.Fatalf("calls after 5 minutes: %d", calls)
	}
}
