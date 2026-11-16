package facts_test

import (
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/transcript"
)

func rec(kind string, sec int, result bool) transcript.Record {
	ts := time.Date(2026, 10, 21, 9, 0, sec, 0, time.UTC)
	if kind == "assistant" {
		return transcript.Record{Type: "assistant", Timestamp: ts}
	}
	r := transcript.Record{Type: "user", Timestamp: ts}
	if result {
		r.Content = []transcript.Block{{Type: "tool_result", ToolUseID: "t1"}}
	}
	return r
}

func TestAIActiveSecondsGapRules(t *testing.T) {
	recs := []transcript.Record{
		rec("user", 0, false),        // human prompt
		rec("assistant", 2, false),   // +2  (later=ai)
		rec("user", 9, true),         // +7  (tool_result, later=ai) -> execution span counted
		rec("user", 300, false),      // human prompt: gap 291, later=human -> NOT counted
		rec("assistant", 305, false), // +5  (later=ai)
	}
	got, last := facts.AIActiveSeconds(recs, facts.Boundary{})
	if got != 14 {
		t.Fatalf("want 14s (2+7+5), got %v", got)
	}
	if !last.Valid || !last.IsAI {
		t.Fatalf("trailing boundary should be the ai assistant at 305: %+v", last)
	}
}

func TestAIActiveSecondsIdleCapExcludesLongGap(t *testing.T) {
	recs := []transcript.Record{rec("user", 0, false), rec("assistant", 1000, false)} // gap 1000 > 300
	if got, _ := facts.AIActiveSeconds(recs, facts.Boundary{}); got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}

func TestAIActiveSecondsSeedsAcrossBoundary(t *testing.T) {
	prev := facts.Boundary{Time: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC), IsAI: true, Valid: true}
	recs := []transcript.Record{rec("user", 4, true)} // tool_result 4s after prev; later=ai -> +4
	got, _ := facts.AIActiveSeconds(recs, prev)
	if got != 4 {
		t.Fatalf("want 4s across the checkpoint boundary, got %v", got)
	}
}

func TestByDaySplitsGapsByLaterEventsDay(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 21, h, m, s, 0, time.UTC) }
	prev := facts.Boundary{Time: at(23, 59, 50), IsAI: true, Valid: true}
	recs := []transcript.Record{
		{Type: "assistant", Timestamp: at(23, 59, 58)},                      // +8 on the 21st
		{Type: "user", IsMeta: true, Timestamp: at(23, 59, 59)},             // meta: not an event
		{Type: "assistant", Timestamp: at(23, 59, 58).Add(5 * time.Second)}, // +5 on the 22nd (gap straddles midnight)
		{Type: "user", Timestamp: at(23, 59, 58).Add(10 * time.Second)},     // human: no time, but an event of the 22nd
	}
	days, last := facts.ByDay(recs, prev, time.UTC)
	if len(days) != 2 {
		t.Fatalf("want 2 days, got %+v", days)
	}
	d1, d2 := days["2026-10-21"], days["2026-10-22"]
	if d1.AISeconds != 8 || !d1.First.Equal(at(23, 59, 58)) || !d1.Last.Equal(at(23, 59, 58)) {
		t.Fatalf("21st: %+v", d1)
	}
	if d2.AISeconds != 5 || !d2.First.Equal(at(23, 59, 58).Add(5*time.Second)) || !d2.Last.Equal(at(23, 59, 58).Add(10*time.Second)) {
		t.Fatalf("22nd: %+v", d2)
	}
	if !last.Valid || last.IsAI || !last.Time.Equal(at(23, 59, 58).Add(10*time.Second)) {
		t.Fatalf("trailing boundary: %+v", last)
	}
}

func TestByDayUsesLocation(t *testing.T) {
	loc := time.FixedZone("plus2", 2*3600)
	recs := []transcript.Record{rec("user", 0, false), rec("assistant", 3, false)} // 09:00 UTC = 11:00 plus2, still the 21st
	days, _ := facts.ByDay(recs, facts.Boundary{}, loc)
	if days["2026-10-21"].AISeconds != 3 {
		t.Fatalf("got %+v", days)
	}
	late := []transcript.Record{{Type: "assistant", Timestamp: time.Date(2026, 10, 21, 23, 0, 0, 0, time.UTC)}} // the 22nd in plus2
	if days, _ := facts.ByDay(late, facts.Boundary{}, loc); len(days) != 1 || days["2026-10-22"].Last.IsZero() {
		t.Fatalf("got %+v", days)
	}
}

