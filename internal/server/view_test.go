package server

import (
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
)

// now is a fixed UTC reference for most cases.
var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func sess(id, tail string, lastEvent, started, lastAct time.Time, aiSeconds float64) store.Session {
	return store.Session{
		ID: id, ProjectKey: "github.com/x/" + id, Agent: "claude", Branch: "main",
		StartedAt: started, LastActivityAt: lastAct, LastEventAt: lastEvent,
		Tail: tail, AIActiveSeconds: aiSeconds, Status: "", // stored status intentionally empty
	}
}

func TestBuildViewLanesByLiveStatus(t *testing.T) {
	running := sess("run", facts.TailAIWorking, now.Add(-30*time.Second), now.Add(-time.Hour), now.Add(-30*time.Second), 100)
	waiting := sess("wait", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-time.Hour), now.Add(-10*time.Minute), 100)
	idle := sess("idle", facts.TailAIWorking, now.Add(-30*time.Minute), now.Add(-time.Hour), now.Add(-30*time.Minute), 100)
	quiet := sess("quiet", facts.TailAIDone, now.Add(-5*time.Hour), now.Add(-6*time.Hour), now.Add(-5*time.Hour), 100)
	v := BuildView([]store.Session{running, waiting, idle, quiet}, nil, nil, nil, now)
	if len(v.Running.Cards) != 1 || v.Running.Cards[0].ID != "run" {
		t.Fatalf("running lane: %+v", v.Running)
	}
	if len(v.Waiting.Cards) != 1 || v.Waiting.Cards[0].ID != "wait" {
		t.Fatalf("waiting lane: %+v", v.Waiting)
	}
	// a handed-back session long quiet reads idle: there is no Done lane
	if len(v.Idle.Cards) != 2 || v.Idle.Cards[0].ID != "idle" || v.Idle.Cards[1].ID != "quiet" || v.Idle.Cards[1].Status != "idle" {
		t.Fatalf("idle lane: %+v", v.Idle)
	}
}

func TestBuildViewMissingSummaryRendersFromSession(t *testing.T) {
	s := sess("s", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-time.Hour), now.Add(-10*time.Minute), 3600)
	days := map[string]facts.Day{"s": {AISeconds: 3600, First: now.Add(-time.Hour), Last: now.Add(-10 * time.Minute)}}
	v := BuildView([]store.Session{s}, nil, days, nil, now)
	c := v.Waiting.Cards[0]
	if c.AIHours != 1.0 || c.TodayPoints != nil || !c.TodayAsOf.IsZero() {
		t.Fatalf("card from session only: %+v", c)
	}
}

func TestBuildViewBadStructuredJSON(t *testing.T) {
	s := sess("s", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-time.Hour), now.Add(-10*time.Minute), 100)
	sum := store.Summary{Level: summary.LevelDay, SubjectID: summary.DayKey("s", now), Structured: "not json"}
	v := BuildView([]store.Session{s}, []store.Summary{sum}, nil, nil, now)
	c := v.Waiting.Cards[0]
	if c.TodayPoints != nil {
		t.Fatalf("bad structured should fall back cleanly: %+v", c)
	}
}

func TestBuildViewCarriesTodaysPointsOnly(t *testing.T) {
	s := sess("s", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-time.Hour), now.Add(-10*time.Minute), 100)
	asOf := now.Add(-10 * time.Minute)
	sums := []store.Summary{
		{Level: summary.LevelDay, SubjectID: summary.DayKey("s", now.AddDate(0, 0, -1)), Structured: `["yesterday"]`},
		{Level: summary.LevelDay, SubjectID: summary.DayKey("s", now), Structured: `["fixed a bug","wrote a test"]`, AsOf: asOf},
	}
	v := BuildView([]store.Session{s}, sums, nil, nil, now)
	c := v.Waiting.Cards[0]
	if strings.Join(c.TodayPoints, "|") != "fixed a bug|wrote a test" || !c.TodayAsOf.Equal(asOf) {
		t.Fatalf("today's points on the card: %+v", c)
	}
}

