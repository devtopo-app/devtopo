package ask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testDay = time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)

// fake answers by prompt kind, counts calls and records peak concurrency.
type fake struct {
	mu                   sync.Mutex
	check, read, combine int
	cur, peak            int
	checkPrompts         []string
	onCheck              func(ctx context.Context, p string) (string, error)
	onRead               func(ctx context.Context, p string) (string, error)
	onCombine            func(ctx context.Context, p string) (string, error)
}

func (f *fake) calls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.check + f.read + f.combine }

func (f *fake) Narrate(ctx context.Context, p string) (string, error) {
	switch {
	case strings.HasPrefix(p, "You read one coding session"):
		f.mu.Lock()
		f.read++
		f.cur++
		if f.cur > f.peak {
			f.peak = f.cur
		}
		f.mu.Unlock()
		defer func() { f.mu.Lock(); f.cur--; f.mu.Unlock() }()
		if f.onRead == nil {
			return "- 09:30 found it", nil
		}
		return f.onRead(ctx, p)
	case strings.Contains(p, "=== Notes from today's sessions ==="):
		f.mu.Lock()
		f.combine++
		f.mu.Unlock()
		if f.onCombine == nil {
			return `{"answer":"combined","sources":[{"session":"S1","time":"09:30","why":"w"}]}`, nil
		}
		return f.onCombine(ctx, p)
	default:
		f.mu.Lock()
		f.check++
		f.checkPrompts = append(f.checkPrompts, p)
		f.mu.Unlock()
		if f.onCheck == nil {
			return `{"answer":"","read":[]}`, nil
		}
		return f.onCheck(ctx, p)
	}
}

// sessions makes n sessions, each with a real transcript holding one turn today.
func sessions(t *testing.T, n int, summarised bool) []Session {
	t.Helper()
	dir := t.TempDir()
	var out []Session
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("id%d", i)
		path := filepath.Join(dir, id+".jsonl")
		line := fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":"work in %s"}}`+"\n", testDay.Add(-2*time.Hour).Format(time.RFC3339), id)
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		out = append(out, Session{ID: id, Label: "proj" + fmt.Sprint(i), Agent: "claude", TranscriptPath: path, HasSummary: summarised, Points: []string{"09:30 did a thing"}})
	}
	return out
}

func run(ctx context.Context, f *fake, ss []Session, mem []Turn) []Event {
	var evs []Event
	Run(ctx, f, Request{Question: "what did I do?", Memory: mem, Sessions: ss, Day: testDay}, func(e Event) { evs = append(evs, e) })
	return evs
}

func last(evs []Event) Event { return evs[len(evs)-1] }

func ofType(evs []Event, typ string) (out []Event) {
	for _, e := range evs {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return
}

func aliasList(n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("S%d", i))
	}
	return out
}

func readList(aliases ...string) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) {
		q := make([]string, len(aliases))
		for i, a := range aliases {
			q[i] = `"` + a + `"`
		}
		return `{"answer":"","read":[` + strings.Join(q, ",") + `]}`, nil
	}
}

func TestAnswersFromSummariesInOneCall(t *testing.T) {
	f := &fake{onCheck: func(context.Context, string) (string, error) {
		return `{"answer":"You fixed sync.","sources":[{"session":"S1","time":"09:30","why":"fix"}],"read":[]}`, nil
	}}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	r := last(evs).Result
	if last(evs).Type != "done" || f.calls() != 1 || !r.FromSummaries || r.Read != 0 || r.Answer != "You fixed sync." || len(r.Sources) != 1 {
		t.Fatalf("calls=%d events=%+v result=%+v", f.calls(), evs, r)
	}
}

func TestReadsEveryRelevantSessionWithNoCap(t *testing.T) {
	f := &fake{onCheck: readList(aliasList(10)...)}
	evs := run(context.Background(), f, sessions(t, 14, true), nil)
	if f.read != 10 || f.combine != 1 || f.check != 1 {
		t.Fatalf("check=%d read=%d combine=%d", f.check, f.read, f.combine)
	}
	if rel := ofType(evs, "relevant"); len(rel) != 1 || rel[0].Count != 10 {
		t.Fatalf("relevant = %+v", rel)
	}
	reads := ofType(evs, "read")
	if len(reads) != 10 {
		t.Fatalf("read events = %d", len(reads))
	}
	for i, e := range reads {
		if e.Done != i+1 || e.Total != 10 || e.Note == nil || e.Note.Text == "" {
			t.Fatalf("read event %d = %+v", i, e)
		}
	}
	r := last(evs).Result
	if r.Read != 10 || r.Total != 14 || r.FromSummaries {
		t.Fatalf("result = %+v", r)
	}
}

