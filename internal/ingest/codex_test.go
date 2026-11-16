package ingest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/store"
)

const codexID = "0c0d0e0f-1111-7222-8333-944455556666"

// codexFixture lays the invented rollout out under <root>/sessions/YYYY/MM/DD
// the way Codex does, and returns its lines.
func codexFixture(t *testing.T) (root, path string, lines []string) {
	t.Helper()
	b, err := os.ReadFile("../transcript/testdata/codex.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "10", "21")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "rollout-2026-10-21T09-00-00-"+codexID+".jsonl")
	return root, path, strings.SplitAfter(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

func TestDiscoverCodexFindsRollouts(t *testing.T) {
	root, path, lines := codexFixture(t)
	write(t, path, strings.Join(lines, ""))
	write(t, filepath.Join(filepath.Dir(path), "notes.jsonl"), "")
	files, err := ingest.DiscoverCodex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("got %v", files)
	}
	if !ingest.IsCodexRollout(root, path) || ingest.IsCodexRollout(root, filepath.Join(filepath.Dir(path), "notes.jsonl")) {
		t.Fatal("IsCodexRollout disagrees with DiscoverCodex")
	}
}

// A rollout ingests as a codex session: its id, folder and branch from the
// session_meta, its tail from the turn events, and the Codex app as its host.
// A later batch with no session_meta takes the id from the file name and keeps
// the turn open.
func TestIngestCodexRollout(t *testing.T) {
	root, path, lines := codexFixture(t)
	write(t, path, strings.Join(lines[:15], "")) // up to the final answer, turn still open
	st, err := store.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, path); err != nil {
		t.Fatal(err)
	}
	se, ok, _ := st.GetSession(codexID)
	if !ok {
		t.Fatal("session not stored under the session_meta id")
	}
	if se.Agent != "codex" || se.CWD != `C:\repo` || se.Branch != "feat/x" || se.Tail != facts.TailAIWorking || se.AIActiveSeconds != 38 {
		t.Fatalf("session %+v", se)
	}
	if counts, _ := st.FactCounts(codexID); counts["prompt"] != 1 {
		t.Fatalf("facts %v", counts)
	}
	if hosts, _ := st.SessionHosts(); hosts[codexID] != host.CodexApp {
		t.Fatalf("hosts %v", hosts)
	}

	appendLine(t, path, strings.Join(lines[15:], ""))
	if _, err := ingest.IngestFile(st, fakeGit{}, path); err != nil {
		t.Fatal(err)
	}
	se, _, _ = st.GetSession(codexID)
	if se.Tail != facts.TailAIEnded || se.CWD != `C:\repo` || se.Agent != "codex" {
		t.Fatalf("after the turn ended: %+v", se)
	}
	if all, _ := st.ListSessions(); len(all) != 1 {
		t.Fatalf("the second batch must not make a session of its own: %+v", all)
	}
}

func TestIngestAllReadsCodexToo(t *testing.T) {
	root, path, lines := codexFixture(t)
	write(t, path, strings.Join(lines, ""))
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	if _, sessions, _, _, err := ingest.IngestAll(st, fakeGit{}, t.TempDir(), root); err != nil || sessions != 1 {
		t.Fatalf("sessions %d err %v", sessions, err)
	}
}

// A session's Codex title is its newest thread_name in session_index.jsonl.
func TestCodexTitles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "session_index.jsonl"),
		`{"id":"a","thread_name":"First name","updated_at":"2026-10-29T09:00:00Z"}`+"\n"+
			`{"id":"b","thread_name":"Other","updated_at":"2026-10-29T09:00:00Z"}`+"\n"+
			`not json`+"\n"+
			`{"id":"a","thread_name":"Renamed","updated_at":"2026-10-29T10:00:00Z"}`+"\n")
	got := ingest.CodexTitles(root)
	if len(got) != 2 || got["a"] != "Renamed" || got["b"] != "Other" {
		t.Fatalf("got %v", got)
	}
	if len(ingest.CodexTitles(t.TempDir())) != 0 {
		t.Fatal("no index, no titles")
	}
}

// Codex's homes are ~/.codex and $CODEX_HOME when that is another folder (an
// Orca terminal points it at Orca's own), the same folder counted once.
func TestCodexHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	dflt := filepath.Join(home, ".codex")
	other := t.TempDir()
	for env, want := range map[string][]string{
		"":    {dflt},
		other: {dflt, other},
		strings.ToUpper(dflt) + string(filepath.Separator): {dflt},
		filepath.Join(dflt, "x", ".."):                     {dflt},
	} {
		t.Setenv("CODEX_HOME", env)
		if got := ingest.CodexHomes(); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("CODEX_HOME=%q: got %v want %v", env, got, want)
		}
	}
}

func TestDiscoverCodexAndTitlesAcrossHomes(t *testing.T) {
	a, pa, lines := codexFixture(t)
	write(t, pa, strings.Join(lines, ""))
	b, pb, _ := codexFixture(t)
	write(t, pb, strings.Join(lines, ""))
	files, err := ingest.DiscoverCodex(a, b)
	if err != nil || len(files) != 2 || files[0] != pa || files[1] != pb {
		t.Fatalf("got %v %v", files, err)
	}
	write(t, filepath.Join(a, "session_index.jsonl"), `{"id":"x","thread_name":"From app"}`+"\n")
	write(t, filepath.Join(b, "session_index.jsonl"), `{"id":"y","thread_name":"From cli"}`+"\n")
	if got := ingest.CodexTitles(a, b); got["x"] != "From app" || got["y"] != "From cli" {
		t.Fatalf("titles %v", got)
	}
}
