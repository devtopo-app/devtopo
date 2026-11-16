package server

import (
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestSPAServesIndexAtRoot(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	rec := get(t, s, "/")
	if rec.Code != 200 {
		t.Fatalf("root status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("root should serve the SPA shell: %s", rec.Body.String())
	}
}

func TestSPAFallbackForClientRoute(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	rec := get(t, s, "/planner")
	if rec.Code != 200 {
		t.Fatalf("client route status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatalf("an unknown client route should fall back to the SPA shell: %s", rec.Body.String())
	}
}

func TestSPADoesNotShadowAPI(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	rec := get(t, s, "/api/does-not-exist")
	if rec.Code != 404 {
		t.Fatalf("an unknown /api path must 404, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `id="root"`) {
		t.Fatal("an unknown /api path must not fall back to the SPA shell")
	}
}

// the legacy server-rendered page moves to /legacy and still works
func TestLegacyPageStillRenders(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("run", "ai_working", tnow.Add(-30*time.Second), tnow.Add(-time.Hour), tnow.Add(-30*time.Second), 100),
	}
	s := newSeededServer(t, tnow, ss, nil)
	rec := get(t, s, "/legacy")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Running") {
		t.Fatalf("/legacy should render the HTML page: %d %s", rec.Code, rec.Body.String())
	}
}