func TestByDayBucketsByLocalHour(t *testing.T) {
	loc := time.FixedZone("plus2", 2*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 21, h, m, 0, 0, time.UTC) }
	recs := []transcript.Record{
		{Type: "user", Timestamp: at(8, 58)},
		{Type: "assistant", Timestamp: at(9, 1)}, // 180s ending 11:01 plus2
		{Type: "assistant", Timestamp: at(9, 2)}, // +60s, same hour
	}
	days, _ := facts.ByDay(recs, facts.Boundary{}, loc)
	h := days["2026-10-21"].Hours
	if h[11] != 240 || h[10] != 0 {
		t.Fatalf("hours: %v", h)
	}
}

func TestExtractCountsAnsweredQuestions(t *testing.T) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	got := facts.Extract([]transcript.Record{{Type: "user", Timestamp: at, Answered: true, Answers: 2, PermissionSource: "user_temporary", Content: []transcript.Block{{Type: "tool_result", ToolUseID: "t"}}}})
	if len(got) != 2 || got[0].Type != "decision" || got[1].Type != "decision" || !got[0].Timestamp.Equal(at) {
		t.Fatalf("want one decision per question answered at its time: %+v", got)
	}
}

func TestExtractDoesNotCountPermissionApprovals(t *testing.T) {
	got := facts.Extract([]transcript.Record{{Type: "user", Timestamp: time.Now(), PermissionSource: "user_temporary", Content: []transcript.Block{{Type: "tool_result", ToolUseID: "t"}}}})
	if len(got) != 0 {
		t.Fatalf("an approved permission is no decision: %+v", got)
	}
}

func TestExtractCountsPromptsTurnsTools(t *testing.T) {
	recs := []transcript.Record{
		{Type: "user", Timestamp: time.Now()},               // prompt
		{Type: "user", IsMeta: true, Timestamp: time.Now()}, // meta, not a prompt
		{Type: "assistant", Timestamp: time.Now(), Content: []transcript.Block{{Type: "tool_use", Name: "Bash", ToolUseID: "t1"}}},
		{Type: "user", Timestamp: time.Now(), Content: []transcript.Block{{Type: "tool_result", ToolUseID: "t1"}}}, // not a prompt
	}
	var p, turns, tools int
	for _, f := range facts.Extract(recs) {
		switch f.Type {
		case "prompt":
			p++
		case "assistant_turn":
			turns++
		case "tool_use":
			tools++
		}
	}
	if p != 1 || turns != 1 || tools != 1 {
		t.Fatalf("prompts=%d turns=%d tools=%d", p, turns, tools)
	}
}

// Messages the harness injects as user records are not prompts the user typed.
func TestIsPromptSkipsInjectedMessages(t *testing.T) {
	for _, text := range []string{
		`<task-notification><task-id>x</task-id>`,
		`<teammate-message teammate_id="lead">go</teammate-message>`,
		`<system-reminder>x</system-reminder>`,
		`<command-name>/clear</command-name>`,
		`<local-command-stdout>ok</local-command-stdout>`,
	} {
		if facts.IsPrompt(transcript.Record{Type: "user", Text: text}) {
			t.Errorf("injected message counted as a prompt: %q", text)
		}
	}
	if !facts.IsPrompt(transcript.Record{Type: "user", Text: "fix the <system-reminder> parser"}) {
		t.Error("a typed prompt mentioning a tag is still a prompt")
	}
}

// Agents reporting back to a lead session and the summary a compaction writes
// arrive as plain user records; only what the human typed is a prompt.
func TestIsPromptSkipsAgentReportsAndCompaction(t *testing.T) {
	for _, r := range []transcript.Record{
		{Type: "user", Text: "Another Claude session sent a message:\n<teammate-message"},
		{Type: "user", Text: "This session is being continued from a previous conversation that ran out"},
		{Type: "user", Origin: "peer", Text: "done, all green"},
		{Type: "user", Origin: "task-notification", Text: "finished"},
	} {
		if facts.IsPrompt(r) {
			t.Errorf("not typed by the human, but counted as a prompt: %+v", r)
		}
	}
	for _, r := range []transcript.Record{
		{Type: "user", Origin: "human", Text: "ok, implement it"},
		{Type: "user", Text: "an older transcript has no origin"},
	} {
		if !facts.IsPrompt(r) {
			t.Errorf("a typed prompt was not counted: %+v", r)
		}
	}
}

