package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/store"
)

func TestPromptsInRangeBySession(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	for i, fs := range [][]facts.Fact{
		{{Type: "prompt", Timestamp: day.Add(-time.Second)}, {Type: "prompt", Timestamp: day}, {Type: "assistant_turn", Timestamp: day.Add(time.Hour)}},
		{{Type: "prompt", Timestamp: day.Add(23*time.Hour + 500*time.Millisecond)}, {Type: "prompt", Timestamp: day.Add(24 * time.Hour)}},
	} {
		id := []string{"a", "b"}[i]
		if err := st.Apply(store.Delta{Project: store.Project{Key: "p"}, Session: store.Session{ID: id, ProjectKey: "p"}, Facts: fs, CursorPath: id}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.Prompts(day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 2 || n["a"] != 1 || n["b"] != 1 {
		t.Fatalf("want one prompt in [day, day+24h) in each session, got %v", n)
	}
}

func TestDecisionsCountsADay(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	fs := []facts.Fact{
		{Type: "decision", Timestamp: day.Add(-time.Second)},
		{Type: "decision", Timestamp: day},
		{Type: "decision", Timestamp: day.Add(time.Hour)},
		{Type: "decision", Timestamp: day.Add(24 * time.Hour)},
		{Type: "prompt", Timestamp: day.Add(time.Hour)},
	}
	if err := st.Apply(store.Delta{Project: store.Project{Key: "p"}, Session: store.Session{ID: "a", ProjectKey: "p"}, Facts: fs, CursorPath: "a"}); err != nil {
		t.Fatal(err)
	}
	n, err := st.Decisions(day, day.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 1 || n["a"] != 2 {
		t.Fatalf("want two decisions in [day, day+24h), got %v", n)
	}
}
