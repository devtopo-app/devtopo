package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/ask"
	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
)

// askNarr is a narrator that records its prompts and answers with fn.
type askNarr struct {
	mu      sync.Mutex
	prompts []string
	fn      func(ctx context.Context, prompt string) (string, error)
}

func (a *askNarr) Narrate(ctx context.Context, prompt string) (string, error) {
	a.mu.Lock()
	a.prompts = append(a.prompts, prompt)
	a.mu.Unlock()
	return a.fn(ctx, prompt)
}

func (a *askNarr) seen() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.prompts...)
}

const askAnswer = `{"answer":"You fixed the bug.","sources":[{"session":"S1","time":"10:00","why":"the fix"}],"read":[]}`

func answering(text string) *askNarr {
	return &askNarr{fn: func(context.Context, string) (string, error) { return text, nil }}
}

// askNow is just after midnight, so a session waiting since last evening still
// sits in the Waiting lane though it is not today's.
var askNow = time.Date(2026, 10, 27, 0, 30, 0, 0, time.UTC)

// askServer seeds a waiting session "today" (branch "today-branch") and a
// waiting one whose last activity was yesterday ("old-branch"), and wires n in
// as the only model, "fake:m".
func askServer(t *testing.T, n summary.Narrator) *Server {
	t.Helper()
	today := sess("today", facts.TailAIDone, askNow.Add(-10*time.Minute), askNow.Add(-20*time.Minute), askNow.Add(-10*time.Minute), 100)
	today.Branch = "today-branch"
	old := sess("old", facts.TailAIDone, askNow.Add(-40*time.Minute), askNow.Add(-3*time.Hour), askNow.Add(-40*time.Minute), 100)
	old.Branch = "old-branch"
	s := newSeededServer(t, askNow, []store.Session{today, old}, nil)
	s.askModels = func() []summary.Model { return []summary.Model{{ID: "fake:m", Tool: "fake", Label: "Fake"}} }
	s.askNarrator = func(id string) (summary.Narrator, bool) { return n, id == "fake:m" && n != nil }
	return s
}

func askBody(q string) []byte {
	b, _ := json.Marshal(map[string]any{"ModelID": "fake:m", "Question": q})
	return b
}

func postAsk(s *Server, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/ask", bytes.NewReader(body))
	r.Host = testHost
	s.Handler().ServeHTTP(rec, r)
	return rec
}

func events(t *testing.T, body string) []ask.Event {
	t.Helper()
	var out []ask.Event
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		var e ask.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %q is not an event: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func TestAskModelsListsTheTools(t *testing.T) {
	s := askServer(t, nil)
	(&fakeTools{claude: true, codex: true}).attach(t, s)
	s.askModels, s.askNarrator = s.defaultAskModels, s.defaultAskNarrator
	rec := get(t, s, "/api/ask/models")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Models []summary.Model
		Off    bool
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	n := len(got.Models)
	if got.Off || n < 2 || got.Models[0].ID != "claude:haiku" || !got.Models[0].Default || got.Models[n-1].Tool != "codex" {
		t.Fatalf("models: %+v", got)
	}
}

// The chat gets its models from the Selector the daemon runs with, not a test
// double, and a model the Selector doesn't list is refused before anything runs.
func TestAskUsesTheSelectorsModels(t *testing.T) {
	s := askServer(t, nil)
	(&fakeTools{claude: true}).attach(t, s)
	s.askModels, s.askNarrator = s.defaultAskModels, s.defaultAskNarrator
	if _, ok := s.askNarrator("claude:haiku"); !ok {
		t.Fatal("a listed model must be accepted")
	}
	if _, ok := s.askNarrator("codex:gpt-6-luna"); ok {
		t.Fatal("a tool that is not available must be refused")
	}
	if rec := postAsk(s, []byte(`{"ModelID":"claude:--flag","Question":"q"}`)); rec.Code != 400 {
		t.Fatalf("status %d, want an unknown model refused", rec.Code)
	}
}

func TestAskWithoutASelectorOffersNothing(t *testing.T) {
	s := askServer(t, nil)
	s.askModels, s.askNarrator = s.defaultAskModels, s.defaultAskNarrator
	if rec := get(t, s, "/api/ask/models"); !strings.Contains(rec.Body.String(), `"Models":[]`) {
		t.Fatalf("models: %s", rec.Body)
	}
	if rec := postAsk(s, askBody("q")); rec.Code != 503 {
		t.Fatalf("status %d, want questions off", rec.Code)
	}
}

func TestAskModelsSaysWhenSwitchedOff(t *testing.T) {
	s := askServer(t, nil)
	s.SetNarrativeOff(true)
	rec := get(t, s, "/api/ask/models")
	if !strings.Contains(rec.Body.String(), `"Models":[]`) || !strings.Contains(rec.Body.String(), `"Off":true`) {
		t.Fatalf("off: %s", rec.Body)
	}
	// nothing is available without a router either: still an array, not null
	s2 := askServer(t, nil)
	s2.askModels = nil
	s2.askModels = s2.defaultAskModels
	if rec := get(t, s2, "/api/ask/models"); !strings.Contains(rec.Body.String(), `"Models":[]`) {
		t.Fatalf("no router: %s", rec.Body)
	}
}

func TestAskRefusesAnEmptyQuestion(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	for _, q := range []string{"", "  \n\t "} {
		if rec := postAsk(s, askBody(q)); rec.Code != http.StatusBadRequest {
			t.Fatalf("question %q: %d", q, rec.Code)
		}
	}
	if rec := postAsk(s, []byte("not json")); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rec.Code)
	}
	if len(n.seen()) != 0 {
		t.Fatalf("the model was called: %v", n.seen())
	}
}