func TestIsBackgroundDispatch(t *testing.T) {
	cases := []struct {
		b    transcript.Block
		want bool
	}{
		{transcript.Block{Type: "tool_use", Name: "Bash", Background: true}, true},
		{transcript.Block{Type: "tool_use", Name: "Agent"}, true},
		{transcript.Block{Type: "tool_use", Name: "Task"}, true},
		{transcript.Block{Type: "tool_use", Name: "Bash"}, false},
		{transcript.Block{Type: "text"}, false},
	}
	for i, c := range cases {
		if got := facts.IsBackgroundDispatch(c.b); got != c.want {
			t.Fatalf("case %d (%+v): want %v got %v", i, c.b, c.want, got)
		}
	}
}

func TestTailKind(t *testing.T) {
	mk := func(line string) transcript.Record {
		r, err := transcript.Decode([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	asstTool := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"}]}}`)
	asstText := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:03Z","message":{"role":"assistant","content":[{"type":"text"}]}}`)
	toolRes := mk(`{"type":"user","timestamp":"2026-10-22T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}`)
	prompt := mk(`{"type":"user","timestamp":"2026-10-22T10:00:00Z","message":{"role":"user","content":"go"}}`)
	meta := mk(`{"type":"user","isMeta":true,"timestamp":"2026-10-22T10:00:09Z","message":{"role":"user","content":"caveat"}}`)
	sys := mk(`{"type":"system","timestamp":"2026-10-22T10:00:09Z"}`)

	// newest real event wins
	if got := facts.TailKind([]transcript.Record{prompt, asstTool}, ""); got != facts.TailAIWorking {
		t.Fatalf("tool_use -> working, got %q", got)
	}
	if got := facts.TailKind([]transcript.Record{asstTool, asstText}, ""); got != facts.TailAIDone {
		t.Fatalf("text turn -> done, got %q", got)
	}
	if got := facts.TailKind([]transcript.Record{asstText, toolRes}, ""); got != facts.TailAIWorking {
		t.Fatalf("tool_result -> working, got %q", got)
	}
	if got := facts.TailKind([]transcript.Record{prompt}, ""); got != facts.TailHuman {
		t.Fatalf("prompt -> human, got %q", got)
	}
	// meta and non-event records are ignored; prev is carried
	if got := facts.TailKind([]transcript.Record{meta, sys}, facts.TailAIDone); got != facts.TailAIDone {
		t.Fatalf("no real event -> prev, got %q", got)
	}
	// a skippable meta record is the newest by timestamp; the real event must still win
	if got := facts.TailKind([]transcript.Record{asstText, meta}, ""); got != facts.TailAIDone {
		t.Fatalf("newest is meta; real event wins -> ai_done, got %q", got)
	}
	if got := facts.TailKind(nil, facts.TailHuman); got != facts.TailHuman {
		t.Fatalf("empty -> prev, got %q", got)
	}
}

func TestTailKindBackgroundDispatch(t *testing.T) {
	mk := func(line string) transcript.Record {
		r, err := transcript.Decode([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	prompt := mk(`{"type":"user","timestamp":"2026-10-22T10:00:00Z","message":{"role":"user","content":"go"}}`)
	dispatch := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Agent","id":"a1"}]}}`)
	toolRes := mk(`{"type":"user","timestamp":"2026-10-22T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1"}]}}`)
	text := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:05Z","message":{"role":"assistant","content":[{"type":"text"}]}}`)

	// a background dispatch in this turn-cluster, then an end-of-turn text:
	// the agent is still working, not waiting on you.
	if got := facts.TailKind([]transcript.Record{prompt, dispatch, toolRes, text}, ""); got != facts.TailAIDone {
		t.Fatalf("background dispatch then text -> ai_done: the agent handed back, got %q", got)
	}
	// control: a foreground tool then text is a genuine handoff.
	fgBash := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"b1"}]}}`)
	fgRes := mk(`{"type":"user","timestamp":"2026-10-22T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1"}]}}`)
	if got := facts.TailKind([]transcript.Record{prompt, fgBash, fgRes, text}, ""); got != facts.TailAIDone {
		t.Fatalf("foreground tool then text -> ai_done, got %q", got)
	}
}

func TestTailKindBackgroundDispatchResumed(t *testing.T) {
	mk := func(line string) transcript.Record {
		r, err := transcript.Decode([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// A sub-agent was dispatched (a1), its launch handle returned (t3), the agent
	// said "started it" (t4) - but then activity continued: the sub-agent finished
	// and the agent resumed, consuming another result (t6) and giving a final
	// answer (t8). The agent has handed back to you, so this is "waiting", not a
	// still-running background dispatch. (The a1 handle is the launch, not the
	// completion; what proves the work is over is the activity AFTER that handle.)
	prompt := mk(`{"type":"user","timestamp":"2026-10-22T10:00:00Z","message":{"role":"user","content":"go"}}`)
	dispatch := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Agent","id":"a1"}]}}`)
	handle := mk(`{"type":"user","timestamp":"2026-10-22T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1"}]}}`)
	started := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:04Z","message":{"role":"assistant","content":[{"type":"text"}]}}`)
	resumed := mk(`{"type":"user","timestamp":"2026-10-22T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b2"}]}}`)
	final := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:08Z","message":{"role":"assistant","content":[{"type":"text"}]}}`)
	if got := facts.TailKind([]transcript.Record{prompt, dispatch, handle, started, resumed, final}, ""); got != facts.TailAIDone {
		t.Fatalf("a completed-and-resumed background dispatch -> ai_done, got %q", got)
	}
}

