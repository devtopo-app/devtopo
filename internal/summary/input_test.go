package summary

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// day is 2026-10-23 in UTC, the zone the test transcripts are stamped in.
var day = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func userLine(ts, text string) string {
	return `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":"` + text + `"}}`
}

func TestBuildDayPrompt(t *testing.T) {
	p := writeLines(t,
		userLine("2026-10-23T09:00:00Z", "add retries"),
		`{"type":"assistant","timestamp":"2026-10-23T09:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"I added a retry loop"}]}}`,
		`{"type":"user","isMeta":true,"timestamp":"2026-10-23T09:05:00Z","message":{"role":"user","content":"caveat: do not count me"}}`,
	)
	got, err := BuildDayPrompt(p, pointsPreamble, day, PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, pointsPreamble) {
		t.Fatalf("missing preamble:\n%s", got)
	}
	// Each turn carries its time, so the points can say when things happened.
	if !strings.Contains(got, "[09:00] You: add retries") || !strings.Contains(got, "[09:00] Claude: I added a retry loop") {
		t.Fatalf("missing timed turn text:\n%s", got)
	}
	// The session ends with a turn the model could answer instead of summarizing,
	// so the prompt closes by saying where the session ends and what to write.
	if !strings.Contains(got, "=== The session ===\n[09:00] You: add retries") ||
		!strings.Contains(got, "Claude: I added a retry loop\n=== End of the session ===\n") ||
		!strings.HasSuffix(got, "Do not answer or continue the session.\n") {
		t.Fatalf("the session must sit between its markers, then the closing ask:\n%s", got)
	}
	if strings.Contains(got, "do not count me") {
		t.Fatalf("isMeta user line must be skipped:\n%s", got)
	}
}