func TestDuplicateReadIsReadOnce(t *testing.T) {
	f := &fake{onCheck: readList("S2", "S2", "S1", "S2")}
	evs := run(context.Background(), f, sessions(t, 3, true), nil)
	if f.read != 2 || ofType(evs, "relevant")[0].Count != 2 {
		t.Fatalf("read=%d events=%+v", f.read, evs)
	}
}

func TestNeverRunsMoreThanThreeReadsAtOnce(t *testing.T) {
	// Once three reads are running, hold them for a window so a fourth, if the
	// cap let it, would start and raise the peak. A cap below three never gets
	// there, so a fallback timer opens the gate and the peak check fails.
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	fallback := time.AfterFunc(2*time.Second, open)
	defer fallback.Stop()
	f := &fake{onCheck: readList(aliasList(10)...)}
	f.onRead = func(ctx context.Context, p string) (string, error) {
		f.mu.Lock()
		if f.cur == 3 {
			time.AfterFunc(100*time.Millisecond, open)
		}
		f.mu.Unlock()
		<-gate
		return "- 09:30 x", nil
	}
	run(context.Background(), f, sessions(t, 10, true), nil)
	if f.peak != 3 {
		t.Fatalf("peak = %d, want 3", f.peak)
	}
}

func TestNoSummarySessionIsReadWhenAReadRuns(t *testing.T) {
	ss := sessions(t, 2, true)
	ss[1].HasSummary = false
	f := &fake{onCheck: readList("S1")}
	evs := run(context.Background(), f, ss, nil)
	if f.read != 2 || ofType(evs, "relevant")[0].Count != 2 {
		t.Fatalf("read=%d", f.read)
	}
}

func TestNoSummarySessionIsSkippedWhenCheckAnswers(t *testing.T) {
	ss := sessions(t, 2, true)
	ss[1].HasSummary = false
	f := &fake{onCheck: func(context.Context, string) (string, error) {
		return `{"answer":"a","sources":[{"session":"S1","time":"09:30","why":"w"}],"read":[]}`, nil
	}}
	evs := run(context.Background(), f, ss, nil)
	if f.read != 0 || f.calls() != 1 || !last(evs).Result.FromSummaries {
		t.Fatalf("read=%d calls=%d", f.read, f.calls())
	}
}

func TestAnswerWithOnlyInventedSourcesIsNotAccepted(t *testing.T) {
	f := &fake{onCheck: func(context.Context, string) (string, error) {
		return `{"answer":"made up","sources":[{"session":"S9","time":"09:30","why":"w"}],"read":[]}`, nil
	}}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	if f.read != 2 || last(evs).Result.FromSummaries || last(evs).Result.Answer == "made up" {
		t.Fatalf("read=%d result=%+v", f.read, last(evs).Result)
	}
}

func TestUnparseableCheckReadsEverySession(t *testing.T) {
	f := &fake{onCheck: func(context.Context, string) (string, error) { return "sorry, no JSON", nil }}
	run(context.Background(), f, sessions(t, 4, true), nil)
	if f.read != 4 {
		t.Fatalf("read=%d", f.read)
	}
}

func TestAllReadsInventedReadsEverySession(t *testing.T) {
	f := &fake{onCheck: readList("S8", "S9")}
	run(context.Background(), f, sessions(t, 3, true), nil)
	if f.read != 3 {
		t.Fatalf("read=%d", f.read)
	}
}

func TestNothingRelevantSaysSoWithoutACombineCall(t *testing.T) {
	f := &fake{}
	evs := run(context.Background(), f, sessions(t, 3, true), nil)
	r := last(evs).Result
	if f.calls() != 1 || last(evs).Type != "done" || !strings.Contains(r.Answer, "didn't find it in what I read") {
		t.Fatalf("calls=%d last=%+v", f.calls(), last(evs))
	}
}

func TestNothingInAnySessionWordsItTheSameWay(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2"), onRead: func(context.Context, string) (string, error) { return "NOTHING", nil }}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	r := last(evs).Result
	if f.combine != 0 || r.Answer != "I didn't find it in what I read." || r.Read != 2 {
		t.Fatalf("combine=%d result=%+v", f.combine, r)
	}
}

func TestNoSessionsTodayCallsNoModel(t *testing.T) {
	f := &fake{}
	evs := run(context.Background(), f, nil, nil)
	if f.calls() != 0 || last(evs).Type != "done" || last(evs).Result.Answer != "There are no sessions today yet." {
		t.Fatalf("calls=%d evs=%+v", f.calls(), evs)
	}
}

