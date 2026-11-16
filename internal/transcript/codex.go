package transcript

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Turn values mark a Codex turn opening and closing (Record.Turn).
const (
	TurnStarted = "started"
	TurnEnded   = "ended"
	TurnAborted = "aborted"
)

// injectedTag starts the context Codex adds as user messages
// (<environment_context>, open pages, instructions), not typed by the human.
var injectedTag = regexp.MustCompile(`^<[a-z][a-z0-9_-]*[ >]`)

// CodexInjected reports whether a Codex user message's text is context Codex
// injected rather than what the human typed.
func CodexInjected(text string) bool {
	text = strings.TrimSpace(text)
	return injectedTag.MatchString(text) || strings.HasPrefix(text, "# AGENTS.md instructions")
}

// decodeCodex maps one Codex rollout line onto the Claude record shape, so the
// facts read both alike: a typed message is a user record and injected context
// a meta one, an assistant message, reasoning or tool call is an assistant
// record (a call as a tool_use block), and a tool output is a user record with
// a tool_result block. Developer messages and the other lines keep their own
// type and are no event; the turn events set Turn.
func decodeCodex(rec Record, payload json.RawMessage) (Record, error) {
	var p struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Name    string `json:"name"`
		CallID  string `json:"call_id"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		SessionID  string `json:"session_id"`
		CWD        string `json:"cwd"`
		Originator string `json:"originator"`
		Git        struct {
			Branch string `json:"branch"`
		} `json:"git"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return Record{}, err
	}
	switch rec.Type {
	case "session_meta":
		rec.SessionID, rec.CWD, rec.GitBranch, rec.Originator = p.SessionID, p.CWD, p.Git.Branch, p.Originator
	case "turn_context":
		rec.CWD = p.CWD
	case "event_msg":
		rec.Turn = map[string]string{"task_started": TurnStarted, "task_complete": TurnEnded, "turn_aborted": TurnAborted}[p.Type]
	case "response_item":
		switch p.Type {
		case "message":
			if p.Role != "user" && p.Role != "assistant" {
				break
			}
			rec.Type = p.Role
			if len(p.Content) > 0 {
				rec.Text = textStart(p.Content[0].Text)
			}
			rec.IsMeta = p.Role == "user" && CodexInjected(rec.Text)
		case "reasoning":
			rec.Type = "assistant"
		case "function_call", "custom_tool_call":
			rec.Type = "assistant"
			rec.Content = []Block{{Type: "tool_use", Name: p.Name, ToolUseID: p.CallID}}
		case "function_call_output", "custom_tool_call_output":
			rec.Type = "user"
			rec.Content = []Block{{Type: "tool_result", ToolUseID: p.CallID}}
		}
	}
	return rec, nil
}
