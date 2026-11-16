// Package watch is the always-on daemon: it watches Claude transcripts for
// activity, keeps the local store current, and derives each session's status.
package watch

import (
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

// Session status values (see docs/data-model.md). "stuck" is deferred: it
// cannot be told from a slow tool or a handed-back turn without heuristics.
// There is no "done": a quiet session cannot be told finished from parked, so
// it reads idle.
const (
	StatusRunning = "running"
	StatusWaiting = "waiting"
	StatusIdle    = "idle"
)

// Status derivation windows. Transcript lines land every few seconds while an
// agent works, so a short silence means it is not visibly producing - unless a
// tool is mid-flight, which runs with no writes (hence the longer
// ToolRunningWindow). A handed-back session stays "waiting on you" until it has
// been quiet for WaitingWindow, so a parked decision survives a lunch or a
// meeting.
const (
	RunningWindow     = 2 * time.Minute
	ToolRunningWindow = 20 * time.Minute
	WaitingWindow     = 4 * time.Hour
	// BackgroundWindow bounds how long a session whose turn ended on a
	// background dispatch is still shown as working. A background task writes
	// nothing to this transcript, so past the window we cannot claim it is
	// still running; it becomes idle, never "waiting on you" (the human was not
	// the blocker).
	BackgroundWindow = 30 * time.Minute
)

// SessionStatus is a stored session's status: running while any of its
// sub-agents is working by the same windows as the session's own transcript,
// else DeriveStatus of the session's own last event and tail.
func SessionStatus(now time.Time, se store.Session) string {
	own := DeriveStatus(now, se.LastEventAt, se.Tail)
	if own == StatusWaiting {
		return own // handed back: sub-agents still at work show on the card
	}
	for _, sa := range se.Subagents {
		if DeriveStatus(now, sa.LastEventAt, sa.Tail) == StatusRunning {
			return StatusRunning
		}
	}
	return own
}

// DeriveStatus maps (now, last event time, tail shape) to a session status. It
// is pure: the watcher calls it both when new activity lands and on its idle
// sweep, so a quiet session ages on its own.
func DeriveStatus(now, lastEvent time.Time, tail string) string {
	if lastEvent.IsZero() {
		return StatusIdle // no real event seen
	}
	age := now.Sub(lastEvent)
	// The transcript says the turn ended, so it hands back at once. Any other tail
	// can't say that yet, so it gets RunningWindow.
	// An old stored ai_background reads the same: background work no longer
	// keeps a handed-back session running.
	if age >= 0 && age <= WaitingWindow && (tail == facts.TailAIEnded || tail == facts.TailAIBackground) {
		return StatusWaiting
	}
	if age < 0 || age <= RunningWindow {
		return StatusRunning // something just happened, whatever the tail
	}
	switch tail {
	case facts.TailAIDone, facts.TailAIEnded:
		if age <= WaitingWindow {
			return StatusWaiting // handed back to you; persists until long-quiet
		}
		return StatusIdle
	case facts.TailAIWorking:
		if age <= ToolRunningWindow {
			return StatusRunning // a dispatched tool may still be executing
		}
		return StatusIdle // silent too long to claim running
	case facts.TailAIBackground:
		if age <= BackgroundWindow {
			return StatusRunning // a background task or sub-agent is still working
		}
		return StatusIdle // past the window with no activity; not "waiting on you"
	default: // human or empty
		return StatusIdle
	}
}