func TestOneFailedReadCarriesOnAndIsNamed(t *testing.T) {
	ss := sessions(t, 2, true)
	if err := os.Remove(ss[1].TranscriptPath); err != nil {
		t.Fatal(err)
	}
	f := &fake{onCheck: readList("S1", "S2")}
	evs := run(context.Background(), f, ss, nil)
	r := last(evs).Result
	if f.combine != 1 || len(r.Failed) != 1 || r.Failed[0] != "proj2" || r.Read != 1 {
		t.Fatalf("combine=%d result=%+v", f.combine, r)
	}
}

func TestUsageLimitStopsAndCancelsTheRest(t *testing.T) {
	// The first read to start succeeds. The second waits until the caller has
	// seen the first note, then hits the limit; every other read waits for the
	// cancel. So the note is emitted before the limit, and nothing hangs.
	noteSeen := make(chan struct{})
	var started atomic.Int32
	f := &fake{onCheck: readList(aliasList(10)...)}
	f.onRead = func(ctx context.Context, p string) (string, error) {
		switch started.Add(1) {
		case 1:
			return "- 09:30 first note", nil
		case 2:
			<-noteSeen
			return "", errors.New("Claude usage limit reached")
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	var evs []Event
	var once sync.Once
	Run(context.Background(), f, Request{Question: "q", Sessions: sessions(t, 10, true), Day: testDay}, func(e Event) {
		evs = append(evs, e)
		if e.Type == "read" && e.Note != nil {
			once.Do(func() { close(noteSeen) })
		}
	})
	e := last(evs)
	if e.Type != "error" || !strings.HasPrefix(e.Error, "Your subscription's usage limit was reached: ") || !strings.Contains(e.Error, "usage limit reached") {
		t.Fatalf("last = %+v", e)
	}
	if f.combine != 0 || f.read >= 10 {
		t.Fatalf("combine=%d read=%d", f.combine, f.read)
	}
	if len(ofType(evs, "done")) != 0 {
		t.Fatalf("done after a limit: %+v", evs)
	}
	reads := ofType(evs, "read")
	if len(reads) != 1 || reads[0].Note == nil || reads[0].Note.Text != "- 09:30 first note" {
		t.Fatalf("the note read before the limit was not kept: %+v", evs)
	}
}

func TestEveryReadFailingIsAnError(t *testing.T) {
	ss := sessions(t, 2, true)
	for _, s := range ss {
		os.Remove(s.TranscriptPath)
	}
	f := &fake{onCheck: readList("S1", "S2")}
	evs := run(context.Background(), f, ss, nil)
	if last(evs).Type != "error" || last(evs).Error == "" || f.combine != 0 {
		t.Fatalf("last = %+v", last(evs))
	}
}

func TestEveryReadFailingShowsTheToolsError(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2"), onRead: func(context.Context, string) (string, error) {
		return "", errors.New("not logged in: run claude login")
	}}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	e := last(evs)
	if e.Type != "error" || !strings.Contains(e.Error, "not logged in: run claude login") || f.combine != 0 {
		t.Fatalf("last = %+v", e)
	}
}

func TestShortenedSessionIsNamed(t *testing.T) {
	ss := sessions(t, 2, true)
	long := fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":%q}}`+"\n", testDay.Add(-time.Hour).Format(time.RFC3339), strings.Repeat("x", 200*1024))
	if err := os.WriteFile(ss[0].TranscriptPath, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fake{onCheck: readList("S1", "S2")}
	r := last(run(context.Background(), f, ss, nil)).Result
	if len(r.Shortened) != 1 || r.Shortened[0] != "proj1" {
		t.Fatalf("shortened = %v", r.Shortened)
	}
}

func TestCombineThatWontParseStillShowsAnswerAndSources(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2"), onCombine: func(context.Context, string) (string, error) { return "plain words", nil }}
	r := last(run(context.Background(), f, sessions(t, 2, true), nil)).Result
	if r.Answer != "plain words" || len(r.Sources) != 2 {
		t.Fatalf("result = %+v", r)
	}
}

func TestCombineWithEmptyAnswerSaysNothingWasFound(t *testing.T) {
	for name, reply := range map[string]string{
		"empty json": `{"answer":"","sources":[],"read":[]}`,
		"empty text": "  ",
	} {
		f := &fake{onCheck: readList("S1"), onCombine: func(context.Context, string) (string, error) { return reply, nil }}
		r := last(run(context.Background(), f, sessions(t, 2, true), nil)).Result
		if r.Answer != "I didn't find it in what I read." || len(r.Sources) != 0 {
			t.Fatalf("%s: result = %+v", name, r)
		}
	}
}

func TestFollowUpFromNotesSendsMemoryToCheck(t *testing.T) {
	ss := sessions(t, 2, true)
	mem := []Turn{{Question: "q1", Answer: "a1", Notes: []Note{{SessionID: "id1", Label: "proj1", Text: "UNIQUE-NOTE-TEXT"}}}}
	f := &fake{onCheck: func(context.Context, string) (string, error) {
		return `{"answer":"a","sources":[{"session":"S1","time":"09:30","why":"w"}]}`, nil
	}}
	run(context.Background(), f, ss, mem)
	if f.calls() != 1 || !strings.Contains(f.checkPrompts[0], "UNIQUE-NOTE-TEXT") {
		t.Fatalf("calls=%d", f.calls())
	}
}

func TestFollowUpCanReReadASession(t *testing.T) {
	ss := sessions(t, 2, true)
	mem := []Turn{{Question: "q1", Answer: "a1", Notes: []Note{{SessionID: "id1", Label: "proj1", Text: "old"}}}}
	f := &fake{onCheck: readList("S1")}
	run(context.Background(), f, ss, mem)
	if f.read != 1 {
		t.Fatalf("read=%d", f.read)
	}
}

func TestCancelStopsCallsAndEmitsNoAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 10)
	f := &fake{onCheck: readList(aliasList(5)...)}
	f.onRead = func(c context.Context, p string) (string, error) {
		started <- struct{}{}
		<-c.Done()
		return "", c.Err()
	}
	go func() { <-started; cancel() }()
	evs := run(ctx, f, sessions(t, 5, true), nil)
	for _, e := range evs {
		if e.Type == "done" || e.Type == "error" {
			t.Fatalf("unexpected %+v", e)
		}
	}
	if len(ofType(evs, "read")) != 0 || f.combine != 0 {
		t.Fatalf("events=%+v combine=%d", evs, f.combine)
	}
}