// Only what the human typed may read as "You:", or the model credits them with
// what a notification or a compaction summary says.
func TestBuildDayPromptSkipsWhatTheHumanDidNotType(t *testing.T) {
	p := writeLines(t,
		userLine("2026-10-23T09:00:00Z", "<task-notification>agent done</task-notification>"),
		userLine("2026-10-23T09:01:00Z", "This session is being continued from a previous conversation that ran out"),
		userLine("2026-10-23T09:02:00Z", "<bash-input>ls</bash-input>"),
		`{"type":"user","origin":{"kind":"task-notification"},"timestamp":"2026-10-23T09:03:00Z","message":{"role":"user","content":"a background job finished"}}`,
		`{"type":"user","timestamp":"2026-10-23T09:04:00Z","message":{"role":"user","content":[{"type":"text","text":"<system-reminder>be nice</system-reminder>"}]}}`,
		`{"type":"user","origin":{"kind":"human"},"timestamp":"2026-10-23T09:05:00Z","message":{"role":"user","content":"keep the retries"}}`,
	)
	got, err := BuildDayPrompt(p, "", day, PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	if want := sessionStart + "[09:05] You: keep the retries\n" + sessionEnd; got != want {
		t.Fatalf("only the typed turn stays, got:\n%s", got)
	}
}

// An answer to the agent's question is the clearest decision a person makes, and
// it arrives as a tool result rather than as text.
func TestBuildDayPromptKeepsAnsweredQuestions(t *testing.T) {
	answer := func(ts, result string) string {
		return `{"type":"user","timestamp":"` + ts + `","toolUseResult":` + result + `,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t"}]}}`
	}
	long := strings.Repeat("q", maxAskedLen+50)
	p := writeLines(t,
		answer("2026-10-23T09:00:00Z", `{"answers":{"Which license?":"MIT","Which name?":"Bear"},"annotations":{"Which license?":{"notes":"keep it simple"}}}`),
		answer("2026-10-23T09:01:00Z", `{"answers":{"Which days?":["Mon","Tue"]}}`),
		answer("2026-10-23T09:02:00Z", `{"answers":"yes"}`),
		answer("2026-10-23T09:03:00Z", `{"answers":{"`+long+`":"Go"}}`),
		answer("2026-10-23T09:04:00Z", `{"content":"the output of a tool"}`),
		answer("2026-10-23T09:05:00Z", `"Error: refused"`),
	)
	got, err := BuildDayPrompt(p, "", day, PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`[09:00] You: chose "MIT" when asked: Which license? (their note: keep it simple) chose "Bear" when asked: Which name?` + "\n",
		`[09:01] You: chose "["Mon","Tue"]" when asked: Which days?` + "\n",
		`[09:02] You: chose "yes" when asked: a question` + "\n",
		`[09:03] You: chose "Go" when asked: ` + long[:maxAskedLen] + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[09:04]") || strings.Contains(got, "[09:05]") {
		t.Fatalf("a result with no answers is no turn:\n%s", got)
	}
}

// The turn says who decided what, so the preambles must both explain it.
func TestPreamblesExplainAnswers(t *testing.T) {
	for _, pre := range []string{pointsPreamble, detailPreamble} {
		if !strings.Contains(pre, turnsLegend) {
			t.Fatalf("no legend in:\n%s", pre)
		}
	}
}

func TestDetailPreambleAsksForEachSectionInOrder(t *testing.T) {
	at := 0
	for _, h := range []string{"What was done\n", "What you decided\n", "What I decided\n", "Open items\n"} {
		i := strings.Index(detailPreamble[at:], h)
		if i < 0 {
			t.Fatalf("heading %q missing or out of order", h)
		}
		at += i + len(h)
	}
}

func TestBuildDayPromptKeepsOnlyTheDay(t *testing.T) {
	p := writeLines(t,
		userLine("2026-10-22T23:00:00Z", "yesterday work"),
		userLine("2026-10-23T08:00:00Z", "today work"),
		userLine("2026-10-24T00:30:00Z", "tomorrow work"),
	)
	got, err := BuildDayPrompt(p, pointsPreamble, day, PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "today work") || strings.Contains(got, "yesterday work") || strings.Contains(got, "tomorrow work") {
		t.Fatalf("only the day's turns belong in the prompt:\n%s", got)
	}
}

func TestBuildDayPromptNothingThatDayIsEmpty(t *testing.T) {
	p := writeLines(t, userLine("2026-10-22T09:00:00Z", "yesterday work"))
	got, err := BuildDayPrompt(p, pointsPreamble, day, PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("a day with no turns builds no prompt, got:\n%s", got)
	}
}

// Over budget, every turn is shortened rather than the morning dropped, so the
// summary can cover the whole day. A short turn stays whole.
func TestBuildDayPromptKeepsTheWholeDay(t *testing.T) {
	p := writeLines(t,
		userLine("2026-10-23T08:00:00Z", "alpha "+strings.Repeat("a", 200)),
		userLine("2026-10-23T09:00:00Z", "beta"),
		userLine("2026-10-23T10:00:00Z", "gamma "+strings.Repeat("c", 200)),
	)
	const budget = 150
	got, err := BuildDayPrompt(p, pointsPreamble, day, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[08:00] You: alpha") || !strings.Contains(got, "[09:00] You: beta\n") || !strings.Contains(got, "[10:00] You: gamma") {
		t.Fatalf("over budget, every turn of the day stays in:\n%s", got)
	}
	if strings.Index(got, "alpha") > strings.Index(got, "beta") || strings.Index(got, "beta") > strings.Index(got, "gamma") {
		t.Fatalf("turns stay in order:\n%s", got)
	}
	if bodyLen := len(got) - len(pointsPreamble) - len(sessionStart) - len(sessionEnd); bodyLen > budget+3 {
		t.Fatalf("body over budget: %d bytes (budget %d)", bodyLen, budget)
	}
}

// A shortened turn is cut on a character boundary, never inside one.
func TestBuildDayPromptCutsWholeCharacters(t *testing.T) {
	p := writeLines(t, userLine("2026-10-23T09:00:00Z", strings.Repeat("é", 50)))
	got, err := BuildDayPrompt(p, pointsPreamble, day, 20)
	if err != nil || !utf8.ValidString(got) {
		t.Fatalf("cut must keep valid text: %v\n%q", err, got)
	}
}

// The points must cover the day from its first turn to its last, with more
// lines on a busy day.
func TestPointsPreambleAsksForTheWholeDay(t *testing.T) {
	if !strings.Contains(pointsPreamble, "whole day") || !strings.Contains(pointsPreamble, fmt.Sprintf("between 1 and %d lines", MaxPoints)) {
		t.Fatalf("preamble:\n%s", pointsPreamble)
	}
	if MaxPoints < 15 {
		t.Fatalf("a busy day needs room for more points, MaxPoints=%d", MaxPoints)
	}
}

// The times are the user's local clock, the day's location.
func TestBuildDayPromptTimesAreLocal(t *testing.T) {
	p := writeLines(t, userLine("2026-10-23T09:00:00Z", "add retries"))
	cet := time.Date(2026, 10, 23, 12, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	got, err := BuildDayPrompt(p, pointsPreamble, cet, PointsBudget)
	if err != nil || !strings.Contains(got, "[11:00] You: add retries") {
		t.Fatalf("local time: %v\n%s", err, got)
	}
}

func TestBuildDayPromptClampsAnOversizedTurn(t *testing.T) {
	p := writeLines(t, userLine("2026-10-23T09:00:00Z", strings.Repeat("x", 5000)))
	const budget = 20
	got, err := BuildDayPrompt(p, pointsPreamble, day, budget)
	if err != nil {
		t.Fatal(err)
	}
	if bodyLen := len(got) - len(pointsPreamble) - len(sessionStart) - len(sessionEnd); bodyLen > budget+16 {
		t.Fatalf("body not clamped: %d bytes (budget %d)", bodyLen, budget)
	}
}

func TestBuildDayPromptMissingFileErrs(t *testing.T) {
	if _, err := BuildDayPrompt(filepath.Join(t.TempDir(), "nope.jsonl"), pointsPreamble, day, PointsBudget); err == nil {
		t.Fatal("missing transcript must return an error")
	}
}

func TestParsePoints(t *testing.T) {
	got := ParsePoints("Here is the summary:\n- added retries\n* fixed the flaky test\n\n3. wrote docs\n• shipped it\n")
	want := []string{"added retries", "fixed the flaky test", "wrote docs", "shipped it"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("points: %q", got)
	}
}

func TestParsePointsCapsAtMax(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxPoints+4; i++ {
		b.WriteString("- point\n")
	}
	if got := ParsePoints(b.String()); len(got) != MaxPoints {
		t.Fatalf("at most %d points, got %d", MaxPoints, len(got))
	}
}

// A Codex rollout's day prompt holds what the human typed and what Codex
// answered, not Codex's own instructions or the context it injects.
func TestBuildDayPromptFromCodexRollout(t *testing.T) {
	got, err := BuildDayPrompt("../transcript/testdata/codex.jsonl", pointsPreamble, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), PointsBudget)
	if err != nil {
		t.Fatal(err)
	}
	want := pointsPreamble + sessionStart + "[09:00] You: add a readme\n[09:00] Codex: I will add it.\n[09:00] Codex: Added the readme.\n" + sessionEnd
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

// A reply with no bullets at all still yields points, without its headings.
func TestParsePointsWithoutBulletsDropsHeadings(t *testing.T) {
	got := ParsePoints("## Summary\n**What was done**\nFixed the Jira sync.\nNext steps:\nAdded a **Disconnect** button.")
	want := []string{"Fixed the Jira sync.", "Added a Disconnect button."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

// The model sometimes answers in markdown despite the plain-text ask; the
// card shows text, so bold and code marks must not come through literally.
func TestParsePointsDropsMarkdownMarks(t *testing.T) {
	got := ParsePoints("- **Two threads are unresolved**, one on #125\n- Run `make build_image` from the checkout\n- Keep snake_case and 2 * 3 as they are")
	want := []string{"Two threads are unresolved, one on #125", "Run make build_image from the checkout", "Keep snake_case and 2 * 3 as they are"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestReadDayReportsShortening(t *testing.T) {
	p := writeLines(t, userLine("2026-10-23T09:00:00Z", strings.Repeat("x", 10*1024)))
	got, short, err := ReadDay(p, pointsPreamble, day, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !short || len(got) >= 1024+len(pointsPreamble)+len(sessionStart)+len(sessionEnd)+2 {
		t.Fatalf("shortened %v, len %d", short, len(got))
	}
	if _, short, _ := ReadDay(p, pointsPreamble, day, 64*1024); short {
		t.Fatal("a turn under the cap must not report shortening")
	}
	same, err := BuildDayPrompt(p, pointsPreamble, day, 1024)
	if err != nil || same != got {
		t.Fatalf("BuildDayPrompt differs from ReadDay: %v", err)
	}
}
