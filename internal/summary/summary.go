package summary

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/watch"
)

// Summary levels for what a session did on one local day, keyed by DayKey.
// LevelDay holds the bullet points as a JSON string array in Structured;
// LevelDetail holds the fuller on-demand summary in Narrative. Both carry the
// session's LastActivityAt they cover as AsOf.
const (
	LevelDay    = "session_day"
	LevelDetail = "session_detail"
)

// PointsEvery is the shortest gap between two regenerations of a running
// session's day points, so a busy session does not spend subscription usage on
// every pass. A session that hands back is not held to it.
const PointsEvery = 15 * time.Minute

// DayKey is the subject id of a session's summary for day's local date.
func DayKey(sessionID string, day time.Time) string {
	return sessionID + "/" + day.Format("2006-01-02")
}

// warrantsNarrative reports whether a session's status is one worth narrating
// at once: it has handed back. A running session is still changing, so it is
// narrated at most every PointsEvery.
func warrantsNarrative(status string) bool {
	return status == "waiting"
}

// Summarize writes a session's day points, when a narrator is present and the
// session was active on now's local day. A narrator or prompt-build failure is
// logged and leaves any prior points intact with their watermark, so the next
// run retries; it is never returned up.
func Summarize(ctx context.Context, st *store.Store, se store.Session, n Narrator, now time.Time) error {
	n = forAgent(n, se.Agent)
	if n == nil || !sameDay(se.LastActivityAt, now) {
		return nil
	}

	key := DayKey(se.ID, now)
	existing, had, err := st.GetSummary(LevelDay, key)
	if err != nil {
		return err
	}
	had = had && existing.SchemaVersion >= PointsSchema // older points are redone
	if had && !se.LastActivityAt.After(existing.AsOf) {
		return nil // the points already cover the latest activity
	}
	// Gate on the live-derived status, the same source BuildView uses for lane
	// placement, so a card shown as waiting/done is narrated without waiting for
	// the watcher's next sweep to rewrite the stored Status column.
	status := watch.SessionStatus(now, se)
	if had && !warrantsNarrative(status) && now.Sub(existing.AsOf) < PointsEvery {
		return nil
	}

	prompt, err := BuildDayPrompt(se.TranscriptPath, pointsPreamble, now, PointsBudget)
	if err != nil {
		log.Printf("summary: prompt for %s: %v", se.ID, err)
		return nil
	}
	if prompt == "" {
		return nil // activity today, but no human or assistant text yet
	}
	text, err := n.Narrate(ctx, prompt)
	if err != nil {
		log.Printf("summary: narrate %s: %v", se.ID, err)
		return nil
	}
	_, err = storePoints(st, se, key, text)
	return err
}

// storePoints parses a narration into se's points under key and stores them.
func storePoints(st *store.Store, se store.Session, key, text string) (store.Summary, error) {
	points, err := json.Marshal(ParsePoints(text))
	if err != nil {
		return store.Summary{}, err
	}
	sum := store.Summary{
		Level:         LevelDay,
		SubjectID:     key,
		AsOf:          se.LastActivityAt,
		Structured:    string(points),
		SchemaVersion: PointsSchema,
	}
	return sum, st.UpsertSummary(sum)
}

// RefreshPoints brings se's points for now's local day up to date when a person
// asks: unlike Summarize it does not wait out PointsEvery, and a failure is
// returned, since a person is waiting on it. Points that already cover the
// latest activity are returned as they are, with no narration. ok is false when
// there is nothing to summarize that day.
func RefreshPoints(ctx context.Context, st *store.Store, se store.Session, n Narrator, now time.Time) (sum store.Summary, ok bool, err error) {
	if !sameDay(se.LastActivityAt, now) {
		return store.Summary{}, false, nil
	}
	key := DayKey(se.ID, now)
	cached, had, err := st.GetSummary(LevelDay, key)
	if err != nil {
		return store.Summary{}, false, err
	}
	if had && cached.SchemaVersion >= PointsSchema && !se.LastActivityAt.After(cached.AsOf) {
		return cached, true, nil
	}
	prompt, err := BuildDayPrompt(se.TranscriptPath, pointsPreamble, now, PointsBudget)
	if err != nil || prompt == "" {
		return store.Summary{}, false, err
	}
	narrator := forAgent(n, se.Agent)
	if narrator == nil {
		return store.Summary{}, false, ErrNoNarrator
	}
	text, err := narrator.Narrate(ctx, prompt)
	if err != nil {
		return store.Summary{}, false, err
	}
	sum, err = storePoints(st, se, key, text)
	return sum, err == nil, err
}