func TestAskRefusesAnUnknownModel(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	body, _ := json.Marshal(map[string]any{"ModelID": "nope:x", "Question": "what?"})
	if rec := postAsk(s, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown model: %d %s", rec.Code, rec.Body)
	}
	if len(n.seen()) != 0 {
		t.Fatal("the model was called")
	}
}

func TestAskRefusesAnOversizedBody(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	big := askBody(strings.Repeat("x", 2<<20+10))
	if rec := postAsk(s, big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized: %d", rec.Code)
	}
	if len(n.seen()) != 0 {
		t.Fatal("the model was called")
	}
}

func TestAskWithoutANarratorIs503(t *testing.T) {
	s := askServer(t, nil)
	s.askModels = func() []summary.Model { return nil }
	if rec := postAsk(s, askBody("what?")); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no narrator: %d", rec.Code)
	}
}

func TestAskStreamsEventsAndEndsWithDone(t *testing.T) {
	s := askServer(t, answering(askAnswer))
	rec := postAsk(s, askBody("what did I do?"))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	evs := events(t, rec.Body.String())
	if len(evs) < 2 || evs[0].Type != "checking" {
		t.Fatalf("events: %+v", evs)
	}
	last := evs[len(evs)-1]
	if last.Type != "done" || last.Result == nil || last.Result.Answer != "You fixed the bug." || !last.Result.FromSummaries {
		t.Fatalf("last event: %+v", last)
	}
	if last.Result.Sources[0].SessionID != "today" {
		t.Fatalf("source: %+v", last.Result.Sources)
	}
}

func TestAskOnlyUsesSessionsActiveToday(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	if v, err := s.todayView(); err != nil || len(v.Waiting.Cards) != 2 {
		t.Fatalf("precondition: both sessions wait: %+v %v", v.Waiting, err)
	}
	postAsk(s, askBody("what?"))
	p := n.seen()
	if len(p) == 0 || !strings.Contains(p[0], "today-branch") || strings.Contains(p[0], "old-branch") {
		t.Fatalf("check prompt: %v", p)
	}
}

func TestAskUsesFullSummaryNotTheTenPointCard(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	var points []string
	for i := 1; i <= 15; i++ {
		points = append(points, "point number "+string(rune('A'+i-1)))
	}
	raw, _ := json.Marshal(points)
	asOf := askNow.Add(-10 * time.Minute)
	if err := s.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("today", askNow), AsOf: asOf, Structured: string(raw)}); err != nil {
		t.Fatal(err)
	}
	postAsk(s, askBody("what?"))
	p := n.seen()[0]
	for _, pt := range points {
		if !strings.Contains(p, pt) {
			t.Fatalf("%q missing from the prompt:\n%s", pt, p)
		}
	}
	if strings.Contains(p, "newer activity") || strings.Contains(p, "no summary yet") {
		t.Fatalf("a current summary was marked: %s", p)
	}
}

