package facts_test

import (
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/transcript"
)

func codexRecords(t *testing.T) []transcript.Record {
	t.Helper()
	recs, _, _, err := transcript.ReadNew("../transcript/testdata/codex.jsonl", 0)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// A Codex turn counts the typed message as the one prompt, and the agent's time
// from it to the final answer, the 30s tool call included.
func TestCodexPromptsAndAITime(t *testing.T) {
	recs := codexRecords(t)
	prompts := 0
	for _, f := range facts.Extract(recs) {
		if f.Type == "prompt" {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatalf("prompts %d, want 1 (developer and injected messages are not prompts)", prompts)
	}
	got, last := facts.AIActiveSeconds(recs, facts.Boundary{})
	if got != 38 {
		t.Fatalf("AI seconds %v, want 38 (09:00:02 prompt to 09:00:40 answer)", got)
	}
	if !last.Time.Equal(time.Date(2026, 10, 21, 9, 0, 40, 0, time.UTC)) {
		t.Fatalf("last event %v", last.Time)
	}
}

// TurnTail reads a Codex session's tail from its turn events: an open turn is
// working, an ended one handed back, an aborted one the human's; a batch with
// no turn event keeps the previous tail.
func TestTurnTail(t *testing.T) {
	recs := codexRecords(t)
	if got := facts.TurnTail(recs, ""); got != facts.TailAIEnded {
		t.Fatalf("ended turn: %q", got)
	}
	if got := facts.TurnTail(recs[:10], ""); got != facts.TailAIWorking {
		t.Fatalf("open turn: %q", got)
	}
	if got := facts.TurnTail(recs[11:14], facts.TailAIWorking); got != facts.TailAIWorking {
		t.Fatalf("no turn event keeps prev: %q", got)
	}
	aborted := append(recs[:2:2], transcript.Record{Turn: transcript.TurnAborted, Timestamp: recs[1].Timestamp.Add(time.Second)})
	if got := facts.TurnTail(aborted, ""); got != facts.TailHuman {
		t.Fatalf("aborted turn: %q", got)
	}
}
