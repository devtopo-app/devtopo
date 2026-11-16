package summary

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

// now is the test clock: noon on the day writeTranscript's turns are stamped.
var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

type fakeNarrator struct {
	calls  int
	text   string
	fail   bool
	prompt string
}

func (f *fakeNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	f.calls++
	f.prompt = prompt
	if f.fail {
		return "", context.DeadlineExceeded
	}
	return f.text, nil
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// writeTranscript writes a minimal real transcript and returns its cleaned path.
func writeTranscript(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := `{"type":"user","timestamp":"2026-10-23T09:00:00Z","message":{"role":"user","content":"do the thing"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-23T09:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"done the thing"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(p)
}

// putSession stores a session whose last event is at last with the given tail,
// so its live status under now is what the caller means: ai_done an hour
// before now derives "waiting", ai_working a minute before derives "running".
func putSession(t *testing.T, st *store.Store, id, tail, tpath string, last time.Time, prompts int) store.Session {
	t.Helper()
	var fs []facts.Fact
	for i := 0; i < prompts; i++ {
		fs = append(fs, facts.Fact{Type: "prompt"})
	}
	if err := st.Apply(store.Delta{
		Project: store.Project{Key: "k", Name: "n"},
		Session: store.Session{
			ID: id, ProjectKey: "k", TranscriptPath: tpath,
			StartedAt: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC), LastActivityAt: last,
			LastEventAt: last, Tail: tail,
		},
		Facts: fs, CursorPath: id, CursorOffset: 1,
	}); err != nil {
		t.Fatal(err)
	}
	se, _, _ := st.GetSession(id)
	return se
}

func dayPoints(t *testing.T, st *store.Store, id string) (store.Summary, bool) {
	t.Helper()
	sum, ok, err := st.GetSummary(LevelDay, DayKey(id, now))
	if err != nil {
		t.Fatal(err)
	}
	return sum, ok
}

func TestSummarizeWithoutNarratorWritesNothing(t *testing.T) {
	st := openStore(t)
	se := putSession(t, st, "s1", facts.TailAIDone, "", now.Add(-time.Hour), 3)
	if err := Summarize(context.Background(), st, se, nil, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := dayPoints(t, st, "s1"); ok {
		t.Fatal("no narrator means no day points")
	}
}

func TestSummarizeStoresTodayPoints(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s2.jsonl")
	se := putSession(t, st, "s2", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "- did the thing\n- tested it"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.prompt, "do the thing") {
		t.Fatalf("the prompt carries today's turns:\n%s", n.prompt)
	}
	sum, ok := dayPoints(t, st, "s2")
	if !ok || sum.Structured != `["did the thing","tested it"]` {
		t.Fatalf("day points: %+v ok=%v", sum, ok)
	}
	if !sum.AsOf.Equal(se.LastActivityAt) {
		t.Fatalf("as of is the activity covered, got %s", sum.AsOf)
	}
}

func TestSummarizeSkipsSessionWithNoActivityToday(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s3.jsonl")
	se := putSession(t, st, "s3", facts.TailAIDone, tp, now.Add(-24*time.Hour), 1)
	n := &fakeNarrator{text: "- x"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if n.calls != 0 {
		t.Fatalf("no activity today, no narration: calls=%d", n.calls)
	}
}

func TestSummarizeRegeneratesOnlyOnNewActivity(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s4.jsonl")
	se := putSession(t, st, "s4", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "- v"}
	for i := 0; i < 2; i++ {
		if err := Summarize(context.Background(), st, se, n, now); err != nil {
			t.Fatal(err)
		}
	}
	if n.calls != 1 {
		t.Fatalf("unchanged session must narrate once, calls=%d", n.calls)
	}
	se.LastActivityAt = now.Add(-30 * time.Minute) // handed back again since
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if n.calls != 2 {
		t.Fatalf("a waiting session with new activity is re-narrated at once, calls=%d", n.calls)
	}
}

func TestSummarizeThrottlesARunningSession(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s5.jsonl")
	se := putSession(t, st, "s5", facts.TailAIWorking, tp, now.Add(-time.Minute), 1)
	n := &fakeNarrator{text: "- v"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if n.calls != 1 {
		t.Fatalf("a running session with no points yet gets them, calls=%d", n.calls)
	}

	// Ten minutes on, still running with new activity: too soon.
	later := now.Add(10 * time.Minute)
	se.LastActivityAt, se.LastEventAt = later, later
	if err := Summarize(context.Background(), st, se, n, later); err != nil {
		t.Fatal(err)
	}
	if n.calls != 1 {
		t.Fatalf("a running session is re-narrated at most every %s, calls=%d", PointsEvery, n.calls)
	}

	// Fifteen minutes after the stored points, it is due.
	later = now.Add(14 * time.Minute)
	se.LastActivityAt, se.LastEventAt = later, later
	if err := Summarize(context.Background(), st, se, n, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n.calls != 2 {
		t.Fatalf("a running session is re-narrated once %s has passed, calls=%d", PointsEvery, n.calls)
	}
}

func TestSummarizeRetriesAfterFailedNarration(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "sr.jsonl")
	se := putSession(t, st, "sr", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "- v1"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}

	// New activity, but narration fails: the old points stay and the watermark
	// does not advance, so the next run retries.
	se.LastActivityAt = now.Add(-30 * time.Minute)
	n.fail, n.text = true, "- v2"
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if mid, _ := dayPoints(t, st, "sr"); mid.Structured != `["v1"]` {
		t.Fatalf("failed narration must keep the old points, got %s", mid.Structured)
	}
	n.fail = false
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := dayPoints(t, st, "sr"); got.Structured != `["v2"]` {
		t.Fatalf("a failed narration must be retried on the next run, got %s", got.Structured)
	}
}

func TestSummarizeAllContinuesPastAFailure(t *testing.T) {
	st := openStore(t)
	dir := t.TempDir()
	putSession(t, st, "good", facts.TailAIDone, writeTranscript(t, dir, "good.jsonl"), now.Add(-time.Hour), 1)
	putSession(t, st, "bad", facts.TailAIDone, filepath.Join(dir, "missing.jsonl"), now.Add(-time.Hour), 1)

	n := &fakeNarrator{text: "- ok"}
	sessions, narrated, err := SummarizeAll(context.Background(), st, n, now)
	if err != nil {
		t.Fatalf("SummarizeAll must not fail on one bad file: %v", err)
	}
	if sessions != 2 || narrated != 1 {
		t.Fatalf("both summarized, only the good one narrated: sessions=%d narrated=%d", sessions, narrated)
	}
	if _, ok := dayPoints(t, st, "bad"); ok {
		t.Fatal("bad session must have no day points")
	}
}

func TestDetailCachesUntilNewActivity(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "sd.jsonl")
	se := putSession(t, st, "sd", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "What was done\n- the thing"}

	got, ok, err := Detail(context.Background(), st, se, n, now)
	if err != nil || !ok || got.Narrative != n.text {
		t.Fatalf("detail: %+v ok=%v err=%v", got, ok, err)
	}
	if !strings.HasPrefix(n.prompt, detailPreamble) || !strings.Contains(n.prompt, "do the thing") {
		t.Fatalf("detail prompt:\n%s", n.prompt)
	}
	if _, _, err := Detail(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if n.calls != 1 {
		t.Fatalf("an unchanged session's detail is cached, calls=%d", n.calls)
	}
	se.LastActivityAt = now.Add(-30 * time.Minute)
	if _, _, err := Detail(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	if n.calls != 2 {
		t.Fatalf("new activity makes a fresh detail, calls=%d", n.calls)
	}
}

func TestRefreshPointsIgnoresTheThrottle(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "rt.jsonl")
	se := putSession(t, st, "rt", facts.TailAIWorking, tp, now.Add(-time.Minute), 1)
	n := &fakeNarrator{text: "- v1"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}

	// Ten minutes on, still running with new activity: the loop would wait.
	later := now.Add(10 * time.Minute)
	se.LastActivityAt, se.LastEventAt = later, later
	n.text = "- v2"
	got, ok, err := RefreshPoints(context.Background(), st, se, n, later)
	if err != nil || !ok {
		t.Fatalf("refresh: ok=%v err=%v", ok, err)
	}
	if got.Structured != `["v2"]` || !got.AsOf.Equal(later) {
		t.Fatalf("refreshed points: %+v", got)
	}
	if stored, _ := dayPoints(t, st, "rt"); stored.Structured != `["v2"]` {
		t.Fatalf("refreshed points are stored: %+v", stored)
	}
}

func TestRefreshPointsKeepsPointsThatCoverTheLatestActivity(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "rc.jsonl")
	se := putSession(t, st, "rc", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "- v1"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	got, ok, err := RefreshPoints(context.Background(), st, se, n, now)
	if err != nil || !ok || got.Structured != `["v1"]` {
		t.Fatalf("refresh: %+v ok=%v err=%v", got, ok, err)
	}
	if n.calls != 1 {
		t.Fatalf("nothing new to narrate, calls=%d", n.calls)
	}
}

func TestRefreshPointsNoActivityToday(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "rn.jsonl")
	se := putSession(t, st, "rn", facts.TailAIDone, tp, now.Add(-24*time.Hour), 1)
	n := &fakeNarrator{text: "x"}
	if _, ok, err := RefreshPoints(context.Background(), st, se, n, now); err != nil || ok || n.calls != 0 {
		t.Fatalf("no activity today: ok=%v err=%v calls=%d", ok, err, n.calls)
	}
}

func TestRefreshPointsReturnsNarratorErrorAndKeepsOldPoints(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "re.jsonl")
	se := putSession(t, st, "re", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	n := &fakeNarrator{text: "- v1"}
	if err := Summarize(context.Background(), st, se, n, now); err != nil {
		t.Fatal(err)
	}
	se.LastActivityAt = now.Add(-30 * time.Minute)
	n.fail = true
	if _, _, err := RefreshPoints(context.Background(), st, se, n, now); err == nil {
		t.Fatal("a failed narration is returned to the caller")
	}
	if kept, _ := dayPoints(t, st, "re"); kept.Structured != `["v1"]` {
		t.Fatalf("a failed refresh keeps the old points: %+v", kept)
	}
}

func TestRefreshPointsWithoutATool(t *testing.T) {
	st := openStore(t)
	se := putSession(t, st, "ru", facts.TailAIDone, writeTranscript(t, t.TempDir(), "ru.jsonl"), now.Add(-time.Hour), 1)
	if _, _, err := RefreshPoints(context.Background(), st, se, (&fakeTools{}).selector(t), now); !errors.Is(err, ErrNoNarrator) {
		t.Fatalf("refresh: %v", err)
	}
}

func TestDetailRewritesAnOlderFormat(t *testing.T) {
	st := openStore(t)
	se := putSession(t, st, "so", facts.TailAIDone, writeTranscript(t, t.TempDir(), "so.jsonl"), now.Add(-time.Hour), 1)
	if err := st.UpsertSummary(store.Summary{Level: LevelDetail, SubjectID: DayKey("so", now), AsOf: se.LastActivityAt, Narrative: "Decisions\n- old", SchemaVersion: DetailSchema - 1}); err != nil {
		t.Fatal(err)
	}
	n := &fakeNarrator{text: "What you decided\n- new"}
	got, ok, err := Detail(context.Background(), st, se, n, now)
	if err != nil || !ok || got.Narrative != n.text || got.SchemaVersion != DetailSchema {
		t.Fatalf("detail: %+v ok=%v err=%v", got, ok, err)
	}
	if n.calls != 1 {
		t.Fatalf("a detail in an older format is written again, calls=%d", n.calls)
	}
}

func TestDetailNoActivityToday(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "sn.jsonl")
	se := putSession(t, st, "sn", facts.TailAIDone, tp, now.Add(-24*time.Hour), 1)
	n := &fakeNarrator{text: "x"}
	if _, ok, err := Detail(context.Background(), st, se, n, now); err != nil || ok || n.calls != 0 {
		t.Fatalf("no activity today: ok=%v err=%v calls=%d", ok, err, n.calls)
	}
}

func TestDetailReturnsNarratorError(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "se.jsonl")
	se := putSession(t, st, "se", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	if _, _, err := Detail(context.Background(), st, se, &fakeNarrator{fail: true}, now); err == nil {
		t.Fatal("a failed narration is returned to the caller")
	}
	if _, ok, _ := st.GetSummary(LevelDetail, DayKey("se", now)); ok {
		t.Fatal("a failed detail is not cached")
	}
}

// A tool can go away while DevTopo runs, so these meet a Selector with none.
func TestDetailAndPastPointsWithoutATool(t *testing.T) {
	st := openStore(t)
	se := putSession(t, st, "st", facts.TailAIDone, writeTranscript(t, t.TempDir(), "st.jsonl"), now.Add(-time.Hour), 1)
	s := (&fakeTools{}).selector(t)
	if _, _, err := Detail(context.Background(), st, se, s, now); !errors.Is(err, ErrNoNarrator) {
		t.Fatalf("detail: %v", err)
	}
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if _, err := PastPoints(context.Background(), st, se, s, day); !errors.Is(err, ErrNoNarrator) {
		t.Fatalf("past points: %v", err)
	}
}

func TestSummarizeWithoutAToolWritesNoPoints(t *testing.T) {
	st := openStore(t)
	se := putSession(t, st, "sm", facts.TailAIDone, writeTranscript(t, t.TempDir(), "sm.jsonl"), now.Add(-time.Hour), 1)
	if err := Summarize(context.Background(), st, se, (&fakeTools{}).selector(t), now); err != nil {
		t.Fatal(err)
	}
	if _, ok := dayPoints(t, st, "sm"); ok {
		t.Fatal("no points with no tool")
	}
}

// A Codex session is summarized by Codex, also on demand for a past day.
func TestCodexSessionIsSummarizedByCodex(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s11.jsonl")
	se := putSession(t, st, "s11", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	se.Agent = "codex"
	claude, codex := &fakeNarrator{text: "- a"}, &fakeNarrator{text: "- b"}
	r := NewSelector(func(string) (string, bool, error) { return "", false, nil }, Backends{
		ProbeClaude: func(context.Context) bool { return true },
		ProbeCodex:  func(context.Context) bool { return true },
		NewClaude:   func(string) Narrator { return claude },
		NewCodex:    func(string) Narrator { return codex },
	})
	r.Probe(context.Background())
	if err := Summarize(context.Background(), st, se, r, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Detail(context.Background(), st, se, r, now); err != nil {
		t.Fatal(err)
	}
	past := putSession(t, st, "s12", facts.TailAIDone, writeTranscript(t, t.TempDir(), "s12.jsonl"), now.Add(-time.Hour), 1)
	past.Agent = "codex"
	if _, err := PastPoints(context.Background(), st, past, r, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if claude.calls != 0 || codex.calls != 3 {
		t.Fatalf("claude=%d codex=%d", claude.calls, codex.calls)
	}
}

// Points written under older instructions are written again in the current
// format, once, both for today and for a past day.
func TestOldFormatPointsAreRewrittenOnce(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s10.jsonl")
	se := putSession(t, st, "s10", facts.TailAIDone, tp, now.Add(-time.Hour), 1)
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	old := store.Summary{Level: LevelDay, SubjectID: DayKey("s10", now), AsOf: se.LastActivityAt, Structured: `["**Old** style"]`, SchemaVersion: 1}
	if err := st.UpsertSummary(old); err != nil {
		t.Fatal(err)
	}
	n := &fakeNarrator{text: "- Fixed the thing."}
	for i := 0; i < 2; i++ {
		if err := Summarize(context.Background(), st, se, n, now); err != nil {
			t.Fatal(err)
		}
	}
	if sum, _ := dayPoints(t, st, "s10"); n.calls != 1 || sum.Structured != `["Fixed the thing."]` {
		t.Fatalf("today: calls=%d points=%s", n.calls, sum.Structured)
	}

	_ = st.UpsertSummary(old)
	for i := 0; i < 2; i++ {
		if points, err := PastPoints(context.Background(), st, se, n, day); err != nil || points[0] != "Fixed the thing." {
			t.Fatalf("past day: %v %v", points, err)
		}
	}
	if n.calls != 2 {
		t.Fatalf("a past day is rewritten once, calls=%d", n.calls)
	}
}

// A past day DevTopo never summarized gets its points on demand, once: the
// second ask reads them back without calling the narrator.
func TestPastPointsNarratesOnceThenCaches(t *testing.T) {
	st := openStore(t)
	tp := writeTranscript(t, t.TempDir(), "s9.jsonl")
	se := putSession(t, st, "s9", facts.TailAIDone, tp, time.Date(2026, 10, 23, 9, 0, 2, 0, time.UTC), 1)
	n := &fakeNarrator{text: "- did the thing"}
	day := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		points, err := PastPoints(context.Background(), st, se, n, day)
		if err != nil {
			t.Fatal(err)
		}
		if len(points) != 1 || points[0] != "did the thing" {
			t.Fatalf("points: %v", points)
		}
	}
	if n.calls != 1 || !strings.Contains(n.prompt, "do the thing") {
		t.Fatalf("calls=%d prompt=%q", n.calls, n.prompt)
	}
	if sum, ok, _ := st.GetSummary(LevelDay, DayKey("s9", day)); !ok || sum.Structured != `["did the thing"]` {
		t.Fatalf("cached: %+v %v", sum, ok)
	}
}
