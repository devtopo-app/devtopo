// Package transcript decodes Claude Code transcript JSONL records.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Block is one content block of a user or assistant message. For a tool_use
// block ToolUseID is the block's own id; for a tool_result block it is the
// tool_use_id it refers to. The two carry the same value, which links a call
// to its result.
type Block struct {
	Type      string
	Name      string
	ToolUseID string
	// Background is true for a tool_use block whose input sets
	// run_in_background, so the status core can tell a dispatched background
	// task from a foreground one.
	Background bool
	// TaskID is the background task a tool_use stops (TaskStop's task_id, or an
	// older KillShell's shell_id).
	TaskID string
}

// Record is one decoded transcript line. Only the fields the capture core uses
// are kept; unknown fields in the JSON are ignored.
type Record struct {
	Type        string
	SessionID   string
	CWD         string
	GitBranch   string
	UUID        string
	ParentUUID  string
	Timestamp   time.Time
	IsSidechain bool
	IsMeta      bool
	Content     []Block
	// StopReason is message.stop_reason on an assistant record: "end_turn" when the
	// turn ended, "tool_use" when a tool call follows, "" when not recorded.
	StopReason string
	// Origin is origin.kind: who sent a user record ("human", "peer" for a
	// teammate or sub-agent hand-back, "task-notification").
	Origin string
	// PermissionSource is permissionDecision.source on a tool result:
	// "user_temporary" when the human was asked to approve the call.
	PermissionSource string
	// Answered is true for a tool result carrying the human's answers (an
	// AskUserQuestion result).
	Answered bool
	// Answers is how many questions those answers cover.
	Answers int
	// TaskToolUseID and TaskStatus come from a queue-operation enqueue of a
	// <task-notification>: the background task launched by that tool_use
	// ended with that status (completed, failed, killed).
	TaskToolUseID string
	TaskStatus    string
	// BackgroundTaskID is the task id a background launch's result names, which
	// a later TaskStop refers to.
	BackgroundTaskID string
	// NotifiedToolUseID and NotifiedStatus come from a task-notification that
	// arrives as a message, when the agent was idle: the background task that
	// tool_use launched has ended. Unlike a queued one, the record stays an event.
	NotifiedToolUseID string
	NotifiedStatus    string
	// Text is the first textMax bytes of the message text (string content, or
	// the first text block), enough to recognise an injected message.
	Text string
	// Turn is set on a Codex turn event: TurnStarted, TurnEnded or TurnAborted.
	Turn string
	// Originator is the Codex client that wrote a rollout, from its
	// session_meta ("Codex Desktop" for the app).
	Originator string
	// ContinuedIn is set on a continued-in record, which ends a transcript Claude
	// Code continued under a new session id (as when a session is sent to the
	// background): the new session's id. Its transcript starts with a copy of
	// this one's history.
	ContinuedIn string
}

const textMax = 64

func textStart(s string) string {
	if len(s) > textMax {
		return s[:textMax]
	}
	return s
}

