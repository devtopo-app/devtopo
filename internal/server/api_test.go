package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
)

func TestAPITodayReturnsJSON(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("run", facts.TailAIWorking, tnow.Add(-30*time.Second), tnow.Add(-time.Hour), tnow.Add(-30*time.Second), 3600),
		sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 1800),
	}
	s := newSeededServer(t, tnow, ss, nil)
	rec := get(t, s, "/api/today")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type %q", ct)
	}
	var v TodayView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(v.Running.Cards) != 1 || v.Running.Cards[0].Project == "" {
		t.Fatalf("running lane in json: %+v", v.Running)
	}
}

func TestAPIStatusCountsLiveLanes(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("run", facts.TailAIWorking, tnow.Add(-30*time.Second), tnow.Add(-time.Hour), tnow.Add(-30*time.Second), 100),
		sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 100),
	}
	s := newSeededServer(t, tnow, ss, nil)
	rec := get(t, s, "/api/status")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var st struct {
		Daemon         string `json:"daemon"`
		ActiveSessions int    `json:"activeSessions"`
		Waiting        int    `json:"waiting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.Daemon != "live" || st.ActiveSessions != 1 || st.Waiting != 1 {
		t.Fatalf("status counts: %+v", st)
	}
	// The cheap status agrees with the full Today view it no longer builds.
	view, err := s.todayView()
	if err != nil {
		t.Fatal(err)
	}
	var full struct {
		DayAIHours float64 `json:"dayAIHours"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &full)
	if len(view.Running.Cards) != st.ActiveSessions || len(view.Waiting.Cards) != st.Waiting || view.DayAIHours != full.DayAIHours {
		t.Fatalf("status %+v / %v, view %d running %d waiting %v h", st, full.DayAIHours, len(view.Running.Cards), len(view.Waiting.Cards), view.DayAIHours)
	}
}

func TestAPITodayEmptyStoreOK(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	rec := get(t, s, "/api/today")
	if rec.Code != 200 {
		t.Fatalf("empty store status %d", rec.Code)
	}
}

func TestAPITodayBadStructuredJSON(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := sess("x", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 100)
	srv := newSeededServer(t, tnow, []store.Session{s}, nil)
	if err := srv.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("x", tnow), Structured: "not json"}); err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/api/today")
	if rec.Code != 200 {
		t.Fatalf("a bad structured summary must still 200, got %d", rec.Code)
	}
}

func TestAPITodayCarriesReviews(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	srv := newSeededServer(t, tnow, nil, nil)
	if err := srv.store.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_request", Title: "please review", Repo: "o/r", URL: "https://x/pull/1", ReviewRequestedOfMe: true, UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/api/today")
	var v TodayView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Reviews) != 1 || v.Reviews[0].Title != "please review" {
		t.Fatalf("api should carry reviews: %+v", v.Reviews)
	}
}

func TestCardSignalsExcludeToolCalls(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 100)}
	factsFor := map[string][]facts.Fact{
		"wait": {{Type: "prompt", Timestamp: tnow.Add(-time.Hour)}, {Type: "tool_call"}, {Type: "tool_call"}, {Type: "commit"}},
	}
	s := newSeededServer(t, tnow, ss, factsFor)
	rec := get(t, s, "/api/today")
	var v TodayView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(v.Waiting.Cards) != 1 {
		t.Fatalf("expected the waiting card: %+v", v.Waiting)
	}
	card := v.Waiting.Cards[0]
	if _, ok := card.Signals["tool_call"]; ok {
		t.Fatalf("Signals must never include tool_call: %+v", card.Signals)
	}
	if card.Signals["prompt"] != 1 || len(card.Signals) != 1 {
		t.Fatalf("Signals are today's prompts and decisions only: %+v", card.Signals)
	}
}

// waitingCard is the one waiting session on today's page.
func waitingCard(t *testing.T, s *Server) Card {
	t.Helper()
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(v.Waiting.Cards) != 1 {
		t.Fatalf("expected one waiting card: %+v", v.Waiting)
	}
	return v.Waiting.Cards[0]
}

func TestCardSignalsCountToday(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-48*time.Hour), tnow.Add(-10*time.Minute), 100)}
	var fs []facts.Fact
	for range 60 {
		fs = append(fs, facts.Fact{Type: "prompt", Timestamp: tnow.Add(-24 * time.Hour)})
	}
	fs = append(fs, facts.Fact{Type: "prompt", Timestamp: tnow.Add(-time.Hour)})
	card := waitingCard(t, newSeededServer(t, tnow, ss, map[string][]facts.Fact{"wait": fs}))
	if card.Prompts != 1 || card.Signals["prompt"] != 1 {
		t.Fatalf("both counts must be today's: prompts %d, signals %+v", card.Prompts, card.Signals)
	}
}

func TestCardCountsTodaysDecisions(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-48*time.Hour), tnow.Add(-10*time.Minute), 100)}
	card := waitingCard(t, newSeededServer(t, tnow, ss, map[string][]facts.Fact{"wait": {
		{Type: "decision", Timestamp: tnow.Add(-24 * time.Hour)},
		{Type: "decision", Timestamp: tnow.Add(-time.Hour)},
		{Type: "decision", Timestamp: tnow.Add(-time.Minute)},
	}}))
	if card.Decisions != 2 || card.Signals["decision"] != 2 {
		t.Fatalf("both must be today's two: decisions %d, signals %+v", card.Decisions, card.Signals)
	}
}

