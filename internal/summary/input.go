package summary

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/transcript"
)

// PointsBudget and DetailBudget bound the body (excluding the preamble) of a
// day prompt, in bytes, so a multi-megabyte session cannot blow the stdin cap
// or the model context. The detail prompt is asked for on demand, so it can
// afford more of the day.
const (
	PointsBudget = 64 * 1024
	DetailBudget = 96 * 1024
)

// MaxPoints caps the bullet points kept from a day summary.
const MaxPoints = 15

// PointsSchema is the version of pointsPreamble. Points stored under an older
// one are written again, so every summary has the same format.
const PointsSchema = 6

// DetailSchema is the version of detailPreamble. A cached detail under an older
// one is written again.
const DetailSchema = 2

// answerMark words a turn holding the human's answer to the agent's question,
// and turnsLegend tells the model how to read it.
const (
	answerMark  = `chose "%s" when asked: %s`
	turnsLegend = `A "You:" turn saying chose "..." when asked is the developer's answer to a question from the agent.`
)

// maxAskedLen bounds the question quoted with an answer, since the agent may ask
// with a whole plan in the question.
const maxAskedLen = 300

// pointsPreamble and detailPreamble instruct the model what kind of summary to
// write. They are fixed so summaries read consistently and never leak a
// request to run tools.
var pointsPreamble = `You write the short summary DevTopo shows for one coding session on one day. The reader is the developer, looking back at their day. Below is that day of the session: what they typed and what the coding agent answered, each turn starting with its time as [HH:MM]. Long turns are shortened. ` + turnsLegend + `

Cover the whole day, from the first turn to the last: every stretch of work gets its line, the morning as much as the end of the day.
Write between 1 and ` + strconv.Itoa(MaxPoints) + ` lines, one for each distinct piece of work: few when little happened, more on a busy day. Write nothing else: no title, no intro, no closing line.
Each line is "- ", the time the step started as HH:MM taken from the turns, a space, then one plain sentence of 8 to 18 words ending with a full stop.
Put the lines in time order, earliest first.
Start each sentence with a past-tense verb, such as Fixed, Added, Changed, Removed, Investigated, Reviewed, Decided or Planned.
Say what changed, was found or was decided, and why it matters, in everyday words. Name the feature or the problem, not file names, function names, commands, flags or error codes.
Merge small steps into one line. Leave out tool runs, retries, tests passing and small talk.
Plain text only: no ** or other markdown, no backticks, no headings, no nested bullets, no emoji.

Example of the format:
- 09:12 Fixed the Jira sync so tickets read with a scoped token show in the planner.
- 10:40 Added a Disconnect button in Settings that also removes the synced tickets.
- 14:05 Decided to keep the order of the Today modules per browser for now.

`

// sessionStart and sessionEnd fence the session's turns. The last turn is often
// a question to the user, which the model would otherwise answer.
const (
	sessionStart = "=== The session ===\n"
	sessionEnd   = "=== End of the session ===\nNow write what the instructions at the top ask for. Do not answer or continue the session.\n"
)

var detailPreamble = `You write the longer summary DevTopo shows when the developer asks for more detail on one coding session on one day. The reader is the developer, looking back at their day. Below is that day of the session: what they typed, marked "You:", and what the coding agent answered, marked "Claude:" or "Codex:", each turn starting with its time as [HH:MM]. Long turns are shortened. ` + turnsLegend + `

Write exactly these four headings, in this order, and under each one bullet lines starting with "- ". A heading is a plain line on its own: no "- " before it, no markdown such as # or *. Start with the first heading: no title or intro, no closing line. Write "- none" under a heading with nothing to say; an empty section is better than a padded one. The limits on bullets are hard limits, so on a long day merge and keep what matters most. The shape is:

What was done
- ...

What you decided
- ...

What I decided
- ...

Open items
- ...

What each section holds:

What was done
The reader already sees a timed list with one short line per piece of work, so do not retell it step by step. Add what that list leaves out: what the goal or problem was and where it came from, what turned out to be the cause, how it was solved and how it works now. Group by topic, not by turn. At most 5 bullets of one or two sentences each.

What you decided
The calls the developer made in their own turns: a choice between options, a direction they set, something they rejected, corrected or told the agent to leave alone. Start each bullet with "You", as in "You chose". Say what was chosen and over what, with their reason if they gave one. When they only said yes, say what the yes settled. A plain request for work is not a decision unless it picks how the work is done. At most 6 bullets.

What I decided
Here "I" is the coding agent, so write in the first person. The key calls the agent made on its own, which the developer neither asked for nor settled, and might want to check or overrule: an approach, a trade-off, an assumption, something left out. Give the reason. A proposal the developer then accepted belongs under What you decided, not here. Leave out routine steps such as running tests, committing or the order of the work. At most 5 bullets.

Open items
Only what is still unresolved after the last turn and needs someone to act: a question waiting for an answer, a bug found and not fixed, work promised or put off, a change not yet merged or released, a check only the developer can do. Say who it waits on. Leave out anything a later turn settled, anything the developer said to leave, ideas and polish, and general caveats about what was not tested. At most 4 bullets, the most important first.

Use plain, everyday words. Name the feature or the problem, not file names, function names, commands or flags. Base every bullet on the turns below and do not guess. No tool calls or counts.

`

