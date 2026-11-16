package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/update"
)

func updateStatus(t *testing.T, s *Server, method, path, body string) update.Status {
	t.Helper()
	rec := do(t, s, method, path, body)
	if rec.Code != 200 {
		t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
	}
	var st update.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestUpdateUnsupportedByDefault(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), nil, nil)
	st := updateStatus(t, s, "GET", "/api/update", "")
	if st.Supported || st.Available || st.Current != "dev" || !st.Enabled {
		t.Fatalf("default status %+v", st)
	}
	// a forced check in a dev build does nothing, and says so
	if st := updateStatus(t, s, "POST", "/api/update/check", ""); st.Supported || st.Available {
		t.Fatalf("check in a dev build %+v", st)
	}
}

func TestUpdateCheckAndToggle(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), nil, nil)
	calls := 0
	c := update.NewChecker("v1.0.0", "example/tool", s.updateEnabled)
	c.Fetch = func(context.Context, string) (update.Release, error) {
		calls++
		return update.Release{Tag: "v1.2.0", URL: "https://example.invalid/rel", Notes: "notes"}, nil
	}
	c.Install = update.Install{Method: "homebrew", Name: "tool", Hint: "brew upgrade tool"}
	s.SetUpdate(c)

	st := updateStatus(t, s, "GET", "/api/update", "")
	if !st.Supported || st.Available || calls != 0 {
		t.Fatalf("GET must not fetch: %+v calls=%d", st, calls)
	}
	st = updateStatus(t, s, "POST", "/api/update/check", "")
	if !st.Available || st.Latest != "v1.2.0" || st.URL != "https://example.invalid/rel" || st.Hint != "brew upgrade tool" || st.Install != "homebrew" || calls != 1 {
		t.Fatalf("after check %+v calls=%d", st, calls)
	}
	if st := updateStatus(t, s, "GET", "/api/update", ""); !st.Available {
		t.Fatalf("result not kept: %+v", st)
	}

	st = updateStatus(t, s, "PUT", "/api/update/enabled", `{"enabled":false}`)
	if st.Enabled {
		t.Fatalf("still enabled: %+v", st)
	}
	if v, _, _ := s.store.GetSetting(update.SettingKey); v != "off" {
		t.Fatalf("setting %q", v)
	}
	if c.Enabled() {
		t.Fatal("the checker must read the stored setting")
	}
	if st := updateStatus(t, s, "PUT", "/api/update/enabled", `{"enabled":true}`); !st.Enabled {
		t.Fatalf("not re-enabled: %+v", st)
	}
	if rec := do(t, s, "PUT", "/api/update/enabled", `nope`); rec.Code != 400 {
		t.Fatalf("bad JSON -> 400, got %d", rec.Code)
	}
}
