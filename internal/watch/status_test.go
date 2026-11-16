package watch_test

import (
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/watch"
)

func TestDeriveStatusBackground(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// A stored ai_background (no longer written) reads as a handed-back turn:
	// waiting, then idle past WaitingWindow. Background work keeps nothing running.
	for age, want := range map[time.Duration]string{10 * time.Minute: watch.StatusWaiting, 45 * time.Minute: watch.StatusWaiting, 5 * time.Hour: watch.StatusIdle} {
		if got := watch.DeriveStatus(now, now.Add(-age), facts.TailAIBackground); got != want {
			t.Fatalf("bg at %v -> %s, got %q", age, want, got)
		}
	}
	// the handed-back case is unchanged: ai_done within WaitingWindow -> waiting
	if got := watch.DeriveStatus(now, now.Add(-10*time.Minute), facts.TailAIDone); got != watch.StatusWaiting {
		t.Fatalf("ai_done within WaitingWindow -> waiting, got %q", got)
	}
}

func TestDeriveStatus(t *testing.T) {
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		age        time.Duration
		tail, want string
	}{
		{"fresh is running whatever the tail", 30 * time.Second, facts.TailHuman, watch.StatusRunning},
		{"handed back ages to waiting", 10 * time.Minute, facts.TailAIDone, watch.StatusWaiting},
		{"text with no end marker stays running at first", 30 * time.Second, facts.TailAIDone, watch.StatusRunning},
		{"an ended turn waits at once", 0, facts.TailAIEnded, watch.StatusWaiting},
		{"an ended turn waits seconds later", 5 * time.Second, facts.TailAIEnded, watch.StatusWaiting},
		{"an ended turn keeps waiting", 3 * time.Hour, facts.TailAIEnded, watch.StatusWaiting},
		{"an ended turn past WaitingWindow is idle", 5 * time.Hour, facts.TailAIEnded, watch.StatusIdle},
		{"waiting persists across a lunch", 3 * time.Hour, facts.TailAIDone, watch.StatusWaiting},
		{"handed back past WaitingWindow is idle", 5 * time.Hour, facts.TailAIDone, watch.StatusIdle},
		{"tool in flight stays running", 10 * time.Minute, facts.TailAIWorking, watch.StatusRunning},
		{"tool silent past ToolRunningWindow is idle", 30 * time.Minute, facts.TailAIWorking, watch.StatusIdle},
		{"tool silent for hours is idle", 5 * time.Hour, facts.TailAIWorking, watch.StatusIdle},
		{"human tail ages to idle", 10 * time.Minute, facts.TailHuman, watch.StatusIdle},
		{"human tail stays idle for hours", 5 * time.Hour, facts.TailHuman, watch.StatusIdle},
		{"empty tail in mid band is idle", 10 * time.Minute, "", watch.StatusIdle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := watch.DeriveStatus(base, base.Add(-c.age), c.tail); got != c.want {
				t.Fatalf("age %s tail %q: want %q got %q", c.age, c.tail, c.want, got)
			}
		})
	}
	if got := watch.DeriveStatus(base, time.Time{}, facts.TailAIDone); got != watch.StatusIdle {
		t.Fatalf("zero lastEvent: want idle got %q", got)
	}
	if got := watch.DeriveStatus(base, base.Add(5*time.Second), facts.TailAIDone); got != watch.StatusRunning {
		t.Fatalf("future lastEvent (skew): want running got %q", got)
	}
}

func TestSessionStatusRunsWhileASubagentWorks(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	handedBack := store.Session{LastEventAt: now.Add(-10 * time.Minute), Tail: facts.TailAIDone}
	sub := func(age time.Duration, tail string) store.Subagent {
		return store.Subagent{LastEventAt: now.Add(-age), Tail: tail}
	}
	cases := []struct {
		name string
		subs []store.Subagent
		want string
	}{
		{"no sub-agents keeps the session's own status", nil, watch.StatusWaiting},
		{"a sub-agent that just wrote", []store.Subagent{sub(30*time.Second, facts.TailAIDone)}, watch.StatusWaiting},
		{"a sub-agent mid-tool", []store.Subagent{sub(10*time.Minute, facts.TailAIWorking)}, watch.StatusWaiting},
		{"a sub-agent that just ended its turn", []store.Subagent{sub(5*time.Second, facts.TailAIEnded)}, watch.StatusWaiting},
		{"one quiet, one working", []store.Subagent{sub(time.Hour, facts.TailAIDone), sub(time.Minute, facts.TailAIWorking)}, watch.StatusWaiting},
		{"quiet sub-agents change nothing", []store.Subagent{sub(10*time.Minute, facts.TailAIDone), sub(time.Hour, facts.TailAIWorking)}, watch.StatusWaiting},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			se := handedBack
			se.Subagents = c.subs
			if got := watch.SessionStatus(now, se); got != c.want {
				t.Fatalf("want %q got %q", c.want, got)
			}
		})
	}
}

func TestHandedBackIsWaitingWhateverRunsBehind(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	// An old stored tail from before background work stopped counting as running.
	if got := watch.DeriveStatus(now, now.Add(-10*time.Second), facts.TailAIBackground); got != watch.StatusWaiting {
		t.Errorf("stored ai_background: %q", got)
	}
	ended := store.Session{LastEventAt: now.Add(-10 * time.Second), Tail: facts.TailAIEnded,
		Subagents: []store.Subagent{{LastEventAt: now.Add(-5 * time.Second), Tail: facts.TailAIWorking}}}
	if got := watch.SessionStatus(now, ended); got != watch.StatusWaiting {
		t.Errorf("a sub-agent working behind a handed-back session: %q", got)
	}
	working := store.Session{LastEventAt: now.Add(-10 * time.Minute), Tail: facts.TailAIWorking,
		Subagents: []store.Subagent{{LastEventAt: now.Add(-5 * time.Second), Tail: facts.TailAIWorking}}}
	if got := watch.SessionStatus(now, working); got != watch.StatusRunning {
		t.Errorf("a sub-agent working for a working session: %q", got)
	}
}