// BuildDayPrompt reads the raw transcript at path in place and builds a
// bounded prompt: the preamble followed by the human and assistant turns
// stamped on day's local date (in day's location), as labelled text. When they
// exceed budget bytes of body, the most recent turns are kept. It returns ""
// when no turn falls on that day, and never copies the transcript into the
// store. A missing or unreadable file returns an error.
func BuildDayPrompt(path, preamble string, day time.Time, budget int) (string, error) {
	p, _, err := ReadDay(path, preamble, day, budget)
	return p, err
}

// ReadDay is BuildDayPrompt that also reports whether any turn was shortened
// to fit budget.
func ReadDay(path, preamble string, day time.Time, budget int) (prompt string, shortened bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	var turns []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // tolerate long transcript lines
	for sc.Scan() {
		role, text, at := extractTurn(sc.Bytes())
		if text == "" || !sameDay(at, day) {
			continue
		}
		label := map[string]string{"user": "You: ", "assistant": "Claude: ", "codex": "Codex: "}[role]
		turns = append(turns, "["+at.In(day.Location()).Format("15:04")+"] "+label+text)
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	if len(turns) == 0 {
		return "", false, nil
	}

	// Over budget, cut every turn to the same length, the longest that fits, so
	// the whole day stays in: short prompts whole, long answers shortened.
	c := fitCap(turns, budget)
	for i, t := range turns {
		if len(t) > c {
			shortened = true
		}
		turns[i] = cut(t, c)
	}
	return preamble + sessionStart + strings.Join(turns, "\n") + "\n" + sessionEnd, shortened, nil
}

// fitCap returns the largest length c such that the turns, each cut to c
// bytes, add up to at most budget.
func fitCap(turns []string, budget int) int {
	lens := make([]int, len(turns))
	for i, t := range turns {
		lens[i] = len(t)
	}
	sort.Ints(lens)
	for i, l := range lens {
		share := budget / (len(lens) - i)
		if l > share {
			return share
		}
		budget -= l
	}
	return lens[len(lens)-1]
}

// cut shortens s to at most n bytes without splitting a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// sameDay reports whether t falls on day's calendar date in day's location.
func sameDay(t, day time.Time) bool {
	if t.IsZero() {
		return false
	}
	ty, tm, td := t.In(day.Location()).Date()
	dy, dm, dd := day.Date()
	return ty == dy && tm == dm && td == dd
}

// ParsePoints turns the model's reply into at most MaxPoints clean points: the
// bullet lines with their marker stripped, or every non-empty line when the
// reply has no bullets at all.
func ParsePoints(text string) []string {
	var bullets, lines []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if p, ok := stripBullet(l); ok && p != "" {
			bullets = append(bullets, plain(p))
		} else if !heading(l) {
			lines = append(lines, plain(l))
		}
	}
	if len(bullets) == 0 {
		bullets = lines
	}
	if len(bullets) > MaxPoints {
		bullets = bullets[:MaxPoints]
	}
	return bullets
}

// plain drops the markdown marks the model sometimes uses despite the
// plain-text ask: "**" bold and backtick code. A lone "*" or "_" stays, as in
// "2 * 3" or snake_case.
func plain(s string) string {
	return strings.NewReplacer("**", "", "`", "").Replace(s)
}

// heading reports whether a line is a title rather than a point: "# ...",
// "...:" or a line that is bold as a whole.
func heading(l string) bool {
	return strings.HasPrefix(l, "#") || strings.HasSuffix(l, ":") ||
		(len(l) > 4 && strings.HasPrefix(l, "**") && strings.HasSuffix(l, "**"))
}

// stripBullet removes a leading "- ", "* ", "•" or "1." style marker. A "*"
// with no space after it starts bold text, not a bullet.
func stripBullet(l string) (string, bool) {
	for _, m := range []string{"- ", "* ", "•"} {
		if strings.HasPrefix(l, m) {
			return strings.TrimSpace(strings.TrimPrefix(l, m)), true
		}
	}
	i := 0
	for i < len(l) && l[i] >= '0' && l[i] <= '9' {
		i++
	}
	if i > 0 && i < len(l) && (l[i] == '.' || l[i] == ')') {
		return strings.TrimSpace(l[i+1:]), true
	}
	return "", false
}

