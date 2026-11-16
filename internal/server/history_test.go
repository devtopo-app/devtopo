package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
)

// historyServer seeds two sessions with day rows on the 21st and 22nd of
// October and one on the 2nd of November, as of the 27th.
func historyServer(t *testing.T) *Server {
	t.Helper()
	tnow := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	tp := filepath.Join(t.TempDir(), "a.jsonl")
	line := `{"type":"user","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"fix the bug"}}` + "\n"
	if err := os.WriteFile(tp, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	a := sess("a", facts.TailAIDone, at(22, 10, 0), at(21, 9, 0), at(22, 10, 0), 0)
	a.TranscriptPath = tp
	b := sess("b", facts.TailAIDone, at(21, 16, 0), at(21, 15, 0), at(21, 16, 0), 0)
	b.Agent = "codex"
	s := newSeededServer(t, tnow, []store.Session{a, b}, map[string][]facts.Fact{
		"a": {{Type: "prompt", Timestamp: at(21, 9, 0)}, {Type: "prompt", Timestamp: at(21, 9, 30)}, {Type: "prompt", Timestamp: at(22, 9, 0)}},
		"b": {{Type: "prompt", Timestamp: at(21, 15, 0)}},
	})
	hours := func(h int, secs float64) (out [24]float64) { out[h] = secs; return }
	for id, days := range map[string]map[string]facts.Day{
		"a": {
			"2026-10-21": {AISeconds: 5400, Hours: hours(9, 5400), First: at(21, 9, 0), Last: at(21, 10, 30)},
			"2026-10-22": {AISeconds: 600, Hours: hours(9, 600), First: at(22, 9, 0), Last: at(22, 10, 0)},
			"2026-11-02": {AISeconds: 60},
		},
		"b": {"2026-10-21": {AISeconds: 1800, Hours: hours(15, 1800), First: at(21, 15, 0), Last: at(21, 16, 0)}},
	} {
		se, _, _ := s.store.GetSession(id)
		if err := s.store.Apply(store.Delta{Project: store.Project{Key: se.ProjectKey}, Session: se, Days: days, CursorPath: id + ".days"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.SetSessionName("a", "Bug hunt"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReplaceExternalItems("linear", []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "i1", Repo: "ENG-1", Title: "Fix it", URL: "u1", Stage: "completed", CompletedAt: at(21, 11, 0)},
		{Source: "linear", Kind: "task", ExternalID: "i2", Repo: "ENG-2", Title: "Later", Stage: "completed", CompletedAt: at(24, 11, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.ReplaceExternalItems("github", []store.ExternalItem{
		{Source: "github", Kind: "review_done", ExternalID: "review:p", Repo: "o/r", Title: "A PR", URL: "p", UpdatedAt: at(21, 14, 0)},
	}); err != nil {
		t.Fatal(err)
	}
	// One own activity and one planned review marked done on the 21st, the
	// review not also submitted on GitHub.
	p := store.Plan{ID: "p1", Name: "Plan", WindowStart: at(21, 8, 0), WindowEnd: at(21, 18, 0), Status: "past"}
	if err := s.store.CreatePlan(p); err != nil {
		t.Fatal(err)
	}
	for _, it := range []store.PlanItem{
		{ID: "own", PlanID: "p1", Text: "Write the doc"},
		{ID: "rev", PlanID: "p1", RefSource: "github", RefID: "q", Text: "Review q"},
		{ID: "open", PlanID: "p1", Text: "Not done"},
	} {
		if err := s.store.AddPlanItem(it); err != nil {
			t.Fatal(err)
		}
	}
	done := at(21, 17, 0)
	for _, id := range []string{"own", "rev"} {
		if err := s.store.SetPlanItemDone(id, &done); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.ReplaceCalendarEvents([]store.CalendarEvent{{UID: "m", Title: "Standup", Start: at(22, 9, 0), End: at(22, 9, 30)}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHistoryMonthHasEachActiveDay(t *testing.T) {
	rec := get(t, historyServer(t), "/api/history/month?m=2026-10")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got []HistoryMonthDay
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []HistoryMonthDay{{"2026-10-21", 2, 1}, {"2026-10-22", 1.0 / 6, 0}, {"2026-10-24", 0, 1}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("day %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
	if rec := get(t, historyServer(t), "/api/history/month?m=2026-1"); rec.Code != 400 {
		t.Fatalf("a bad month must be refused, got %d", rec.Code)
	}
}

func TestHistoryDayOverview(t *testing.T) {
	rec := get(t, historyServer(t), "/api/history/day?d=2026-10-21")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var d HistoryDay
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.AIHours != 2 || d.Prompts != 3 || d.Hours[9] != 1.5 || d.Hours[15] != 0.5 {
		t.Fatalf("numbers: %+v", d)
	}
	if !d.First.Equal(time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)) || !d.Last.Equal(time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("active span: %v - %v", d.First, d.Last)
	}
	if len(d.Sessions) != 2 || d.Sessions[0].ID != "a" || d.Sessions[0].Name != "Bug hunt" || d.Sessions[0].Prompts != 2 || d.Sessions[0].AIHours != 1.5 ||
		d.Sessions[1].Agent != "codex" || d.Sessions[0].Points == nil {
		t.Fatalf("sessions, most AI time first: %+v", d.Sessions)
	}
	if len(d.Tickets) != 1 || d.Tickets[0].Key != "ENG-1" || d.Reviews != 2 || d.OtherTasks != 1 {
		t.Fatalf("finished: %+v reviews=%d other=%d", d.Tickets, d.Reviews, d.OtherTasks)
	}
	if d.MeetingHours != nil {
		t.Fatalf("no calendar data for the 21st, so no meetings: %v", *d.MeetingHours)
	}
	rec = get(t, historyServer(t), "/api/history/day?d=2026-10-22")
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.MeetingHours == nil || *d.MeetingHours != 0.5 {
		t.Fatalf("meetings on the 22nd: %v", d.MeetingHours)
	}
}

func TestHistoryEmptyDayHasNoNullLists(t *testing.T) {
	rec := get(t, historyServer(t), "/api/history/day?d=2026-09-01")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Sessions", "Tickets"} {
		if string(raw[k]) != "[]" {
			t.Fatalf("%s: %s", k, raw[k])
		}
	}
	if rec := get(t, historyServer(t), "/api/history/day?d=21-10-2026"); rec.Code != 400 {
		t.Fatalf("a bad date must be refused, got %d", rec.Code)
	}
}

// Points stored in an older format are not shown: the day says it is
// summarizing, so they are written again in the current one.
func TestHistoryRewritesOldFormatPoints(t *testing.T) {
	s := historyServer(t)
	s.narrator = &fakeNarrator{text: "- Fixed the bug."}
	_ = s.store.UpsertSummary(store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("a", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)),
		Structured: `["**old** style"]`, SchemaVersion: 1})
	var d HistoryDay
	json.Unmarshal(get(t, s, "/api/history/day?d=2026-10-21").Body.Bytes(), &d)
	if !d.Sessions[0].Summarizing || len(d.Sessions[0].Points) != 0 {
		t.Fatalf("old points are redone, not shown: %+v", d.Sessions[0])
	}
}

// An ask whose page moved on while it waited its turn makes nothing: the turn
// goes to the day the user is looking at now.
func TestHistoryPointsSkipsACancelledAsk(t *testing.T) {
	s := historyServer(t)
	n := &fakeNarrator{text: "- fixed the bug"}
	s.narrator = n
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/history/points?d=2026-10-21&session=a", nil).WithContext(ctx)
	r.Host = testHost
	s.Handler().ServeHTTP(rec, r)
	if n.calls != 0 {
		t.Fatalf("a cancelled ask narrated %d times", n.calls)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("a cancelled ask is not a success: %d", rec.Code)
	}
}

// A past day with no stored points says it is summarizing; asking for them
// narrates once, and from then on they come with the day.
func TestHistoryPointsMadeOnceForAPastDay(t *testing.T) {
	s := historyServer(t)
	n := &fakeNarrator{text: "- fixed the bug"}
	s.narrator = n
	var d HistoryDay
	json.Unmarshal(get(t, s, "/api/history/day?d=2026-10-21").Body.Bytes(), &d)
	if !d.Sessions[0].Summarizing {
		t.Fatalf("a past day without points is summarizing: %+v", d.Sessions[0])
	}
	for i := 0; i < 2; i++ {
		rec := post(t, s, "/api/history/points?d=2026-10-21&session=a")
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var got struct{ Points []string }
		json.Unmarshal(rec.Body.Bytes(), &got)
		if len(got.Points) != 1 || got.Points[0] != "fixed the bug" {
			t.Fatalf("points: %+v", got)
		}
	}
	if n.calls != 1 {
		t.Fatalf("one claude call per session per day, got %d", n.calls)
	}
	json.Unmarshal(get(t, s, "/api/history/day?d=2026-10-21").Body.Bytes(), &d)
	if d.Sessions[0].Summarizing || len(d.Sessions[0].Points) != 1 {
		t.Fatalf("cached points come with the day: %+v", d.Sessions[0])
	}
	// Today is the live loop's to summarize.
	json.Unmarshal(get(t, s, "/api/history/day?d=2026-10-27").Body.Bytes(), &d)
	for _, se := range d.Sessions {
		if se.Summarizing {
			t.Fatalf("today is not summarized on demand: %+v", se)
		}
	}
}