func TestAIActiveSecondsPeerMessageIsAnEvent(t *testing.T) {
	peer := func(sec int) transcript.Record {
		return transcript.Record{Type: "user", IsMeta: true, Origin: "peer", Timestamp: time.Date(2026, 10, 21, 9, 0, sec, 0, time.UTC)}
	}
	// The lead waits 100s for a teammate (the teammate's own time), then
	// answers in 3s. Only the 3s is the lead's.
	if got, _ := facts.AIActiveSeconds([]transcript.Record{rec("assistant", 0, false), peer(100), rec("assistant", 103, false)}, facts.Boundary{}); got != 3 {
		t.Fatalf("short wait: want 3s, got %v", got)
	}
	// A wait over the idle cap must not lose the answer after it.
	if got, _ := facts.AIActiveSeconds([]transcript.Record{rec("assistant", 0, false), peer(400), rec("assistant", 405, false)}, facts.Boundary{}); got != 5 {
		t.Fatalf("long wait: want 5s, got %v", got)
	}
}

func TestAIActiveSecondsHumanAnswersAreNotAgentTime(t *testing.T) {
	answer := func(mod func(*transcript.Record)) []transcript.Record {
		r := rec("user", 60, true)
		mod(&r)
		return []transcript.Record{rec("assistant", 0, false), r, rec("assistant", 62, false)}
	}
	cases := []struct {
		name string
		mod  func(*transcript.Record)
		want float64
	}{
		{"AskUserQuestion answer", func(r *transcript.Record) { r.Answered = true }, 2},
		{"permission the human approved", func(r *transcript.Record) { r.PermissionSource = "user_temporary" }, 2},
		{"permission from a saved rule", func(r *transcript.Record) { r.PermissionSource = "user_permanent" }, 62},
	}
	for _, c := range cases {
		if got, _ := facts.AIActiveSeconds(answer(c.mod), facts.Boundary{}); got != c.want {
			t.Fatalf("%s: want %vs, got %v", c.name, c.want, got)
		}
	}
}

// bgFixture: a background Bash launched at 0 (handle at 2, turn ends at 3),
// a human prompt at 5000, and the given completion records.
func bgFixture(done ...transcript.Record) []transcript.Record {
	at := func(sec int) time.Time {
		return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second)
	}
	recs := []transcript.Record{
		{Type: "assistant", Timestamp: at(0), Content: []transcript.Block{{Type: "tool_use", Name: "Bash", ToolUseID: "b1", Background: true}}},
		{Type: "user", Timestamp: at(2), Content: []transcript.Block{{Type: "tool_result", ToolUseID: "b1"}}},
		{Type: "assistant", Timestamp: at(3)},
		{Type: "user", Timestamp: at(5000)},
	}
	return append(recs, done...)
}

func taskDone(sec int, id, status string) transcript.Record {
	return transcript.Record{Type: "queue-operation", Timestamp: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second), TaskToolUseID: id, TaskStatus: status}
}

