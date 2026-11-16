package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeNarrator struct {
	text  string
	err   error
	calls int
}

func (f *fakeNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	f.calls++
	return f.text, f.err
}

func post(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, path, nil)
	r.Host = testHost
	s.Handler().ServeHTTP(rec, r)
	return rec
}

// detailServer seeds one waiting session "x" whose transcript has a turn on
// tnow's day, and gives the server n as its narrator.
func detailServer(t *testing.T, lastAct time.Time, n *fakeNarrator) *Server {
	t.Helper()
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	tp := filepath.Join(t.TempDir(), "x.jsonl")
	line := `{"type":"user","timestamp":"2026-10-27T11:00:00Z","message":{"role":"user","content":"fix the bug"}}` + "\n"
	if err := os.WriteFile(tp, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	se := sess("x", facts.TailAIDone, lastAct, lastAct.Add(-time.Hour), lastAct, 100)
	se.TranscriptPath = tp
	s := newSeededServer(t, tnow, []store.Session{se}, nil)
	if n != nil {
		s.narrator = n
	}
	return s
}

func TestDetailReturnsTheSummary(t *testing.T) {
	n := &fakeNarrator{text: "What was done\n- fixed the bug"}
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), n)
	rec := post(t, s, "/api/sessions/x/detail")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Text string
		AsOf time.Time
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != n.text || got.AsOf.IsZero() {
		t.Fatalf("detail: %+v", got)
	}
}

func TestDetailNoActivityToday(t *testing.T) {
	n := &fakeNarrator{text: "x"}
	s := detailServer(t, time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC), n)
	if rec := post(t, s, "/api/sessions/x/detail"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no activity today") {
		t.Fatalf("no activity today: %d %s", rec.Code, rec.Body)
	}
}

func TestDetailUnknownSession(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), &fakeNarrator{})
	if rec := post(t, s, "/api/sessions/nope/detail"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", rec.Code)
	}
}

func TestDetailWithoutNarrator(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), nil)
	if rec := post(t, s, "/api/sessions/x/detail"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no narrator: %d", rec.Code)
	}
}

func TestDetailWithNoToolAvailable(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), nil)
	(&fakeTools{}).attach(t, s)
	rec := post(t, s, "/api/sessions/x/detail")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "neither claude nor codex") {
		t.Fatalf("no tool: %d %s", rec.Code, rec.Body)
	}
}

func TestRefreshPointsReturnsThePoints(t *testing.T) {
	last := time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC)
	s := detailServer(t, last, &fakeNarrator{text: "- fixed the bug\n- wrote a test"})
	rec := post(t, s, "/api/sessions/x/points")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Points []string
		AsOf   time.Time
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Points, "|") != "fixed the bug|wrote a test" || !got.AsOf.Equal(last) {
		t.Fatalf("points: %+v", got)
	}
}

func TestRefreshPointsNoActivityToday(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC), &fakeNarrator{text: "- x"})
	if rec := post(t, s, "/api/sessions/x/points"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no activity today") {
		t.Fatalf("no activity today: %d %s", rec.Code, rec.Body)
	}
}

func TestRefreshPointsUnknownSession(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), &fakeNarrator{})
	if rec := post(t, s, "/api/sessions/nope/points"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown session: %d", rec.Code)
	}
}

func TestRefreshPointsWithNoToolAvailable(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), nil)
	(&fakeTools{}).attach(t, s)
	rec := post(t, s, "/api/sessions/x/points")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "neither claude nor codex") {
		t.Fatalf("no tool: %d %s", rec.Code, rec.Body)
	}
}

func TestRefreshPointsNarratorFailure(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), &fakeNarrator{err: errors.New("claude: logged out")})
	if rec := post(t, s, "/api/sessions/x/points"); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "logged out") {
		t.Fatalf("narrator failure: %d %s", rec.Code, rec.Body)
	}
}

func TestHistoryNotSummarizingWithNoTool(t *testing.T) {
	s := historyServer(t)
	(&fakeTools{}).attach(t, s)
	var d HistoryDay
	json.Unmarshal(get(t, s, "/api/history/day?d=2026-10-21").Body.Bytes(), &d)
	if d.Sessions[0].Summarizing {
		t.Fatalf("nothing can summarize it: %+v", d.Sessions[0])
	}
	rec := post(t, s, "/api/history/points?d=2026-10-21&session=a")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("points with no tool: %d %s", rec.Code, rec.Body)
	}
}

func TestDetailNarratorFailure(t *testing.T) {
	s := detailServer(t, time.Date(2026, 10, 27, 11, 50, 0, 0, time.UTC), &fakeNarrator{err: errors.New("claude: logged out")})
	if rec := post(t, s, "/api/sessions/x/detail"); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "logged out") {
		t.Fatalf("narrator failure: %d %s", rec.Code, rec.Body)
	}
}