// extractTurn pulls a labelled turn from one raw JSONL line. It returns the
// record's role ("user"/"assistant"), its human-readable text and its
// timestamp, or an empty text for anything that is not a real human prompt, a
// human's answer to the agent's question, or an assistant message (a meta user
// line, a notification or summary the harness sent as the user, a tool-only
// turn, a system record). It parses the minimum itself because
// transcript.Decode drops all message text.
func extractTurn(line []byte) (role, text string, at time.Time) {
	var raw struct {
		Type      string          `json:"type"`
		IsMeta    bool            `json:"isMeta"`
		Timestamp time.Time       `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
		Payload   json.RawMessage `json:"payload"`
		Origin    json.RawMessage `json:"origin"`
		Result    json.RawMessage `json:"toolUseResult"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return "", "", time.Time{}
	}
	if raw.Type == "response_item" {
		return codexTurn(raw.Payload, raw.Timestamp)
	}
	if raw.Type != "user" && raw.Type != "assistant" {
		return "", "", time.Time{}
	}
	if raw.Type == "user" && raw.IsMeta {
		return "", "", time.Time{} // injected caveat / command output, not a real turn
	}
	if raw.Type == "user" {
		if a := answerText(raw.Result); a != "" {
			return "user", a, raw.Timestamp
		}
		var origin struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(raw.Origin, &origin)
		if origin.Kind != "" && origin.Kind != "human" {
			return "", "", time.Time{} // sent by a teammate or a finished task
		}
	}
	if len(raw.Message) == 0 {
		return "", "", time.Time{}
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw.Message, &msg); err != nil {
		return "", "", time.Time{}
	}
	content := bytes.TrimSpace(msg.Content)
	if len(content) == 0 {
		return "", "", time.Time{}
	}
	if content[0] == '"' { // a plain string prompt
		var s string
		if err := json.Unmarshal(content, &s); err != nil {
			return "", "", time.Time{}
		}
		return raw.Type, typedText(raw.Type, s), raw.Timestamp
	}
	if content[0] == '[' { // an array of typed blocks; keep only text
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(content, &blocks); err != nil {
			return "", "", time.Time{}
		}
		var parts []string
		for _, bl := range blocks {
			if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" {
				parts = append(parts, strings.TrimSpace(bl.Text))
			}
		}
		return raw.Type, typedText(raw.Type, strings.Join(parts, " ")), raw.Timestamp
	}
	return "", "", time.Time{}
}

// typedText is a record's trimmed text, or "" when a user record's text was not
// typed as a message: an injected one, or a shell command and its output.
func typedText(role, text string) string {
	text = strings.TrimSpace(text)
	if role == "user" && (facts.Injected(text) || strings.HasPrefix(text, "<bash-")) {
		return ""
	}
	return text
}

// answerText words the answers in an AskUserQuestion result as one turn, or ""
// when the result holds none. The answer comes before the question, so that
// shortening the turn to fit the budget takes the question first.
func answerText(result json.RawMessage) string {
	var r struct {
		Answers     json.RawMessage `json:"answers"`
		Annotations map[string]struct {
			Notes string `json:"notes"`
		} `json:"annotations"`
	}
	if json.Unmarshal(result, &r) != nil || len(r.Answers) == 0 {
		return ""
	}
	var byQuestion map[string]json.RawMessage
	if json.Unmarshal(r.Answers, &byQuestion) != nil {
		return fmt.Sprintf(answerMark, answerValue(r.Answers), "a question")
	}
	asked := make([]string, 0, len(byQuestion))
	for q := range byQuestion {
		asked = append(asked, q)
	}
	sort.Strings(asked)
	parts := make([]string, 0, len(asked))
	for _, q := range asked {
		part := fmt.Sprintf(answerMark, answerValue(byQuestion[q]), cut(q, maxAskedLen))
		if note := strings.TrimSpace(r.Annotations[q].Notes); note != "" {
			part += " (their note: " + note + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

// answerValue is one answer as text: a string as it is, any other shape (the
// options picked in a multi-select) as its JSON.
func answerValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// codexTurn is extractTurn for a Codex rollout's response_item: a message the
// human typed is "user" and an assistant message "codex". Developer messages,
// injected context and tool calls are not turns.
func codexTurn(payload json.RawMessage, at time.Time) (role, text string, _ time.Time) {
	var p struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Type != "message" || len(p.Content) == 0 {
		return "", "", time.Time{}
	}
	switch {
	case p.Role == "user" && !transcript.CodexInjected(p.Content[0].Text):
		role = "user"
	case p.Role == "assistant":
		role = "codex"
	default:
		return "", "", time.Time{}
	}
	var parts []string
	for _, c := range p.Content {
		if s := strings.TrimSpace(c.Text); s != "" {
			parts = append(parts, s)
		}
	}
	return role, strings.Join(parts, " "), at
}