func TestBuildViewUsesTodaysActivityOnly(t *testing.T) {
	// started yesterday with a large lifetime total, but only 30 min of AI time
	// between 10:00 and 11:30 today
	yday := sess("yday", facts.TailAIDone, now.Add(-20*time.Minute), now.Add(-20*time.Hour), now.Add(-20*time.Minute), 7200)
	today := sess("today", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-2*time.Hour), now.Add(-10*time.Minute), 3600)
	// no activity today: no row, so it adds nothing to the day's totals
	quiet := sess("quiet", facts.TailAIDone, now.Add(-15*time.Minute), now.Add(-30*time.Hour), now.Add(-15*time.Minute), 900)
	days := map[string]facts.Day{
		"yday":  {AISeconds: 1800, First: now.Add(-2 * time.Hour), Last: now.Add(-30 * time.Minute)},
		"today": {AISeconds: 3600, First: now.Add(-90 * time.Minute), Last: now.Add(-10 * time.Minute)},
	}
	v := BuildView([]store.Session{yday, today, quiet}, nil, days, nil, now)
	if v.DayAIHours != 1.5 {
		t.Fatalf("day AI-hours must sum today's AI time of every session, got %v", v.DayAIHours)
	}
	if v.DaySessions != 2 {
		t.Fatalf("day session count must be the sessions active today, got %d", v.DaySessions)
	}
	if v.DaySpanHours != (110 * time.Minute).Hours() { // 10:00 to 11:50
		t.Fatalf("span must run from today's first to today's last event, got %v", v.DaySpanHours)
	}
	cards := map[string]Card{}
	for _, c := range v.Waiting.Cards {
		cards[c.ID] = c
	}
	if len(cards) != 3 {
		t.Fatalf("all three sessions should still appear in lanes, got %+v", v.Waiting.Cards)
	}
	if c := cards["yday"]; c.AIHours != 0.5 || c.WallHours != 1.5 {
		t.Fatalf("card must show today's AI-h and wall-h: %+v", c)
	}
	if c := cards["quiet"]; c.AIHours != 0 || c.WallHours != 0 {
		t.Fatalf("a session with no activity today shows 0: %+v", c)
	}
}

func TestBuildViewIncludesReviews(t *testing.T) {
	ext := []store.ExternalItem{
		{Source: "github", Kind: "review_request", Title: "review me", Repo: "o/r", URL: "u1", ReviewRequestedOfMe: true, UpdatedAt: now.Add(-time.Hour)},
		{Source: "github", Kind: "pr", Title: "my pr", Repo: "o/r", URL: "u2", State: "open", UpdatedAt: now.Add(-2 * time.Hour)},
	}
	v := BuildView(nil, nil, nil, ext, now)
	if len(v.Reviews) != 1 || v.Reviews[0].Title != "review me" || v.Reviews[0].Repo != "o/r" {
		t.Fatalf("reviews: %+v", v.Reviews)
	}
	if v.ContribPRs != 1 {
		t.Fatalf("a pr item should bump ContribPRs, got %d", v.ContribPRs)
	}
}

func TestBuildViewContribPRsTodayOnly(t *testing.T) {
	ext := []store.ExternalItem{
		{Source: "github", Kind: "pr", Title: "today pr", UpdatedAt: now.Add(-2 * time.Hour)},
		{Source: "github", Kind: "pr", Title: "old pr", UpdatedAt: now.Add(-48 * time.Hour)},
	}
	v := BuildView(nil, nil, nil, ext, now)
	if v.ContribPRs != 1 {
		t.Fatalf("only PRs active today count toward the today figure, got %d", v.ContribPRs)
	}
}

func TestBuildViewEmptyIsCalm(t *testing.T) {
	v := BuildView(nil, nil, nil, nil, now)
	if v.DayAIHours != 0 || len(v.Running.Cards) != 0 {
		t.Fatalf("empty view: %+v", v)
	}
	if !strings.Contains(strings.ToLower(v.StateOfNow), "nothing") {
		t.Fatalf("empty state-of-now should be calm: %q", v.StateOfNow)
	}
}

