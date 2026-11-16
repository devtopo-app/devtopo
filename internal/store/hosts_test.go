package store_test

import (
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/store"
)

// The app a session last ran in survives re-ingest, so a closed session still
// knows where it ran, and a later detection replaces it.
func TestSessionHostsSurviveIngestAndUpdate(t *testing.T) {
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
	if err := st.SetSessionHost("s1", "vscode"); err != nil {
		t.Fatal(err)
	}
	apply()
	if hosts, _ := st.SessionHosts(); hosts["s1"] != "vscode" {
		t.Fatalf("host lost: %v", hosts)
	}
	if err := st.SetSessionHost("s1", "orca"); err != nil {
		t.Fatal(err)
	}
	if hosts, _ := st.SessionHosts(); hosts["s1"] != "orca" || len(hosts) != 1 {
		t.Fatalf("host not replaced: %v", hosts)
	}
}
