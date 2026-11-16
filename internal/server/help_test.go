package server

import (
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/launch"
)

// A second start asks /api/ping to tell a running DevTopo from another program.
func TestPingSaysDevTopo(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), nil, nil)
	if rec := get(t, s, "/api/ping"); rec.Code != 200 || rec.Body.String() != launch.PingReply {
		t.Fatalf("ping: %d %q", rec.Code, rec.Body.String())
	}
}

// The Help page shows the guide the app was built with (GUIDE.md), so the
// guide on GitHub and in the app are the same text.
func TestHelpServesTheGuide(t *testing.T) {
	s := newSeededServer(t, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC), nil, nil)
	rec := get(t, s, "/api/help")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.HasPrefix(body, "# DevTopo guide") || !strings.Contains(body, "Create API token with scopes") {
		t.Fatalf("help must be the guide:\n%.300s", body)
	}
}