func TestBuildViewStateOfNowCountsLanes(t *testing.T) {
	var ss []store.Session
	mk := func(id, tail string, ev time.Time) store.Session {
		return sess(id, tail, ev, now.Add(-time.Hour), ev, 100)
	}
	ss = append(ss, mk("r1", facts.TailAIWorking, now.Add(-30*time.Second)))
	ss = append(ss, mk("r2", facts.TailAIWorking, now.Add(-time.Minute)))
	ss = append(ss, mk("w1", facts.TailAIDone, now.Add(-10*time.Minute)))
	ss = append(ss, mk("d1", facts.TailAIDone, now.Add(-5*time.Hour)))
	ss = append(ss, mk("d2", facts.TailAIDone, now.Add(-5*time.Hour)))
	ss = append(ss, mk("d3", facts.TailAIDone, now.Add(-5*time.Hour)))
	v := BuildView(ss, nil, nil, nil, now)
	if !strings.Contains(v.StateOfNow, "2") || !strings.Contains(v.StateOfNow, "1") || !strings.Contains(v.StateOfNow, "3") {
		t.Fatalf("state-of-now should mention lane counts: %q", v.StateOfNow)
	}
	if strings.Contains(v.StateOfNow, "wrapped up") {
		t.Fatalf("there is no done count: %q", v.StateOfNow)
	}
	if strings.ContainsAny(v.StateOfNow, "–—") {
		t.Fatalf("no en/em dash allowed: %q", v.StateOfNow)
	}
}

func TestBuildViewFinishedTodayFromAnyTracker(t *testing.T) {
	ext := []store.ExternalItem{
		{Source: "linear", Kind: "task", Repo: "ENG-1", Title: "early", URL: "u1", Project: "Agent", Stage: "completed", CompletedAt: now.Add(-3 * time.Hour)},
		{Source: "jira", Kind: "task", Repo: "OPS-2", Title: "late", URL: "u2", Stage: "completed", CompletedAt: now.Add(-time.Hour)},
		{Source: "linear", Kind: "task", Repo: "ENG-3", Title: "yesterday", Stage: "completed", CompletedAt: now.Add(-24 * time.Hour)},
		{Source: "linear", Kind: "task", Repo: "ENG-4", Title: "canceled", Stage: "canceled"},
		{Source: "linear", Kind: "task", Repo: "ENG-5", Title: "doing", Stage: "started"},
	}
	v := BuildView(nil, nil, nil, ext, now)
	if len(v.FinishedToday) != 2 || v.FinishedToday[0].Title != "late" || v.FinishedToday[1].Title != "early" {
		t.Fatalf("finished today, newest first: %+v", v.FinishedToday)
	}
	f := v.FinishedToday[1]
	if f.Source != "linear" || f.Key != "ENG-1" || f.URL != "u1" || f.Project != "Agent" || !f.CompletedAt.Equal(now.Add(-3*time.Hour)) {
		t.Fatalf("finished ticket fields: %+v", f)
	}
}

func TestBuildViewFinishedTodayNeverNull(t *testing.T) {
	if v := BuildView(nil, nil, nil, nil, now); v.FinishedToday == nil {
		t.Fatal("FinishedToday must be an empty list, not null")
	}
}

func TestBuildViewReviewsDoneToday(t *testing.T) {
	ext := []store.ExternalItem{
		{Source: "github", Kind: "review_done", Title: "early", Repo: "o/r", URL: "u1", UpdatedAt: now.Add(-3 * time.Hour)},
		{Source: "github", Kind: "review_done", Title: "late", Repo: "o/s", URL: "u2", UpdatedAt: now.Add(-time.Hour)},
		{Source: "github", Kind: "review_done", Title: "yesterday", URL: "u3", UpdatedAt: now.Add(-24 * time.Hour)},
		{Source: "github", Kind: "review_request", Title: "pending", URL: "u4", UpdatedAt: now},
	}
	v := BuildView(nil, nil, nil, ext, now)
	if len(v.ReviewsDone) != 2 || v.ReviewsDone[0].Title != "late" || v.ReviewsDone[1].Title != "early" {
		t.Fatalf("reviews done today, newest first: %+v", v.ReviewsDone)
	}
	r := v.ReviewsDone[0]
	if r.Repo != "o/s" || r.URL != "u2" || !r.SubmittedAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("review done fields: %+v", r)
	}
}