// Detail returns the fuller summary of what se did on now's local day: the
// cached one while the session has had no activity since it was made and its
// format is current, else a fresh narration that it caches. ok is false when the session has no
// activity that day. Unlike Summarize, a failure is returned, since a person
// is waiting on it.
func Detail(ctx context.Context, st *store.Store, se store.Session, n Narrator, now time.Time) (sum store.Summary, ok bool, err error) {
	if !sameDay(se.LastActivityAt, now) {
		return store.Summary{}, false, nil
	}
	key := DayKey(se.ID, now)
	cached, had, err := st.GetSummary(LevelDetail, key)
	if err != nil {
		return store.Summary{}, false, err
	}
	if had && cached.SchemaVersion >= DetailSchema && !se.LastActivityAt.After(cached.AsOf) {
		return cached, true, nil
	}
	prompt, err := BuildDayPrompt(se.TranscriptPath, detailPreamble, now, DetailBudget)
	if err != nil || prompt == "" {
		return store.Summary{}, false, err
	}
	narrator := forAgent(n, se.Agent)
	if narrator == nil {
		return store.Summary{}, false, ErrNoNarrator
	}
	text, err := narrator.Narrate(ctx, prompt)
	if err != nil {
		return store.Summary{}, false, err
	}
	sum = store.Summary{Level: LevelDetail, SubjectID: key, AsOf: se.LastActivityAt, Narrative: text, SchemaVersion: DetailSchema}
	return sum, true, st.UpsertSummary(sum)
}

// PastPoints returns se's points for day, a local midnight before today: the
// stored ones when there are any, else a narration of that day's turns, which
// it stores so the day is narrated once. A failure is returned and nothing is
// stored, so the next ask tries again.
func PastPoints(ctx context.Context, st *store.Store, se store.Session, n Narrator, day time.Time) ([]string, error) {
	key := DayKey(se.ID, day)
	if cached, had, err := st.GetSummary(LevelDay, key); err != nil || (had && cached.SchemaVersion >= PointsSchema) {
		var points []string
		json.Unmarshal([]byte(cached.Structured), &points)
		return points, err
	}
	prompt, err := BuildDayPrompt(se.TranscriptPath, pointsPreamble, day, PointsBudget)
	if err != nil || prompt == "" {
		return nil, err
	}
	narrator := forAgent(n, se.Agent)
	if narrator == nil {
		return nil, ErrNoNarrator
	}
	text, err := narrator.Narrate(ctx, prompt)
	if err != nil {
		return nil, err
	}
	points := ParsePoints(text)
	structured, err := json.Marshal(points)
	if err != nil {
		return nil, err
	}
	return points, st.UpsertSummary(store.Summary{Level: LevelDay, SubjectID: key, AsOf: se.LastActivityAt, Structured: string(structured), SchemaVersion: PointsSchema})
}

// SummarizeAll summarizes every stored session as of now, continuing past a
// per-session store error. It returns how many sessions were summarized and
// how many got new day points this run.
func SummarizeAll(ctx context.Context, st *store.Store, n Narrator, now time.Time) (sessions, narrated int, err error) {
	all, err := st.ListSessions()
	if err != nil {
		return 0, 0, err
	}
	for _, se := range all {
		key := DayKey(se.ID, now)
		before, _, _ := st.GetSummary(LevelDay, key)
		if e := Summarize(ctx, st, se, n, now); e != nil {
			log.Printf("summary: %s: %v", se.ID, e)
			continue
		}
		sessions++
		if after, ok, _ := st.GetSummary(LevelDay, key); ok && !after.AsOf.Equal(before.AsOf) {
			narrated++
		}
	}
	return sessions, narrated, nil
}