func TestCardSignalsLeaveOutADayWithNoPrompts(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{sess("wait", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-48*time.Hour), tnow.Add(-10*time.Minute), 100)}
	card := waitingCard(t, newSeededServer(t, tnow, ss, map[string][]facts.Fact{"wait": {{Type: "prompt", Timestamp: tnow.Add(-24 * time.Hour)}}}))
	if _, ok := card.Signals["prompt"]; ok || card.Prompts != 0 {
		t.Fatalf("no prompts today must show none: prompts %d, signals %+v", card.Prompts, card.Signals)
	}
}

func TestAPITodayReadsTheClocksLocalDay(t *testing.T) {
	// 22:00 on the 26th at UTC-03:00 is 01:00Z on the 27th: the UTC date is the 27th, the
	// local date the 26th, and the local date picks the day rows.
	loc := time.FixedZone("x", -3*3600)
	tnow := time.Date(2026, 10, 26, 22, 0, 0, 0, loc)
	s := newSeededServer(t, tnow, []store.Session{sess("s", facts.TailAIDone, tnow.Add(-time.Hour), tnow.Add(-time.Hour), tnow.Add(-time.Hour), 0)}, nil)
	se, _, _ := s.store.GetSession("s")
	if err := s.store.Apply(store.Delta{Project: store.Project{Key: se.ProjectKey, Name: se.ProjectKey}, Session: se, CursorPath: "p", Days: map[string]facts.Day{
		"2026-10-26": {AISeconds: 1800, First: tnow.Add(-2 * time.Hour), Last: tnow.Add(-time.Hour)},
		"2026-10-27": {AISeconds: 7200, First: tnow.Add(time.Hour), Last: tnow.Add(2 * time.Hour)},
	}}); err != nil {
		t.Fatal(err)
	}
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.DayAIHours != 0.5 {
		t.Fatalf("today is the 26th locally; want 0.5 AI-hours, got %v", v.DayAIHours)
	}
}

func TestAPITodayCarriesTicketsFinishedToday(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("jira", []store.ExternalItem{
		{Source: "jira", Kind: "task", ExternalID: "OPS-1", Repo: "OPS-1", Title: "Rotate the keys", Stage: "completed", CompletedAt: tnow.Add(-time.Hour), UpdatedAt: tnow},
	}); err != nil {
		t.Fatal(err)
	}
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.FinishedToday) != 1 || v.FinishedToday[0].Key != "OPS-1" {
		t.Fatalf("finished today from jira: %+v", v.FinishedToday)
	}
}

func TestAPITodayCountsTodaysPrompts(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ss := []store.Session{
		sess("a", facts.TailAIDone, tnow.Add(-time.Hour), tnow.Add(-time.Hour), tnow.Add(-time.Hour), 0),
		sess("b", facts.TailAIDone, tnow.Add(-time.Hour), tnow.Add(-time.Hour), tnow.Add(-time.Hour), 0),
	}
	s := newSeededServer(t, tnow, ss, map[string][]facts.Fact{
		"a": {{Type: "prompt", Timestamp: tnow.Add(-time.Hour)}, {Type: "prompt", Timestamp: tnow.Add(-13 * time.Hour)}, {Type: "assistant_turn", Timestamp: tnow}},
		"b": {{Type: "prompt", Timestamp: tnow.Add(-2 * time.Hour)}},
	})
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.PromptsToday != 2 {
		t.Fatalf("want today's 2 prompts across sessions, got %d", v.PromptsToday)
	}
	for _, l := range []Lane{v.Running, v.Waiting, v.Idle} {
		for _, c := range l.Cards {
			if c.ID == "a" && c.Prompts != 1 {
				t.Fatalf("a card counts today's prompts only: want 1, got %d", c.Prompts)
			}
		}
	}
}

// A ticket started today that is already in today's plan is not listed again.
func TestAPITodayStartedTodayLeavesOutPlannedTickets(t *testing.T) {
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, nil, nil)
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "in-plan", Stage: "started", StartedAt: tnow.Add(-time.Hour)},
		{Source: "linear", Kind: "task", ExternalID: "in-old-plan", Stage: "started", StartedAt: tnow.Add(-time.Hour)},
		{Source: "linear", Kind: "task", ExternalID: "free", Stage: "started", StartedAt: tnow.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []store.Plan{
		{ID: "today", WindowStart: tnow.Add(-3 * time.Hour), WindowEnd: tnow.Add(3 * time.Hour), Status: "active"},
		{ID: "yday", WindowStart: tnow.Add(-27 * time.Hour), WindowEnd: tnow.Add(-21 * time.Hour), Status: "past"},
	} {
		if err := s.store.CreatePlan(p); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i1", PlanID: "today", RefSource: "linear", RefID: "in-plan"})
	_ = s.store.AddPlanItem(store.PlanItem{ID: "i2", PlanID: "yday", RefSource: "linear", RefID: "in-old-plan"})
	var v TodayView
	if err := json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, st := range v.StartedToday {
		ids = append(ids, st.ID)
	}
	if strings.Join(ids, ",") != "in-old-plan,free" && strings.Join(ids, ",") != "free,in-old-plan" {
		t.Fatalf("started today minus today's plan items: %v", ids)
	}
}