func TestAIActiveSecondsCountsBackgroundTaskUntilItCompletes(t *testing.T) {
	cases := []struct {
		name string
		recs []transcript.Record
		want float64
	}{
		{"completed: launch to completion, the turn's own 3s inside it", bgFixture(taskDone(600, "b1", "completed")), 600},
		{"failed still ran on its own", bgFixture(taskDone(600, "b1", "failed")), 600},
		{"killed by the agent", bgFixture(taskDone(600, "b1", "killed")), 3},
		{"never completes", bgFixture(), 3},
		{"capped at an hour", bgFixture(taskDone(7200, "b1", "completed")), 3600},
		{"another task's completion", bgFixture(taskDone(600, "zz", "completed")), 3},
	}
	for _, c := range cases {
		if got, _ := facts.AIActiveSeconds(c.recs, facts.Boundary{}); got != c.want {
			t.Fatalf("%s: want %v, got %v", c.name, c.want, got)
		}
	}
}

func TestAIActiveSecondsBackgroundIsAUnion(t *testing.T) {
	at := func(sec int) time.Time {
		return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second)
	}
	recs := append(bgFixture(taskDone(600, "b1", "completed"), taskDone(700, "m1", "completed")),
		// a Monitor launched at 100 while b1 runs, and a background sub-agent
		// whose time is in its own transcript
		transcript.Record{Type: "assistant", Timestamp: at(100), Content: []transcript.Block{{Type: "tool_use", Name: "Monitor", ToolUseID: "m1"}, {Type: "tool_use", Name: "Agent", ToolUseID: "a1", Background: true}}},
		transcript.Record{Type: "user", Timestamp: at(101), Content: []transcript.Block{{Type: "tool_result", ToolUseID: "m1"}}},
		taskDone(900, "a1", "completed"),
	)
	if got, _ := facts.AIActiveSeconds(recs, facts.Boundary{}); got != 700 {
		t.Fatalf("want the union 0-700, got %v", got)
	}
}

func TestByDayBackgroundAcrossBatches(t *testing.T) {
	all := bgFixture(taskDone(600, "b1", "completed"))
	first, rest := all[:3], all[3:]
	_, seed := facts.AIActiveSeconds(first, facts.Boundary{})
	got, _ := facts.AIActiveSeconds(rest, seed)
	if got != 597 {
		t.Fatalf("second batch: want the 597s not yet counted, got %v", got)
	}
	// ByDay from the same seed agrees, and the seed is not changed by use.
	days, _ := facts.ByDay(rest, seed, time.UTC)
	if d := days["2026-10-21"]; d.AISeconds != 597 {
		t.Fatalf("day: %+v", days)
	}
	if again, _ := facts.AIActiveSeconds(rest, seed); again != 597 {
		t.Fatalf("seed was changed by use: %v", again)
	}
}

// Measured on real transcripts: a text record with stop_reason tool_use is always
// followed by its tool call, up to 48 seconds later, and end_turn never is.
func TestTailKindReadsTheStopReason(t *testing.T) {
	text := func(stop string) transcript.Record {
		t.Helper()
		sr := `null`
		if stop != "" {
			sr = `"` + stop + `"`
		}
		r, err := transcript.Decode([]byte(`{"type":"assistant","timestamp":"2026-10-22T10:00:03Z","message":{"role":"assistant","stop_reason":` + sr + `,"content":[{"type":"text","text":"x"}]}}`))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for stop, want := range map[string]string{
		"end_turn": facts.TailAIEnded, "stop_sequence": facts.TailAIEnded,
		"tool_use": facts.TailAIWorking,
		"":         facts.TailAIDone,
	} {
		if got := facts.TailKind([]transcript.Record{text(stop)}, ""); got != want {
			t.Errorf("stop reason %q: got %q, want %q", stop, got, want)
		}
	}
}

func TestTailKindHandsBackPastBackgroundWork(t *testing.T) {
	mk := func(s string) transcript.Record {
		t.Helper()
		r, err := transcript.Decode([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	dispatch := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"a1","name":"Bash","input":{"run_in_background":true}}]}}`)
	res := mk(`{"type":"user","timestamp":"2026-10-22T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1"}]}}`)
	end := mk(`{"type":"assistant","timestamp":"2026-10-22T10:00:05Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"x"}]}}`)
	if got := facts.TailKind([]transcript.Record{dispatch, res, end}, ""); got != facts.TailAIEnded {
		t.Fatalf("got %q", got)
	}
}

