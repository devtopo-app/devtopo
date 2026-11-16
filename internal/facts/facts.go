// Package facts extracts activity facts from transcript records and derives AI
// working time from their timestamps.
package facts

import (
	"sort"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/transcript"
)

// IdleCapSeconds bounds a single activity gap. A gap longer than this is the
// session being paused, not the agent working, and is not counted.
const IdleCapSeconds = 300

// Fact is one unit of activity kept so counts can show detail later.
type Fact struct {
	Type      string // "prompt", "decision", "assistant_turn", or "tool_use"
	Timestamp time.Time
	ToolName  string // set for "tool_use"
}

// BackgroundCapSeconds bounds the time one background task can add, so a
// stuck job cannot inflate a day.
const BackgroundCapSeconds = 3600

// pendingMaxAge drops a background launch that has not completed after a day:
// a dev server or watcher that never ends adds nothing.
const pendingMaxAge = 24 * time.Hour

// Boundary is the state of a transcript at the end of a batch, carried across
// an incremental checkpoint: the last event, so a gap that straddles the
// checkpoint is still measured, and the background tasks still running.
type Boundary struct {
	Time  time.Time
	IsAI  bool
	Valid bool
	// Pending maps a running background task's tool_use id to its launch.
	Pending map[string]time.Time
	// Tasks maps a pending task's own id, which TaskStop names, to its tool_use id.
	Tasks map[string]string
	// Spans is the time already counted that a pending task or the next gap
	// can still overlap, so nothing is counted twice.
	Spans []Span
}

// Span is a stretch of counted time.
type Span struct{ From, To time.Time }

