package transcript_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/transcript"
)

func TestDecodeAssistantWithToolUse(t *testing.T) {
	line := []byte(`{"type":"assistant","sessionId":"s1","cwd":"C:\\r","gitBranch":"main","uuid":"u2","parentUuid":"u1","isSidechain":false,"timestamp":"2026-10-21T09:15:00.123Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"x"},{"type":"tool_use","name":"Bash","id":"toolu_1"}]}}`)
	r, err := transcript.Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != "assistant" || r.SessionID != "s1" || r.CWD != `C:\r` || r.GitBranch != "main" {
		t.Fatalf("header: %+v", r)
	}
	if !r.Timestamp.Equal(time.Date(2026, 10, 21, 9, 15, 0, 123000000, time.UTC)) {
		t.Fatalf("ts %v", r.Timestamp)
	}
	var tu *transcript.Block
	for i := range r.Content {
		if r.Content[i].Type == "tool_use" {
			tu = &r.Content[i]
		}
	}
	if tu == nil || tu.Name != "Bash" || tu.ToolUseID != "toolu_1" {
		t.Fatalf("tool_use %+v", r.Content)
	}
}

func TestDecodeUserToolResult(t *testing.T) {
	line := []byte(`{"type":"user","sessionId":"s1","timestamp":"2026-10-21T09:15:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1"}]}}`)
	r, err := transcript.Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Content) != 1 || r.Content[0].Type != "tool_result" || r.Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("tool_result %+v", r.Content)
	}
}

func TestDecodeUserStringPromptHasNoBlocks(t *testing.T) {
	line := []byte(`{"type":"user","sessionId":"s1","timestamp":"2026-10-21T09:10:00Z","message":{"role":"user","content":"hi"}}`)
	r, err := transcript.Decode(line)
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != "user" || len(r.Content) != 0 {
		t.Fatalf("expected no blocks, got %+v", r.Content)
	}
}

