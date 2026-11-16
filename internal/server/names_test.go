package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

func TestRenameSessionShowsOnItsCard(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{
		sess("run", facts.TailAIWorking, tnow.Add(-30*time.Second), tnow.Add(-time.Hour), tnow.Add(-30*time.Second), 60),
	}, nil)
	if rec := do(t, s, "PATCH", "/api/sessions/run", `{"name":"  Bedrock fallback  "}`); rec.Code != 200 {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	var v TodayView
	_ = json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v)
	if len(v.Running.Cards) != 1 || v.Running.Cards[0].Name != "Bedrock fallback" {
		t.Fatalf("want the trimmed name on the card: %+v", v.Running.Cards)
	}
}

func TestRenameSessionRejectsUnknownAndOverlong(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{
		sess("run", facts.TailAIWorking, tnow, tnow, tnow, 60),
	}, nil)
	if rec := do(t, s, "PATCH", "/api/sessions/nope", `{"name":"x"}`); rec.Code != 404 {
		t.Fatalf("unknown session: want 404, got %d", rec.Code)
	}
	if rec := do(t, s, "PATCH", "/api/sessions/run", `{"name":"`+strings.Repeat("a", 121)+`"}`); rec.Code != 400 {
		t.Fatalf("a name over 120 characters: want 400, got %d", rec.Code)
	}
}