func hasToolResult(r transcript.Record) bool {
	for _, b := range r.Content {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

// IsAIEvent reports whether a record is agent-produced: an assistant turn, or a
// user record carrying a tool result (the agent's execution coming back) that
// is not the human's own reply - an AskUserQuestion answer or a permission the
// human was asked to approve. The wait before such a reply is the human's.
func IsAIEvent(r transcript.Record) bool {
	if r.Type == "assistant" {
		return true
	}
	return r.Type == "user" && hasToolResult(r) && !r.Answered && r.PermissionSource != "user_temporary"
}

// injectedPrefixes start the text of user records the harness injects (agent
// and teammate notifications and reports, reminders, slash command echoes, the
// summary a compaction writes), not typed.
var injectedPrefixes = []string{
	"<task-notification", "<teammate-message", "<system-reminder", "<command-", "<local-command",
	"Another Claude session sent a message", "This session is being continued from a previous conversation",
}

// Injected reports whether a user record's text is one the harness injected, not
// one the human typed.
func Injected(text string) bool {
	for _, p := range injectedPrefixes {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// IsPrompt reports whether a record is a human prompt: a non-meta user record
// with no tool result, sent by the human when the record says who sent it,
// whose text is not an injected message. Older transcripts carry no origin.
func IsPrompt(r transcript.Record) bool {
	if r.Type != "user" || r.IsMeta || hasToolResult(r) || (r.Origin != "" && r.Origin != "human") {
		return false
	}
	return !Injected(r.Text)
}

// Extract emits one prompt fact per human prompt, one decision fact per question
// the human answered, one assistant_turn fact per assistant record, and one
// tool_use fact per tool_use block. An approved permission is not a decision.
// It also marks where a task can start, for the timer of a running one: a
// resume fact when the agent is handed a background task's news or a teammate's
// report, and a turn_end fact when an assistant record ends its turn.
func Extract(records []transcript.Record) []Fact {
	var out []Fact
	for _, r := range records {
		switch {
		case IsPrompt(r):
			out = append(out, Fact{Type: "prompt", Timestamp: r.Timestamp})
		case r.Type == "user" && !r.IsMeta && !hasToolResult(r) && (r.Origin == "task-notification" || r.Origin == "peer"):
			out = append(out, Fact{Type: "resume", Timestamp: r.Timestamp})
		case r.Type == "user" && r.Answers > 0:
			for range r.Answers {
				out = append(out, Fact{Type: "decision", Timestamp: r.Timestamp})
			}
		case r.Type == "assistant":
			out = append(out, Fact{Type: "assistant_turn", Timestamp: r.Timestamp})
			if r.StopReason != "" && r.StopReason != "tool_use" {
				out = append(out, Fact{Type: "turn_end", Timestamp: r.Timestamp})
			}
			for _, b := range r.Content {
				if b.Type == "tool_use" {
					out = append(out, Fact{Type: "tool_use", Timestamp: r.Timestamp, ToolName: b.Name})
				}
			}
		}
	}
	return out
}

// isBackgroundTask reports whether a block launches a shell task that runs on
// its own and reports completion back. A background sub-agent is not one: its
// time is in its own transcript.
func isBackgroundTask(b transcript.Block) bool {
	return b.Type == "tool_use" && (b.Name == "Monitor" || (b.Background && (b.Name == "Bash" || b.Name == "PowerShell")))
}

type item struct {
	r     transcript.Record
	event bool // a real user/assistant event; otherwise a task completion
}

// walk goes through the real user/assistant events and the background task
// completions in time order, seeded by prev, and counts the agent's time: a gap
// between consecutive events that is positive, no longer than IdleCapSeconds,
// and whose later event is agent-produced; and a background task that
// completed or failed on its own, from its launch to its completion, at most
// BackgroundCapSeconds. Time already counted is not counted again. onEvent gets
// each event's time, onTime each newly counted stretch's end and length. It
// returns the trailing Boundary.
func walk(records []transcript.Record, prev Boundary, onEvent func(time.Time), onTime func(end time.Time, secs float64)) Boundary {
	var items []item
	for _, r := range records {
		switch {
		case r.TaskToolUseID != "":
			items = append(items, item{r: r})
		case r.Type != "user" && r.Type != "assistant":
		case r.Type == "user" && r.IsMeta && !hasToolResult(r) && r.Origin != "peer":
			// injected meta (caveats, command output): not a real event
		default:
			// A peer hand-back is an event that is not this agent's: the wait
			// before it is the teammate's time, counted in its own transcript.
			items = append(items, item{r: r, event: true})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].r.Timestamp.Before(items[j].r.Timestamp) })

	last := prev
	last.Pending = map[string]time.Time{}
	for id, t := range prev.Pending {
		last.Pending[id] = t
	}
	last.Tasks = map[string]string{}
	for task, id := range prev.Tasks {
		last.Tasks[task] = id
	}
	last.Spans = append([]Span(nil), prev.Spans...)
	count := func(s Span) {
		for _, u := range uncovered(s, last.Spans) {
			onTime(u.To, u.To.Sub(u.From).Seconds())
		}
		last.Spans = merge(last.Spans, s)
	}

	for _, it := range items {
		t := it.r.Timestamp
		if !it.event {
			launch, ok := last.Pending[it.r.TaskToolUseID]
			delete(last.Pending, it.r.TaskToolUseID)
			if ok && (it.r.TaskStatus == "completed" || it.r.TaskStatus == "failed") {
				end := launch.Add(BackgroundCapSeconds * time.Second)
				if t.Before(end) {
					end = t
				}
				count(Span{launch, end})
			}
			continue
		}
		ai := IsAIEvent(it.r)
		if last.Valid {
			if g := t.Sub(last.Time).Seconds(); g > 0 && g <= IdleCapSeconds && ai {
				count(Span{last.Time, t})
			}
		}
		onEvent(t)
		for _, b := range it.r.Content {
			switch {
			case isBackgroundTask(b):
				last.Pending[b.ToolUseID] = t
			case b.TaskID != "":
				// The agent stopped it: it is no longer running.
				delete(last.Pending, last.Tasks[b.TaskID])
				delete(last.Tasks, b.TaskID)
			case b.Type == "tool_result" && it.r.BackgroundTaskID != "":
				if _, ok := last.Pending[b.ToolUseID]; ok {
					last.Tasks[it.r.BackgroundTaskID] = b.ToolUseID
				}
			}
		}
		// It ended while the agent was idle; a queued completion counts its time.
		delete(last.Pending, it.r.NotifiedToolUseID)
		last.Time, last.IsAI, last.Valid = t, ai, true
	}

	// Keep only what a later batch can still need: launches younger than
	// pendingMaxAge, their task ids, and the spans from the oldest of them or the
	// last event.
	cutoff := last.Time
	for id, t := range last.Pending {
		if last.Time.Sub(t) > pendingMaxAge {
			delete(last.Pending, id)
		} else if t.Before(cutoff) {
			cutoff = t
		}
	}
	for task, id := range last.Tasks {
		if _, ok := last.Pending[id]; !ok {
			delete(last.Tasks, task)
		}
	}
	var keep []Span
	for _, s := range last.Spans {
		if s.To.After(cutoff) {
			keep = append(keep, s)
		}
	}
	last.Spans = keep
	return last
}

// uncovered returns the parts of s that the sorted, disjoint spans miss.
func uncovered(s Span, spans []Span) []Span {
	var out []Span
	cur := s.From
	for _, c := range spans {
		if !c.To.After(cur) {
			continue
		}
		if !c.From.Before(s.To) {
			break
		}
		if c.From.After(cur) {
			out = append(out, Span{cur, c.From})
		}
		cur = c.To
	}
	if cur.Before(s.To) {
		out = append(out, Span{cur, s.To})
	}
	return out
}

// merge adds s to spans and returns them sorted and disjoint.
func merge(spans []Span, s Span) []Span {
	if !s.From.Before(s.To) {
		return spans
	}
	spans = append(spans, s)
	sort.Slice(spans, func(i, j int) bool { return spans[i].From.Before(spans[j].From) })
	out := spans[:1]
	for _, c := range spans[1:] {
		if l := &out[len(out)-1]; !c.From.After(l.To) {
			if c.To.After(l.To) {
				l.To = c.To
			}
			continue
		}
		out = append(out, c)
	}
	return out
}

// AIActiveSeconds sums the agent's time as walk counts it, seeded by prev. It
// returns the total and the trailing Boundary for the next call to seed from.
func AIActiveSeconds(records []transcript.Record, prev Boundary) (float64, Boundary) {
	var total float64
	last := walk(records, prev, func(time.Time) {}, func(_ time.Time, secs float64) { total += secs })
	return total, last
}

// Day is one local calendar day of a session: the AI time counted on it, that
// time by local hour of the day, and its earliest and latest real events.
type Day struct {
	AISeconds   float64
	Hours       [24]float64
	First, Last time.Time
}

// ByDay is AIActiveSeconds split by local day (in loc, keyed "2006-01-02"):
// each counted stretch counts on the day and in the hour it ends. Only events of records shape
// First/Last; the prev seed does not.
func ByDay(records []transcript.Record, prev Boundary, loc *time.Location) (map[string]Day, Boundary) {
	days := map[string]Day{}
	key := func(t time.Time) string { return t.In(loc).Format("2006-01-02") }
	last := walk(records, prev, func(t time.Time) {
		d := days[key(t)]
		if d.First.IsZero() {
			d.First = t // events arrive in time order
		}
		d.Last = t
		days[key(t)] = d
	}, func(end time.Time, secs float64) {
		d := days[key(end)]
		d.AISeconds += secs
		d.Hours[end.In(loc).Hour()] += secs
		days[key(end)] = d
	})
	return days, last
}

// Tail-shape constants classify a session's most recent real event, so the
// watcher can tell a handed-back turn from a tool still executing.
const (
	TailAIWorking    = "ai_working"    // a tool was dispatched; its result is pending
	TailAIDone       = "ai_done"       // an assistant text with no end marker: the turn probably ended
	TailAIEnded      = "ai_ended"      // the transcript says the turn ended; the ball is in the human's court
	TailHuman        = "human"         // a human prompt
	TailAIBackground = "ai_background" // the turn ended, but a background dispatch is still running: the agent is working, not handed back
)

// TailKind classifies the newest user/assistant record in records, skipping a
// user record that is meta and carries no tool result (injected output, not a
// real event). An assistant record with a tool_use block is TailAIWorking; an
// assistant record without one is TailAIDone, or by its stop reason TailAIEnded, or
// TailAIWorking when a tool call is still to come; a user record carrying a
// tool_result is TailAIWorking; any other non-meta user record is TailHuman.
// When no real event is present, prev is returned.
func TailKind(records []transcript.Record, prev string) string {
	best := -1
	var bestT time.Time
	for i, r := range records {
		if r.Type != "user" && r.Type != "assistant" {
			continue
		}
		if r.Type == "user" && r.IsMeta && !hasToolResult(r) {
			continue
		}
		if best == -1 || r.Timestamp.After(bestT) {
			best, bestT = i, r.Timestamp
		}
	}
	if best == -1 {
		return prev
	}
	r := records[best]
	if r.Type == "assistant" {
		for _, b := range r.Content {
			if b.Type == "tool_use" {
				return TailAIWorking
			}
		}
		// Text ahead of a tool call that is still being written, which can take a
		// while, so the agent is still working.
		if r.StopReason == "tool_use" {
			return TailAIWorking
		}
		// The turn ended with text: the agent handed back, whatever it left running
		// in the background (the card notes that). Deciding "still working" from
		// one batch's records misread turns that had ended.
		if r.StopReason != "" {
			return TailAIEnded
		}
		return TailAIDone
	}
	if hasToolResult(r) {
		return TailAIWorking
	}
	return TailHuman
}

// TurnTail is TailKind for a Codex session, which says when a turn opens and
// ends: the newest turn event in records gives TailAIWorking while the turn is
// open, TailAIEnded once it ended (handed back with the final answer) and
// TailHuman when the human aborted it. With no turn event prev is returned, so
// an open turn stays open across batches.
func TurnTail(records []transcript.Record, prev string) string {
	var at time.Time
	tail, seen := prev, false
	for _, r := range records {
		if r.Turn == "" || (seen && r.Timestamp.Before(at)) {
			continue
		}
		at, seen = r.Timestamp, true
		tail = map[string]string{transcript.TurnStarted: TailAIWorking, transcript.TurnEnded: TailAIEnded, transcript.TurnAborted: TailHuman}[r.Turn]
	}
	return tail
}

// IsBackgroundDispatch reports whether a block dispatches work that then runs
// with no further writes to this transcript: a run_in_background tool, or a
// sub-agent (Agent/Task). Such a dispatch is the only in-transcript signal
// that the agent is still working after its turn's text has ended.
func IsBackgroundDispatch(b transcript.Block) bool {
	if b.Type != "tool_use" {
		return false
	}
	return b.Background || b.Name == "Agent" || b.Name == "Task"
}
