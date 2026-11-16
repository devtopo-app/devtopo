package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

func openOwnPRStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOwnPRDoneMarks(t *testing.T) {
	st := openOwnPRStore(t)
	noon := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, u := range []string{"a", "b"} {
		if err := st.MarkOwnPRDone(u, noon); err != nil {
			t.Fatal(err)
		}
	}
	marks, err := st.OwnPRsDone()
	if err != nil || len(marks) != 2 || !marks["a"].Equal(noon) {
		t.Fatalf("marks %v err %v", marks, err)
	}
	if err := st.MarkOwnPRDone("a", noon.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if marks, _ = st.OwnPRsDone(); !marks["a"].Equal(noon.Add(time.Hour)) {
		t.Fatalf("marking again must move the time on: %v", marks)
	}
	if err := st.UnmarkOwnPRDone("a"); err != nil {
		t.Fatal(err)
	}
	if marks, _ = st.OwnPRsDone(); len(marks) != 1 {
		t.Fatalf("unmark: %v", marks)
	}
	if err := st.MarkOwnPRDone("a", noon); err != nil {
		t.Fatal(err)
	}
	if err := st.KeepOwnPRsDone([]string{"a"}); err != nil {
		t.Fatal(err)
	}
	if marks, _ = st.OwnPRsDone(); len(marks) != 1 || marks["a"].IsZero() {
		t.Fatalf("keep: %v", marks)
	}
}

func TestExternalItemKeepsReviewer(t *testing.T) {
	st := openOwnPRStore(t)
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	check := func(how string) {
		t.Helper()
		items, err := st.ListExternalItems("github")
		if err != nil || len(items) != 1 || items[0].Reviewer != "alex" || !items[0].ReviewedAt.Equal(at) {
			t.Fatalf("%s: %+v err %v", how, items, err)
		}
	}
	it := store.ExternalItem{Source: "github", Kind: "own_pr", ExternalID: "own:u", URL: "u", Reviewer: "alex", ReviewedAt: at, UpdatedAt: at}
	if err := st.ReplaceExternalItems("github", []store.ExternalItem{it}); err != nil {
		t.Fatal(err)
	}
	check("replace")
	if err := st.ReplaceExternalItems("github", nil); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertExternalItem(it); err != nil {
		t.Fatal(err)
	}
	check("upsert")
}

func TestTaskStarts(t *testing.T) {
	st := openOwnPRStore(t)
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	apply := func(id string, found ...facts.Fact) {
		t.Helper()
		err := st.Apply(store.Delta{Project: store.Project{Key: "p", Name: "p"}, Session: store.Session{ID: id, ProjectKey: "p"}, CursorPath: id, Facts: found})
		if err != nil {
			t.Fatal(err)
		}
	}
	apply("a",
		facts.Fact{Type: "prompt", Timestamp: at.Add(-time.Hour)},
		facts.Fact{Type: "prompt", Timestamp: at},
		facts.Fact{Type: "tool_use", Timestamp: at.Add(time.Hour)},
	)
	apply("b", facts.Fact{Type: "tool_use", Timestamp: at})
	// A picked-up task and an ended turn count as starts too; the latest wins.
	apply("c", facts.Fact{Type: "prompt", Timestamp: at}, facts.Fact{Type: "turn_end", Timestamp: at.Add(time.Minute)}, facts.Fact{Type: "resume", Timestamp: at.Add(2 * time.Minute)})
	apply("d", facts.Fact{Type: "prompt", Timestamp: at}, facts.Fact{Type: "turn_end", Timestamp: at.Add(3 * time.Minute)})
	got, err := st.TaskStarts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !got["a"].Equal(at) || !got["c"].Equal(at.Add(2*time.Minute)) || !got["d"].Equal(at.Add(3*time.Minute)) {
		t.Fatalf("got: %v", got)
	}
	// Asked for some sessions, it reads only those.
	if some, _ := st.TaskStarts("a", "b"); len(some) != 1 || !some["a"].Equal(at) {
		t.Fatalf("only a and b: %v", some)
	}
}

// One day's summaries, not every day's.
func TestSummariesEndingIn(t *testing.T) {
	st := openOwnPRStore(t)
	for _, id := range []string{"s1/2026-11-15", "s1/2026-11-16", "s2/2026-11-16"} {
		if err := st.UpsertSummary(store.Summary{Level: "session_day", SubjectID: id}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.SummariesEndingIn("session_day", "/2026-11-16")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestOpenBackgroundTasks(t *testing.T) {
	st := openOwnPRStore(t)
	at := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	for path, pending := range map[string]map[string]time.Time{"a.jsonl": {"t1": at, "t2": at}, "b.jsonl": {}} {
		err := st.Apply(store.Delta{Project: store.Project{Key: "p", Name: "p"}, Session: store.Session{ID: path, ProjectKey: "p", TranscriptPath: path},
			CursorPath: path, CursorBoundary: facts.Boundary{Time: at, Valid: true, Pending: pending, Tasks: map[string]string{"b1": "t1"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Only t1 has a task id: a launch recorded before ids were kept can't be told
	// stopped from running, so it isn't counted.
	got, err := st.OpenBackgroundTasks()
	if err != nil || got["a.jsonl"] != 1 || got["b.jsonl"] != 0 {
		t.Fatalf("got %v err %v", got, err)
	}
	b, err := st.CursorBoundary("a.jsonl")
	if err != nil || b.Tasks["b1"] != "t1" {
		t.Fatalf("task ids must persist: %+v %v", b, err)
	}
}

// Merging a continued session drops every summary of the old one, its day
// points and details included, and keeps the new one's.
func TestMergeContinuedDropsTheOldSummaries(t *testing.T) {
	st := openOwnPRStore(t)
	for _, id := range []string{"old", "old/2026-11-16", "old-other/2026-11-16", "new/2026-11-16"} {
		if err := st.UpsertSummary(store.Summary{Level: "session_day", SubjectID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MergeContinued("old", "new"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.ListSummaries("session_day")
	var left []string
	for _, s := range got {
		left = append(left, s.SubjectID)
	}
	if len(left) != 2 || !(contains(left, "old-other/2026-11-16") && contains(left, "new/2026-11-16")) {
		t.Fatalf("left: %v", left)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
