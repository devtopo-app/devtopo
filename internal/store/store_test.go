package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

func TestApplyIsAtomicAndReadsBack(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d := store.Delta{
		Project: store.Project{Key: "github.com/x/y", Name: "y"},
		Session: store.Session{
			ID: "s1", ProjectKey: "github.com/x/y", Agent: "claude", CWD: `C:\r`, Branch: "main",
			StartedAt: time.Unix(0, 0).UTC(), LastActivityAt: time.Unix(10, 0).UTC(),
			LastEventAt: time.Unix(10, 0).UTC(), LastEventIsAI: true, AIActiveSeconds: 8, Status: "",
		},
		Facts:        []facts.Fact{{Type: "prompt", Timestamp: time.Unix(0, 0).UTC()}},
		CursorPath:   `C:\p\s1.jsonl`,
		CursorOffset: 42,
	}
	if err := st.Apply(d); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetSession("s1")
	if err != nil || !ok {
		t.Fatalf("session missing: ok=%v err=%v", ok, err)
	}
	if got.AIActiveSeconds != 8 || !got.LastEventIsAI || got.ProjectKey != "github.com/x/y" {
		t.Fatalf("got %+v", got)
	}
	if !got.LastEventAt.Equal(time.Unix(10, 0).UTC()) {
		t.Fatalf("last event at %v", got.LastEventAt)
	}
	if c, _ := st.Cursor(`C:\p\s1.jsonl`); c != 42 {
		t.Fatalf("cursor %d", c)
	}
}

func TestCursorKeepsItsBoundary(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if b, err := st.CursorBoundary("nope"); err != nil || b.Valid {
		t.Fatalf("absent boundary must be invalid: %+v err=%v", b, err)
	}
	at := time.Date(2026, 10, 22, 9, 0, 0, 500, time.UTC)
	if err := st.Apply(store.Delta{
		Project: store.Project{Key: "k", Name: "k"}, Session: store.Session{ID: "s", ProjectKey: "k"},
		CursorPath: "p", CursorOffset: 9, CursorBoundary: facts.Boundary{Time: at, IsAI: true, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	b, err := st.CursorBoundary("p")
	if err != nil || !b.Valid || !b.IsAI || !b.Time.Equal(at) {
		t.Fatalf("boundary: %+v err=%v", b, err)
	}
}

func TestCursorAbsentIsZero(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if c, _ := st.Cursor("nope"); c != 0 {
		t.Fatalf("absent cursor must be 0, got %d", c)
	}
}

func TestGetSessionMissingReturnsFalse(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := st.GetSession("ghost"); err != nil || ok {
		t.Fatalf("missing session should be ok=false, got ok=%v err=%v", ok, err)
	}
}

func TestFactCounts(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.Close()
	if err := st.Apply(store.Delta{
		Project: store.Project{Key: "k", Name: "n"},
		Session: store.Session{ID: "s1", ProjectKey: "k"},
		Facts: []facts.Fact{
			{Type: "prompt"}, {Type: "prompt"},
			{Type: "tool_use", ToolName: "Bash"}, {Type: "tool_use", ToolName: "Edit"}, {Type: "tool_use", ToolName: "Read"},
		},
		CursorPath: "p", CursorOffset: 1,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.FactCounts("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got["prompt"] != 2 || got["tool_use"] != 3 {
		t.Fatalf("fact counts: %+v", got)
	}
	unknown, err := st.FactCounts("ghost")
	if err != nil {
		t.Fatal(err)
	}
	if unknown == nil || len(unknown) != 0 {
		t.Fatalf("unknown session must return a non-nil empty map, got %+v", unknown)
	}
}

func TestSummaryRoundTrip(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.Close()

	asOf := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	sum := store.Summary{
		Level: "session", SubjectID: "s1", AsOf: asOf,
		Narrative: "did a thing", Structured: `{"prompts":4}`, SchemaVersion: 1,
	}
	if err := st.UpsertSummary(sum); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetSummary("session", "s1")
	if err != nil || !ok {
		t.Fatalf("summary missing: ok=%v err=%v", ok, err)
	}
	if got.Narrative != "did a thing" || got.Structured != `{"prompts":4}` || got.SchemaVersion != 1 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.AsOf.Equal(asOf) {
		t.Fatalf("as_of round-trip: %v", got.AsOf)
	}

	// Second upsert for the same (level, subject_id) replaces, not duplicates.
	sum.Narrative = "did another thing"
	if err := st.UpsertSummary(sum); err != nil {
		t.Fatal(err)
	}
	all, err := st.ListSummaries("session")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected one summary after re-upsert, got %d", len(all))
	}
	if all[0].Narrative != "did another thing" {
		t.Fatalf("re-upsert must replace: %q", all[0].Narrative)
	}

	if _, ok, _ := st.GetSummary("session", "ghost"); ok {
		t.Fatal("unknown summary must be ok=false")
	}
}

func TestTailRoundTripAndListAndSetStatus(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.Close()
	if err := st.Apply(store.Delta{
		Project: store.Project{Key: "k", Name: "n"},
		Session: store.Session{ID: "s1", ProjectKey: "k", Tail: "ai_done", Status: "running",
			LastEventAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		CursorPath: "p", CursorOffset: 1,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := st.GetSession("s1")
	if !ok || got.Tail != "ai_done" {
		t.Fatalf("tail round-trip: %+v ok=%v", got, ok)
	}

	all, _ := st.ListSessions()
	if len(all) != 1 || all[0].ID != "s1" {
		t.Fatalf("ListSessions: %+v", all)
	}

	if err := st.SetStatus("s1", "waiting"); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := st.GetSession("s1")
	if got2.Status != "waiting" {
		t.Fatalf("SetStatus: %q", got2.Status)
	}
	if got2.Tail != "ai_done" || !got2.LastEventAt.Equal(got.LastEventAt) {
		t.Fatalf("SetStatus must touch only status: %+v", got2)
	}
}

func TestCursorKeepsPendingTasksAndSpans(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	at := time.Date(2026, 10, 22, 9, 0, 0, 500, time.UTC)
	want := facts.Boundary{
		Time: at, IsAI: true, Valid: true,
		Pending: map[string]time.Time{"b1": at.Add(-time.Minute)},
		Spans:   []facts.Span{{From: at.Add(-time.Minute), To: at}},
	}
	if err := st.Apply(store.Delta{
		Project: store.Project{Key: "k", Name: "k"}, Session: store.Session{ID: "s", ProjectKey: "k"},
		CursorPath: "p", CursorOffset: 9, CursorBoundary: want,
	}); err != nil {
		t.Fatal(err)
	}
	b, err := st.CursorBoundary("p")
	if err != nil || !b.Pending["b1"].Equal(want.Pending["b1"]) || len(b.Spans) != 1 ||
		!b.Spans[0].From.Equal(want.Spans[0].From) || !b.Spans[0].To.Equal(at) {
		t.Fatalf("boundary: %+v err=%v", b, err)
	}
}