func TestBuildViewStartedToday(t *testing.T) {
	ext := []store.ExternalItem{
		{Source: "linear", Kind: "task", ExternalID: "id1", Repo: "ENG-1", Title: "early", URL: "u1", Project: "P", State: "In Progress", Stage: "started", Estimate: 3, StartedAt: now.Add(-3 * time.Hour)},
		{Source: "jira", Kind: "task", ExternalID: "OPS-2", Repo: "OPS-2", Title: "late", Stage: "started", StartedAt: now.Add(-time.Hour)},
		{Source: "linear", Kind: "task", ExternalID: "id3", Title: "yesterday", Stage: "started", StartedAt: now.Add(-24 * time.Hour)},
		{Source: "linear", Kind: "task", ExternalID: "id4", Title: "done", Stage: "completed", StartedAt: now.Add(-time.Hour)},
	}
	v := BuildView(nil, nil, nil, ext, now)
	if len(v.StartedToday) != 2 || v.StartedToday[0].Title != "late" || v.StartedToday[1].Title != "early" {
		t.Fatalf("started today, newest first: %+v", v.StartedToday)
	}
	if s := v.StartedToday[1]; s.Source != "linear" || s.ID != "id1" || s.Key != "ENG-1" || s.URL != "u1" || s.Project != "P" || s.State != "In Progress" || s.Stage != "started" || s.Hours != 3 || !s.StartedAt.Equal(now.Add(-3*time.Hour)) {
		t.Fatalf("started ticket fields: %+v", s)
	}
	if v.StartedToday[0].Hours != 1 {
		t.Fatalf("an unestimated ticket counts an hour: %+v", v.StartedToday[0])
	}
}

func TestBuildViewFinishedTodayCarriesID(t *testing.T) {
	ext := []store.ExternalItem{{Source: "linear", Kind: "task", ExternalID: "id1", Repo: "ENG-1", Stage: "completed", CompletedAt: now}}
	if v := BuildView(nil, nil, nil, ext, now); len(v.FinishedToday) != 1 || v.FinishedToday[0].ID != "id1" {
		t.Fatalf("finished ticket id: %+v", v.FinishedToday)
	}
}

func TestBuildViewNewListsNeverNull(t *testing.T) {
	if v := BuildView(nil, nil, nil, nil, now); v.ReviewsDone == nil || v.StartedToday == nil {
		t.Fatal("ReviewsDone and StartedToday must be empty lists, not null")
	}
}

func TestBuildViewRunningWhileASubagentWorks(t *testing.T) {
	// Still working itself, a lead with a sub-agent mid-tool is running.
	lead := sess("lead", facts.TailAIWorking, now.Add(-30*time.Minute), now.Add(-time.Hour), now.Add(-30*time.Minute), 100)
	lead.Subagents = []store.Subagent{{LastEventAt: now.Add(-5 * time.Minute), Tail: facts.TailAIWorking}}
	v := BuildView([]store.Session{lead}, nil, nil, nil, now)
	if len(v.Running.Cards) != 1 || v.Running.Cards[0].Status != "running" {
		t.Fatalf("a lead whose sub-agent is mid-tool is running: %+v", v)
	}
	// Handed back, it waits on you whatever its sub-agents do.
	lead.Tail, lead.LastEventAt = facts.TailAIEnded, now.Add(-10*time.Minute)
	v = BuildView([]store.Session{lead}, nil, nil, nil, now)
	if len(v.Waiting.Cards) != 1 {
		t.Fatalf("a handed-back lead waits on you: %+v", v)
	}
}