func TestAskMarksStaleAndMissingSummaries(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	// a second session today with no summary at all
	extra := sess("fresh", facts.TailAIDone, askNow.Add(-5*time.Minute), askNow.Add(-20*time.Minute), askNow.Add(-5*time.Minute), 100)
	extra.Branch = "fresh-branch"
	if err := s.store.Apply(store.Delta{Project: store.Project{Key: extra.ProjectKey, Name: extra.ProjectKey}, Session: extra, CursorPath: "fresh", CursorOffset: 1}); err != nil {
		t.Fatal(err)
	}
	// "today" has a summary from before its latest activity
	if err := s.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("today", askNow), AsOf: askNow.Add(-15 * time.Minute), Structured: `["early work"]`}); err != nil {
		t.Fatal(err)
	}
	// and one whose stored summary is empty ("null"), which counts as missing
	empty := sess("empty", facts.TailAIDone, askNow.Add(-4*time.Minute), askNow.Add(-20*time.Minute), askNow.Add(-4*time.Minute), 100)
	empty.Branch = "empty-branch"
	if err := s.store.Apply(store.Delta{Project: store.Project{Key: empty.ProjectKey, Name: empty.ProjectKey}, Session: empty, CursorPath: "empty", CursorOffset: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("empty", askNow), AsOf: askNow, Structured: "null"}); err != nil {
		t.Fatal(err)
	}
	postAsk(s, askBody("what?"))
	p := n.seen()[0]
	if !strings.Contains(p, "empty-branch (claude) - no summary yet") {
		t.Fatalf("empty summary not marked:\n%s", p)
	}
	if !strings.Contains(p, "today-branch (claude) - newer activity than this summary") {
		t.Fatalf("stale not marked:\n%s", p)
	}
	if !strings.Contains(p, "fresh-branch (claude) - no summary yet") {
		t.Fatalf("missing not marked:\n%s", p)
	}
}

func TestAskSecondRunGets409(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	n := &askNarr{fn: func(ctx context.Context, _ string) (string, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
		}
		return askAnswer, nil
	}}
	s := askServer(t, n)
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- postAsk(s, askBody("one")) }()
	<-started
	rec := postAsk(s, askBody("two"))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Another question is still running.") {
		t.Fatalf("second run: %d %s", rec.Code, rec.Body)
	}
	close(release)
	if r := <-first; r.Code != 200 {
		t.Fatalf("first run: %d", r.Code)
	}
	if rec := postAsk(s, askBody("three")); rec.Code != 200 {
		t.Fatalf("after the first ended: %d", rec.Code)
	}
}

func TestAskClosedConnectionCancelsTheModelCalls(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	var once sync.Once
	calls := 0
	n := &askNarr{fn: func(ctx context.Context, _ string) (string, error) {
		calls++
		if calls > 1 {
			return askAnswer, nil
		}
		close(started)
		<-ctx.Done()
		once.Do(func() { close(cancelled) })
		return "", ctx.Err()
	}}
	s := askServer(t, n)
	h := s.Handler()
	handled := make(chan struct{}, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		handled <- struct{}{}
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/ask", bytes.NewReader(askBody("slow")))
	errc := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		errc <- err
	}()
	timeout := time.After(10 * time.Second)
	select {
	case <-started:
	case <-timeout:
		t.Fatal("the model was never called")
	}
	cancel()
	select {
	case <-cancelled:
	case <-timeout:
		t.Fatal("the model call was not cancelled")
	}
	<-errc
	select {
	case <-handled: // the handler has returned, so the run lock is free
	case <-timeout:
		t.Fatal("the handler did not return")
	}
	resp, err := http.Post(ts.URL+"/api/ask", "application/json", bytes.NewReader(askBody("again")))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("a new ask after the cancel: %d", resp.StatusCode)
	}
}

func TestAskWritesNothingToTheStore(t *testing.T) {
	s := askServer(t, answering(askAnswer))
	count := func() (int, int) {
		sums, err := s.store.ListSummaries(summary.LevelDay)
		if err != nil {
			t.Fatal(err)
		}
		ss, err := s.store.ListSessions()
		if err != nil {
			t.Fatal(err)
		}
		return len(sums), len(ss)
	}
	a1, b1 := count()
	postAsk(s, askBody("what?"))
	if a2, b2 := count(); a1 != a2 || b1 != b2 {
		t.Fatalf("store changed: summaries %d->%d sessions %d->%d", a1, a2, b1, b2)
	}
}

func TestAskIsSameOriginProtected(t *testing.T) {
	n := answering(askAnswer)
	s := askServer(t, n)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/ask", bytes.NewReader(askBody("what?")))
	r.Host = testHost
	r.Header.Set("Origin", "http://evil.example")
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden || len(n.seen()) != 0 {
		t.Fatalf("cross-origin: %d", rec.Code)
	}
}