// Decode parses one JSONL line into a Record. It maps the camelCase header
// fields, parses the RFC3339 timestamp, and flattens message.content into
// Content only when that content is a JSON array (string content, a typed
// prompt, yields no blocks).
func Decode(line []byte) (Record, error) {
	var raw struct {
		Type        string          `json:"type"`
		SessionID   string          `json:"sessionId"`
		CWD         string          `json:"cwd"`
		GitBranch   string          `json:"gitBranch"`
		UUID        string          `json:"uuid"`
		ParentUUID  string          `json:"parentUuid"`
		Timestamp   string          `json:"timestamp"`
		IsSidechain bool            `json:"isSidechain"`
		IsMeta      bool            `json:"isMeta"`
		Message     json.RawMessage `json:"message"`
		Origin      json.RawMessage `json:"origin"`
		Permission  json.RawMessage `json:"permissionDecision"`
		ToolResult  json.RawMessage `json:"toolUseResult"`
		Operation   string          `json:"operation"`
		Content     json.RawMessage `json:"content"`
		Payload     json.RawMessage `json:"payload"`
		ContinuedIn string          `json:"continuedInSessionId"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return Record{}, err
	}

	rec := Record{
		Type:        raw.Type,
		SessionID:   raw.SessionID,
		CWD:         raw.CWD,
		GitBranch:   raw.GitBranch,
		UUID:        raw.UUID,
		ParentUUID:  raw.ParentUUID,
		IsSidechain: raw.IsSidechain,
		IsMeta:      raw.IsMeta,
		ContinuedIn: raw.ContinuedIn,
	}

	// These fields take several shapes across record types; one that does not
	// fit just leaves its field empty.
	var origin struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(raw.Origin, &origin)
	rec.Origin = origin.Kind
	var perm struct {
		Source string `json:"source"`
	}
	_ = json.Unmarshal(raw.Permission, &perm)
	rec.PermissionSource = perm.Source
	var result struct {
		Answers          json.RawMessage `json:"answers"`
		BackgroundTaskID string          `json:"backgroundTaskId"`
	}
	_ = json.Unmarshal(raw.ToolResult, &result)
	rec.BackgroundTaskID = result.BackgroundTaskID
	rec.Answered = len(result.Answers) > 0
	if rec.Answered {
		var byQuestion map[string]json.RawMessage
		if json.Unmarshal(result.Answers, &byQuestion) == nil {
			rec.Answers = len(byQuestion)
		} else {
			rec.Answers = 1
		}
	}
	if raw.Type == "queue-operation" && raw.Operation == "enqueue" {
		var text string
		_ = json.Unmarshal(raw.Content, &text)
		if strings.HasPrefix(strings.TrimSpace(text), "<task-notification>") {
			rec.TaskToolUseID, rec.TaskStatus = tag(text, "tool-use-id"), tag(text, "status")
		}
	}

	if raw.Timestamp != "" {
		ts, err := time.Parse(time.RFC3339, raw.Timestamp)
		if err != nil {
			return Record{}, fmt.Errorf("timestamp %q: %w", raw.Timestamp, err)
		}
		rec.Timestamp = ts.UTC()
	}
	if len(raw.Payload) > 0 {
		return decodeCodex(rec, raw.Payload) // a Codex rollout line
	}

	if len(raw.Message) > 0 {
		var msg struct {
			Content    json.RawMessage `json:"content"`
			StopReason string          `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw.Message, &msg); err != nil {
			return Record{}, err
		}
		rec.StopReason = msg.StopReason
		content := bytes.TrimSpace(msg.Content)
		if len(content) > 0 && content[0] == '"' {
			var s string
			if err := json.Unmarshal(content, &s); err != nil {
				return Record{}, err
			}
			rec.Text = textStart(s)
			rec.notified(s)
		}
		if len(content) > 0 && content[0] == '[' {
			var blocks []struct {
				Type      string          `json:"type"`
				Name      string          `json:"name"`
				ID        string          `json:"id"`
				ToolUseID string          `json:"tool_use_id"`
				Input     json.RawMessage `json:"input"`
				Text      string          `json:"text"`
			}
			if err := json.Unmarshal(content, &blocks); err != nil {
				return Record{}, err
			}
			for _, b := range blocks {
				if b.Type == "text" && rec.Text == "" {
					rec.Text = textStart(b.Text)
				}
				if b.Type == "text" {
					rec.notified(b.Text)
				}
				id := b.ToolUseID
				if id == "" {
					id = b.ID
				}
				block := Block{Type: b.Type, Name: b.Name, ToolUseID: id}
				if b.Type == "tool_use" && len(b.Input) > 0 {
					var in struct {
						RunInBackground bool   `json:"run_in_background"`
						TaskID          string `json:"task_id"`
						ShellID         string `json:"shell_id"`
					}
					// A malformed or absent input just leaves Background false.
					_ = json.Unmarshal(b.Input, &in)
					block.Background = in.RunInBackground
					if b.Name == "TaskStop" || b.Name == "KillShell" || b.Name == "KillBash" {
						block.TaskID = in.TaskID
						if block.TaskID == "" {
							block.TaskID = in.ShellID
						}
					}
				}
				rec.Content = append(rec.Content, block)
			}
		}
	}

	return rec, nil
}

// notified reads a task-notification message: which background launch ended, and how.
func (r *Record) notified(text string) {
	if r.Origin == "task-notification" && r.NotifiedToolUseID == "" && strings.Contains(text, "<task-notification>") {
		r.NotifiedToolUseID, r.NotifiedStatus = tag(text, "tool-use-id"), tag(text, "status")
	}
}

// tag returns the trimmed text of the first <name>...</name> in s, or "".
func tag(s, name string) string {
	_, after, ok := strings.Cut(s, "<"+name+">")
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(after, "</"+name+">")
	return strings.TrimSpace(v)
}
