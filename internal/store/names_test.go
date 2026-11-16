package store_test

import (
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/store"
)

// A session's name is the user's: re-ingesting the session must not touch it,
// and an empty name goes back to the default.
func TestSessionNamesSurviveIngestAndClear(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	apply := func() {
		if err := st.Apply(store.Delta{Project: store.Project{Key: "k", Name: "k"}, Session: store.Session{ID: "s1", ProjectKey: "k"}, CursorPath: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	if err := st.SetSessionName("s1", "Bedrock fallback"); err != nil {
		t.Fatal(err)
	}
	apply()
	if names, _ := st.SessionNames(); names["s1"] != "Bedrock fallback" {
		t.Fatalf("name lost: %v", names)
	}
	if err := st.SetSessionName("s1", ""); err != nil {
		t.Fatal(err)
	}
	if names, _ := st.SessionNames(); len(names) != 0 {
		t.Fatalf("an empty name must clear it: %v", names)
	}
}