func bgDecode(t *testing.T, s string) transcript.Record {
	t.Helper()
	r, err := transcript.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// The agent ended its turn: it waits on you, whatever it left running.
func TestTailKindHandsBackWhateverRunsInTheBackground(t *testing.T) {
	prompt := bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:00:00Z","message":{"role":"user","content":"go"}}`)
	dispatch := bgDecode(t, `{"type":"assistant","timestamp":"2026-11-06T10:00:02Z","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"run_in_background":true}}]}}`)
	handle := bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]},"toolUseResult":{"backgroundTaskId":"b1"}}`)
	end := bgDecode(t, `{"type":"assistant","timestamp":"2026-11-06T10:00:05Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"x"}]}}`)
	for k := 1; k <= 4; k++ {
		recs := []transcript.Record{prompt, dispatch, handle, end}[4-k:]
		if got := facts.TailKind(recs, ""); got != facts.TailAIEnded {
			t.Errorf("a batch of the last %d records: %q", k, got)
		}
	}
}

func TestOpenBackgroundTasksCloseWhenStoppedOrFinished(t *testing.T) {
	launch := func(id, task, at string) []transcript.Record {
		return []transcript.Record{
			bgDecode(t, `{"type":"assistant","timestamp":"`+at+`","message":{"role":"assistant","content":[{"type":"tool_use","id":"`+id+`","name":"Bash","input":{"run_in_background":true}}]}}`),
			bgDecode(t, `{"type":"user","timestamp":"`+at+`","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"`+id+`"}]},"toolUseResult":{"backgroundTaskId":"`+task+`"}}`),
		}
	}
	// Each batch alone, carrying the boundary, as ingest reads them.
	_, b := facts.AIActiveSeconds(launch("t1", "b1", "2026-11-06T10:00:00Z"), facts.Boundary{})
	_, b = facts.AIActiveSeconds(launch("t2", "b2", "2026-11-06T10:01:00Z"), b)
	if len(b.Pending) != 2 {
		t.Fatalf("both started: %v", b.Pending)
	}
	stop := bgDecode(t, `{"type":"assistant","timestamp":"2026-11-06T10:02:00Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t3","name":"TaskStop","input":{"task_id":"b1"}}]}}`)
	_, b = facts.AIActiveSeconds([]transcript.Record{stop}, b)
	if _, ok := b.Pending["t1"]; ok || len(b.Pending) != 1 {
		t.Fatalf("a stopped one closes: %v", b.Pending)
	}
	done := bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:03:00Z","origin":{"kind":"task-notification"},"message":{"role":"user","content":"<task-notification><tool-use-id>t2</tool-use-id><status>completed</status></task-notification>"}}`)
	_, b = facts.AIActiveSeconds([]transcript.Record{done}, b)
	if len(b.Pending) != 0 || len(b.Tasks) != 0 {
		t.Fatalf("a finished one closes: %v %v", b.Pending, b.Tasks)
	}
}

// The timer of a running task counts from when it started: a prompt, or the agent
// picking up again on a background task's news, and never before its last hand-back.
func TestExtractMarksWhereATaskCanStart(t *testing.T) {
	recs := []transcript.Record{
		bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:00:00Z","message":{"role":"user","content":"go"}}`),
		bgDecode(t, `{"type":"assistant","timestamp":"2026-11-06T10:01:00Z","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"x"}]}}`),
		bgDecode(t, `{"type":"assistant","timestamp":"2026-11-06T10:02:00Z","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"x"}]}}`),
		bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:08:00Z","origin":{"kind":"task-notification"},"message":{"role":"user","content":"<task-notification><tool-use-id>t1</tool-use-id><status>completed</status></task-notification>"}}`),
		bgDecode(t, `{"type":"user","timestamp":"2026-11-06T10:09:00Z","origin":{"kind":"peer"},"message":{"role":"user","content":"report"}}`),
	}
	got := map[string][]string{}
	for _, f := range facts.Extract(recs) {
		got[f.Type] = append(got[f.Type], f.Timestamp.Format("15:04"))
	}
	if len(got["prompt"]) != 1 || got["prompt"][0] != "10:00" {
		t.Errorf("prompt: %v", got["prompt"])
	}
	if len(got["turn_end"]) != 1 || got["turn_end"][0] != "10:02" {
		t.Errorf("turn_end: %v", got["turn_end"])
	}
	if len(got["resume"]) != 2 || got["resume"][0] != "10:08" || got["resume"][1] != "10:09" {
		t.Errorf("resume: %v", got["resume"])
	}
}