// Text keeps the start of the message, string or first text block, so an
// injected message can be told from a typed prompt.
func TestDecodeKeepsTextStart(t *testing.T) {
	for line, want := range map[string]string{
		`{"type":"user","message":{"content":"hi there"}}`:                                                            "hi there",
		`{"type":"user","message":{"content":[{"type":"text","text":"<teammate-message id=\"x\">do it"}]}}`:           `<teammate-message id="x">do it`,
		`{"type":"user","message":{"content":"` + strings.Repeat("a", 100) + `"}}`:                                    strings.Repeat("a", 64),
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t"},{"type":"text","text":"x"}]}}`: "x",
	} {
		r, err := transcript.Decode([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		if r.Text != want {
			t.Errorf("%s: Text = %q, want %q", line, r.Text, want)
		}
	}
}

func TestDecodeBackgroundBashBlock(t *testing.T) {
	bg := []byte(`{"type":"assistant","timestamp":"2026-10-21T09:15:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1","input":{"run_in_background":true}}]}}`)
	r, err := transcript.Decode(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Content) != 1 || !r.Content[0].Background {
		t.Fatalf("run_in_background bash should decode Background=true: %+v", r.Content)
	}
	fg := []byte(`{"type":"assistant","timestamp":"2026-10-21T09:15:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t2"}]}}`)
	r2, err := transcript.Decode(fg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Content) != 1 || r2.Content[0].Background {
		t.Fatalf("plain bash should decode Background=false: %+v", r2.Content)
	}
}

func TestDecodeEveryFixtureLine(t *testing.T) {
	b, err := os.ReadFile("testdata/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for i, ln := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		if _, err := transcript.Decode(ln); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}
}

func TestDecodeTimingSignals(t *testing.T) {
	mk := func(line string) transcript.Record {
		r, err := transcript.Decode([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		return r
	}
	if r := mk(`{"type":"user","isMeta":true,"origin":{"kind":"peer","from":"x"},"timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"report"}}`); r.Origin != "peer" {
		t.Fatalf("origin: %+v", r)
	}
	if r := mk(`{"type":"pr-link","origin":"elsewhere","timestamp":"2026-10-21T09:00:00Z"}`); r.Origin != "" {
		t.Fatalf("a non-object origin must be ignored: %+v", r)
	}
	if r := mk(`{"type":"user","permissionDecision":{"decision":"accept","source":"user_temporary"},"toolUseResult":{"answers":{"q":"a"}},"timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`); r.PermissionSource != "user_temporary" || !r.Answered {
		t.Fatalf("permission/answers: %+v", r)
	}
	if r := mk(`{"type":"user","toolUseResult":"Error: no","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`); r.Answered || r.Answers != 0 {
		t.Fatalf("a string toolUseResult carries no answers: %+v", r)
	}
	// The shape of a real answer, with one entry per question.
	if r := mk(`{"type":"user","permissionDecision":{"decision":"accept","source":"user_temporary"},"toolUseResult":{"questions":[{"question":"Q1"},{"question":"Q2"}],"answers":{"Q1":"a","Q2":"b"},"annotations":{}},"timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`); r.Answers != 2 || !r.Answered {
		t.Fatalf("answers counted per question: %+v", r)
	}
	if r := mk(`{"type":"user","toolUseResult":{"answers":"yes"},"timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`); r.Answers != 1 {
		t.Fatalf("answers that aren't an object count once: %+v", r)
	}
	if r := mk(`{"type":"user","toolUseResult":{"content":"x"},"timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`); r.Answers != 0 || r.Answered {
		t.Fatalf("a result without answers has none: %+v", r)
	}
	r := mk(`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-10-21T09:10:00Z","content":"<task-notification>\n<task-id>b1</task-id>\n<tool-use-id>toolu_9</tool-use-id>\n<status>completed</status>\n</task-notification>"}`)
	if r.TaskToolUseID != "toolu_9" || r.TaskStatus != "completed" {
		t.Fatalf("task notification: %+v", r)
	}
	if r := mk(`{"type":"queue-operation","operation":"remove","timestamp":"2026-10-21T09:10:00Z","content":"<task-notification><tool-use-id>toolu_9</tool-use-id><status>completed</status></task-notification>"}`); r.TaskToolUseID != "" {
		t.Fatalf("only the enqueue marks completion: %+v", r)
	}
}

func TestDecodeReadsStopReason(t *testing.T) {
	r, err := transcript.Decode([]byte(`{"type":"assistant","timestamp":"2026-10-22T10:00:03Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`))
	if err != nil || r.StopReason != "end_turn" {
		t.Fatalf("got %q err %v", r.StopReason, err)
	}
	r, err = transcript.Decode([]byte(`{"type":"assistant","timestamp":"2026-10-22T10:00:03Z","message":{"role":"assistant","stop_reason":null,"content":[{"type":"text","text":"more"}]}}`))
	if err != nil || r.StopReason != "" {
		t.Fatalf("null: got %q err %v", r.StopReason, err)
	}
}

func TestDecodeReadsBackgroundTasksStartingAndStopping(t *testing.T) {
	launch, err := transcript.Decode([]byte(`{"type":"user","timestamp":"2026-11-06T10:00:01Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Command running in background with ID: b9muzg1pq."}]},"toolUseResult":{"stdout":"","backgroundTaskId":"b9muzg1pq"}}`))
	if err != nil || launch.BackgroundTaskID != "b9muzg1pq" {
		t.Fatalf("launch: %q %v", launch.BackgroundTaskID, err)
	}
	stop, err := transcript.Decode([]byte(`{"type":"assistant","timestamp":"2026-11-06T10:05:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_2","name":"TaskStop","input":{"task_id":"b9muzg1pq"}}]}}`))
	if err != nil || len(stop.Content) != 1 || stop.Content[0].TaskID != "b9muzg1pq" {
		t.Fatalf("stop: %+v %v", stop.Content, err)
	}
	old, err := transcript.Decode([]byte(`{"type":"assistant","timestamp":"2026-11-06T10:05:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_3","name":"KillShell","input":{"shell_id":"b2"}}]}}`))
	if err != nil || old.Content[0].TaskID != "b2" {
		t.Fatalf("an older stop: %+v %v", old.Content, err)
	}
}

// A task that finishes while the agent is idle comes back as a message, not queued.
func TestDecodeReadsACompletionMessage(t *testing.T) {
	r, err := transcript.Decode([]byte(`{"type":"user","timestamp":"2026-11-06T10:06:00Z","origin":{"kind":"task-notification"},"message":{"role":"user","content":"<task-notification>\n<task-id>b9muzg1pq</task-id>\n<tool-use-id>toolu_1</tool-use-id>\n<status>completed</status>\n</task-notification>"}}`))
	if err != nil || r.NotifiedToolUseID != "toolu_1" || r.NotifiedStatus != "completed" {
		t.Fatalf("got %q %q %v", r.NotifiedToolUseID, r.NotifiedStatus, err)
	}
	// It stays an ordinary record otherwise: not a queued completion.
	if r.TaskToolUseID != "" {
		t.Fatalf("queued id: %q", r.TaskToolUseID)
	}
}
