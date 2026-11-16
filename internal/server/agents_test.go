package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

// A card lists only the sub-agents working now, by the name Claude Code gave
// them (else their description); quiet ones are left out.
func TestCardsListOnlyRunningSubagentsByName(t *testing.T) {
	tnow := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, tnow, []store.Session{
		sess("lead", facts.TailAIDone, tnow.Add(-10*time.Minute), tnow.Add(-time.Hour), tnow.Add(-10*time.Minute), 60),
	}, nil)
	dir := t.TempDir()
	agent := func(file, meta string, last time.Time) {
		path := filepath.Join(dir, file+".jsonl")
		if err := os.WriteFile(filepath.Join(dir, file+".meta.json"), []byte(meta), 0o600); err != nil {
			t.Fatal(err)
		}
		se, _, _ := s.store.GetSession("lead")
		if err := s.store.Apply(store.Delta{Project: store.Project{Key: se.ProjectKey, Name: se.ProjectKey}, Session: se, CursorPath: path,
			Subagent: store.Subagent{LastEventAt: last, Tail: facts.TailAIDone}}); err != nil {
			t.Fatal(err)
		}
	}
	agent("agent-ausage-1", `{"name":"usage","description":"Claude usage limits"}`, tnow.Add(-30*time.Second))
	agent("agent-a2", `{"description":"Investigate AI hours"}`, tnow.Add(-time.Minute))
	agent("agent-aold-3", `{"name":"old"}`, tnow.Add(-30*time.Minute))

	var v TodayView
	_ = json.Unmarshal(get(t, s, "/api/today").Body.Bytes(), &v)
	// The lead handed back, so it waits on you, naming the sub-agents still at work.
	if len(v.Waiting.Cards) != 1 {
		t.Fatalf("the lead should read waiting: %+v", v)
	}
	got := v.Waiting.Cards[0].RunningAgents
	if len(got) != 2 || got[0] != "usage" || got[1] != "Investigate AI hours" {
		t.Fatalf("want [usage, Investigate AI hours], got %v", got)
	}
}

func TestAgentCardsSayWhenTheAgentFinished(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	own, sub := now.Add(-time.Hour), now.Add(-30*time.Minute)
	sessions := []store.Session{
		// LastActivityAt moves on non-agent lines too, so it must not be the source.
		{ID: "own", LastEventAt: own, LastActivityAt: now},
		{ID: "sub", LastEventAt: own, Subagents: []store.Subagent{{LastEventAt: sub}, {LastEventAt: own.Add(-time.Hour)}}},
		{ID: "none"},
	}
	v := TodayView{Idle: Lane{Cards: []Card{{ID: "own"}, {ID: "sub"}, {ID: "none"}}}}
	agentCards(&v, sessions, now, nil)
	cards := v.Idle.Cards
	if !cards[0].FinishedAt.Equal(own) {
		t.Errorf("own: %v", cards[0].FinishedAt)
	}
	if !cards[1].FinishedAt.Equal(sub) {
		t.Errorf("a sub-agent working later counts: %v", cards[1].FinishedAt)
	}
	if !cards[2].FinishedAt.IsZero() {
		t.Errorf("none: %v", cards[2].FinishedAt)
	}
}

func TestRunningCardsSayWhenTheirTaskStarted(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	own := now.Add(-time.Minute)
	sessions := []store.Session{
		sess("run", facts.TailAIWorking, own, now.Add(-time.Hour), own, 60),
		sess("wait", facts.TailAIDone, now.Add(-10*time.Minute), now.Add(-time.Hour), now.Add(-10*time.Minute), 60),
	}
	s := newSeededServer(t, now, sessions, nil)
	prompt := func(se store.Session, at time.Time) {
		t.Helper()
		err := s.store.Apply(store.Delta{Project: store.Project{Key: "p", Name: "p"}, Session: se, CursorPath: se.ID + at.String(), Facts: []facts.Fact{{Type: "prompt", Timestamp: at}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	prompt(sessions[0], now.Add(-time.Hour))
	prompt(sessions[0], now.Add(-5*time.Minute))
	prompt(sessions[1], now.Add(-20*time.Minute))
	v := todayOf(t, s)
	if len(v.Running.Cards) != 1 || !v.Running.Cards[0].WorkingSince.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("running: %+v", v.Running.Cards)
	}
	if len(v.Waiting.Cards) != 1 || !v.Waiting.Cards[0].WorkingSince.IsZero() {
		t.Fatalf("waiting: %+v", v.Waiting.Cards)
	}
}

func TestAgentCardsCountOpenBackgroundTasks(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	sessions := []store.Session{{ID: "a", TranscriptPath: "a.jsonl"}, {ID: "b", TranscriptPath: "b.jsonl"}}
	v := TodayView{Waiting: Lane{Cards: []Card{{ID: "a"}, {ID: "b"}}}}
	agentCards(&v, sessions, now, map[string]int{"a.jsonl": 2})
	if v.Waiting.Cards[0].BackgroundTasks != 2 || v.Waiting.Cards[1].BackgroundTasks != 0 {
		t.Fatalf("cards: %+v", v.Waiting.Cards)
	}
}

// Picked up again on a background task's news, the timer starts there, not at the prompt.
func TestRunningCardsTimeAPickedUpTaskFromWhenItWasPickedUp(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	se := sess("run", facts.TailAIWorking, now.Add(-time.Minute), now.Add(-time.Hour), now.Add(-time.Minute), 60)
	s := newSeededServer(t, now, []store.Session{se}, nil)
	err := s.store.Apply(store.Delta{Project: store.Project{Key: "p", Name: "p"}, Session: se, CursorPath: "run-facts", Facts: []facts.Fact{
		{Type: "prompt", Timestamp: now.Add(-19 * time.Minute)},
		{Type: "turn_end", Timestamp: now.Add(-15 * time.Minute)},
		{Type: "resume", Timestamp: now.Add(-2 * time.Minute)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c := todayOf(t, s).Running.Cards; len(c) != 1 || !c[0].WorkingSince.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("cards: %+v", c)
	}
}
