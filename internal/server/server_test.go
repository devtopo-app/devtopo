package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/usage"
)

func newSeededServer(t *testing.T, tnow time.Time, sessions []store.Session, factsFor map[string][]facts.Fact) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, se := range sessions {
		if err := st.Apply(store.Delta{
			Project:    store.Project{Key: se.ProjectKey, Name: se.ProjectKey},
			Session:    se,
			Facts:      factsFor[se.ID],
			CursorPath: se.ID, CursorOffset: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	s := New(st, nil)
	s.clock = func() time.Time { return tnow }
	// no session is live and no app is opened unless a test says so
	s.snapshot = func() ([]host.Proc, error) { return nil, nil }
	s.sessionsDir = t.TempDir()
	s.opener = &host.Opener{DesktopDir: t.TempDir()}
	s.codexUsage = func() []usage.Window { return nil }
	s.codexTitles = func() map[string]string { return nil }
	// tests never run the user's gh
	s.ghStatus = func(context.Context) (string, string) { return github.StateLoggedOut, "" }
	s.syncGitHub = func(context.Context) (int, error) { return 0, nil }
	return s
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = testHost
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandlerRendersLanes(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("run", facts.TailAIWorking, tnow.Add(-30*time.Second), tnow.Add(-time.Hour), tnow.Add(-30*time.Second), 3600),
		sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 1800),
		sess("idle", facts.TailAIWorking, tnow.Add(-30*time.Minute), tnow.Add(-time.Hour), tnow.Add(-30*time.Minute), 600),
		sess("done", facts.TailAIDone, tnow.Add(-5*time.Hour), tnow.Add(-6*time.Hour), tnow.Add(-5*time.Hour), 900),
	}
	s := newSeededServer(t, tnow, ss, map[string][]facts.Fact{
		"run": {{Type: "prompt"}, {Type: "prompt"}},
	})
	rec := get(t, s, "/legacy")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"Running", "Waiting on you", "Idle", "github.com/x/run", `http-equiv="refresh"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
	if strings.Contains(body, "Done today") {
		t.Fatal("there is no Done lane")
	}
}

func TestHandlerEscapesSessionText(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := sess("x", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 100)
	s.Branch = "<script>x</script>"
	srv := newSeededServer(t, tnow, []store.Session{s}, nil)
	rec := get(t, srv, "/legacy")
	body := rec.Body.String()
	if strings.Contains(body, "<script>x</script>") {
		t.Fatal("raw script tag from session data must be escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("expected the escaped form of the branch name")
	}
}

func TestHandlerEmptyStoreOK(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	srv := newSeededServer(t, tnow, nil, nil)
	rec := get(t, srv, "/legacy")
	if rec.Code != 200 {
		t.Fatalf("empty store status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Nothing") {
		t.Fatalf("empty state should be calm: %s", rec.Body.String())
	}
}

func TestHandlerFixedClock(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("run", facts.TailAIWorking, tnow.Add(-time.Minute), tnow.Add(-time.Hour), tnow.Add(-time.Minute), 100),
	}
	srv := newSeededServer(t, tnow, ss, nil)
	body := get(t, srv, "/legacy").Body.String()
	if !strings.Contains(body, "1 running") {
		t.Fatalf("a 1-minute-old event should read as running under the fixed clock: %s", body)
	}
}

func TestHandlerShowsWallClock(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	// each card shows a wall-h figure, here 0 since the session has no day rows
	s := sess("x", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-2*time.Hour), tnow.Add(-10*time.Minute), 1800)
	srv := newSeededServer(t, tnow, []store.Session{s}, nil)
	body := get(t, srv, "/legacy").Body.String()
	if !strings.Contains(body, "wall-h") {
		t.Fatalf("each card should show wall-clock alongside AI time: %s", body)
	}
}

func TestHandlerNotFound(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	srv := newSeededServer(t, tnow, nil, nil)
	// Unknown page routes fall back to the SPA shell now; an unknown API path
	// is the one that must still 404.
	if rec := get(t, srv, "/api/nope"); rec.Code != 404 {
		t.Fatalf("unknown /api path should 404, got %d", rec.Code)
	}
}

func TestLegacyShowsTodaysPoints(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{
		sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 100),
	}, nil)
	if err := s.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("wait", tnow), Structured: `["fixed the flaky test"]`}); err != nil {
		t.Fatal(err)
	}
	if body := get(t, s, "/legacy").Body.String(); !strings.Contains(body, "<li>fixed the flaky test</li>") {
		t.Fatalf("legacy page should list today's points: %s", body)
	}
}