const negativeNote = "Read for this question: nothing found."

func TestNothingReadLeavesANegativeNoteOnTheReadEvent(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2"), onRead: func(context.Context, string) (string, error) { return "NOTHING", nil }}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	reads := ofType(evs, "read")
	if len(reads) != 2 {
		t.Fatalf("reads = %+v", reads)
	}
	for _, e := range reads {
		if e.Note == nil || e.Note.Text != negativeNote || e.Note.SessionID == "" || e.Note.Label == "" {
			t.Fatalf("read event = %+v", e)
		}
	}
	r := last(evs).Result
	if f.combine != 0 || r.Answer != "I didn't find it in what I read." || len(r.Sources) != 0 {
		t.Fatalf("combine=%d result=%+v", f.combine, r)
	}
}

func TestNegativeNotesAreNeitherFindingsNorSources(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2")}
	f.onRead = func(_ context.Context, p string) (string, error) {
		if strings.Contains(p, "id2") {
			return "NOTHING", nil
		}
		return "- 09:30 found it", nil
	}
	var combinePrompt string
	f.onCombine = func(_ context.Context, p string) (string, error) {
		combinePrompt = p
		return `{"answer":"c","sources":[{"session":"S1","time":"09:30","why":"w"}]}`, nil
	}
	evs := run(context.Background(), f, sessions(t, 2, true), nil)
	if strings.Contains(combinePrompt, negativeNote) || !strings.Contains(combinePrompt, "found it") {
		t.Fatalf("combine prompt = %q", combinePrompt)
	}
	if r := last(evs).Result; len(r.Sources) != 1 || r.Sources[0].SessionID != "id1" {
		t.Fatalf("sources = %+v", r.Sources)
	}
	neg := 0
	for _, e := range ofType(evs, "read") {
		if e.Note != nil && e.Note.Text == negativeNote {
			neg++
		}
	}
	if neg != 1 {
		t.Fatalf("negative notes = %d", neg)
	}
}

func TestFileReadErrorMentioningQuotaIsNotAUsageLimit(t *testing.T) {
	ss := sessions(t, 2, true)
	for i := range ss {
		ss[i].TranscriptPath = filepath.Join(t.TempDir(), "disk quota exceeded.jsonl")
	}
	f := &fake{onCheck: readList("S1", "S2")}
	e := last(run(context.Background(), f, ss, nil))
	if e.Type != "error" || strings.Contains(e.Error, "usage limit") || !strings.Contains(e.Error, "quota") {
		t.Fatalf("last = %+v", e)
	}
}

func TestCombineWithOnlyInventedSourcesFallsBackToNoteSessions(t *testing.T) {
	f := &fake{onCheck: readList("S1", "S2"), onCombine: func(context.Context, string) (string, error) {
		return `{"answer":"combined","sources":[{"session":"S99","time":"09:30","why":"w"}]}`, nil
	}}
	r := last(run(context.Background(), f, sessions(t, 2, true), nil)).Result
	if r.Answer != "combined" || len(r.Sources) != 2 {
		t.Fatalf("result = %+v", r)
	}
}
