package transcript_test

import (
	"testing"

	"github.com/devtopo-app/devtopo/internal/transcript"
)

// A Codex rollout decodes into the Claude record shape the facts read: the
// typed message is a user record, injected context a meta one, tool calls and
// their outputs tool_use and tool_result blocks, and the turn events mark a
// turn open or ended.
func TestDecodeCodexRollout(t *testing.T) {
	recs, _, dropped, err := transcript.ReadNew("testdata/codex.jsonl", 0)
	if err != nil || dropped != 0 {
		t.Fatalf("err %v dropped %d", err, dropped)
	}
	if len(recs) != 17 {
		t.Fatalf("got %d records", len(recs))
	}
	meta := recs[0]
	if meta.SessionID != "0c0d0e0f-1111-7222-8333-944455556666" || meta.CWD != `C:\repo` || meta.GitBranch != "feat/x" || meta.Originator != "Codex Desktop" {
		t.Fatalf("session_meta: %+v", meta)
	}
	if recs[1].Turn != transcript.TurnStarted || recs[15].Turn != transcript.TurnEnded {
		t.Fatalf("turns: %q %q", recs[1].Turn, recs[15].Turn)
	}
	if recs[3].Type == "user" || recs[3].Type == "assistant" {
		t.Fatalf("a developer message is no turn: %+v", recs[3])
	}
	for _, i := range []int{4, 5} {
		if r := recs[i]; r.Type != "user" || !r.IsMeta {
			t.Fatalf("injected context %d must be meta: %+v", i, r)
		}
	}
	if r := recs[6]; r.Type != "user" || r.IsMeta || r.Text != "add a readme" || len(r.Content) != 0 {
		t.Fatalf("typed prompt: %+v", r)
	}
	if r := recs[7]; r.Type != "assistant" || r.Text != "I will add it." {
		t.Fatalf("assistant message: %+v", r)
	}
	if r := recs[8]; r.Type != "assistant" || len(r.Content) != 1 || r.Content[0] != (transcript.Block{Type: "tool_use", Name: "exec", ToolUseID: "call_a"}) {
		t.Fatalf("custom tool call: %+v", r)
	}
	if r := recs[9]; r.Type != "user" || len(r.Content) != 1 || r.Content[0] != (transcript.Block{Type: "tool_result", ToolUseID: "call_a"}) {
		t.Fatalf("custom tool output: %+v", r)
	}
	if r := recs[11]; r.Type != "assistant" || len(r.Content) != 0 {
		t.Fatalf("reasoning: %+v", r)
	}
	if r := recs[12]; r.Type != "assistant" || r.Content[0].Name != "wait" || r.Content[0].ToolUseID != "call_b" {
		t.Fatalf("function call: %+v", r)
	}
	if r := recs[13]; r.Type != "user" || r.Content[0].Type != "tool_result" || r.Content[0].ToolUseID != "call_b" {
		t.Fatalf("function output: %+v", r)
	}
	for _, i := range []int{2, 10, 16} {
		if r := recs[i]; r.Type == "user" || r.Type == "assistant" || r.Turn != "" {
			t.Fatalf("record %d is no event: %+v", i, r)
		}
	}
}

// An aborted turn ends it too.
func TestDecodeCodexTurnAborted(t *testing.T) {
	r, err := transcript.Decode([]byte(`{"timestamp":"2026-10-21T09:00:00Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"t","reason":"interrupted"}}`))
	if err != nil || r.Turn != transcript.TurnAborted {
		t.Fatalf("%v %+v", err, r)
	}
}

func TestCodexInjected(t *testing.T) {
	for s, want := range map[string]bool{
		"<environment_context>\n<cwd>x</cwd>": true,
		"<user_instructions attr=\"1\">":      true,
		"# AGENTS.md instructions for C:\\r":  true,
		"fix the <div> tag":                   false,
		"<3 thanks":                           false,
		"add a readme":                        false,
	} {
		if got := transcript.CodexInjected(s); got != want {
			t.Errorf("%q: got %v", s, got)
		}
	}
}
