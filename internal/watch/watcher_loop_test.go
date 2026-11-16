package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
)

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestCatchUpContinuesPastAFileError(t *testing.T) {
	root := t.TempDir()
	mkSlug := func(slug, file, body string) string {
		d := filepath.Join(root, "projects", slug)
		os.MkdirAll(d, 0o755)
		p := filepath.Join(d, file)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	mkSlug("a", "sa.jsonl", `{"type":"assistant","sessionId":"sa","cwd":"C:\\repo","timestamp":"`+ts+`","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	bad := mkSlug("b", "sb.jsonl", "")
	mkSlug("b", "sc.jsonl", `{"type":"assistant","sessionId":"sc","cwd":"C:\\repo","timestamp":"`+ts+`","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"}]}}`+"\n")

	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, root, root)
	real := w.ingestFile
	var seen []string
	w.ingestFile = func(s *store.Store, g project.Lookup, p string) (int, error) {
		seen = append(seen, p)
		if filepath.Clean(p) == filepath.Clean(bad) {
			return 0, fmt.Errorf("boom")
		}
		return real(s, g, p)
	}
	if err := w.catchUp(); err != nil {
		t.Fatalf("catchUp must swallow a per-file error, got %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("catchUp must attempt every discovered file, saw %d: %v", len(seen), seen)
	}
	for _, id := range []string{"sa", "sc"} {
		if _, ok, _ := st.GetSession(id); !ok {
			t.Fatalf("good sibling %q not ingested after a bad file", id)
		}
	}
}

func TestReingestingSameFileDoesNotDoubleCount(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "projects", "a")
	os.MkdirAll(d, 0o755)
	p := filepath.Join(d, "sd.jsonl")
	os.WriteFile(p, []byte(
		`{"type":"user","sessionId":"sd","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"sd","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:03Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n"), 0o644)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, root, root)
	if err := w.applyChange(p); err != nil { // fsnotify-path spelling
		t.Fatal(err)
	}
	first, _, _ := st.GetSession("sd")
	if err := w.catchUp(); err != nil { // DiscoverClaude glob spelling
		t.Fatal(err)
	}
	again, _, _ := st.GetSession("sd")
	if again.AIActiveSeconds != first.AIActiveSeconds {
		t.Fatalf("re-ingest double-counted: %v -> %v", first.AIActiveSeconds, again.AIActiveSeconds)
	}
}

func TestRunIngestsOnWriteAndStops(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "slug")
	os.MkdirAll(proj, 0o755)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, root, root)
	// Run the backup catch-up sweep fast so ingest is guaranteed within the wait
	// window regardless of fsnotify event timing (which is racy on Windows).
	w.sweepEvery = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	p := filepath.Join(proj, "live.jsonl")
	os.WriteFile(p, []byte(`{"type":"assistant","sessionId":"live","cwd":"C:\\repo","timestamp":"`+time.Now().UTC().Format(time.RFC3339)+`","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"}]}}`+"\n"), 0o644)

	// Wait for the status itself, not just the session's existence, so the check
	// never races the sweep that follows ingest.
	waitFor(t, 5*time.Second, func() bool {
		s, ok, _ := st.GetSession("live")
		return ok && s.Status == StatusRunning
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
}

func watching(fsw *fsnotify.Watcher, dir string) bool {
	for _, d := range fsw.WatchList() {
		if filepath.Clean(d) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func TestWatchesSubagentDirsIncludingNewOnes(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	old := filepath.Join(projects, "slug", "old", "subagents")
	os.MkdirAll(old, 0o755)
	w := New(nil, fakeGit{}, root, root)
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer fsw.Close()
	w.watchTree(fsw, projects)
	if !watching(fsw, old) {
		t.Fatalf("startup must watch existing subagents dirs: %v", fsw.WatchList())
	}

	// A session dir and its subagents dir appear together while running: the
	// create event names only the session dir.
	sid := filepath.Join(projects, "slug", "new")
	os.MkdirAll(filepath.Join(sid, "subagents"), 0o755)
	timers := map[string]*time.Timer{}
	w.handleEvent(context.Background(), fsw, fsnotify.Event{Name: sid, Op: fsnotify.Create}, timers, make(chan string, 1))
	if !watching(fsw, filepath.Join(sid, "subagents")) {
		t.Fatalf("a new subagents dir must be watched: %v", fsw.WatchList())
	}

	// Only transcripts arm an ingest; other .jsonl files in session dirs do not.
	w.handleEvent(context.Background(), fsw, fsnotify.Event{Name: filepath.Join(sid, "tool-results", "x.jsonl"), Op: fsnotify.Write}, timers, make(chan string, 1))
	w.handleEvent(context.Background(), fsw, fsnotify.Event{Name: filepath.Join(sid, "subagents", "agent-1.jsonl"), Op: fsnotify.Write}, timers, make(chan string, 1))
	if len(timers) != 1 || timers[filepath.Join(sid, "subagents", "agent-1.jsonl")] == nil {
		t.Fatalf("only the sub-agent transcript should arm a timer: %v", timers)
	}
	for _, tm := range timers {
		tm.Stop()
	}
}

// A Codex rollout written while running is ingested, in a day folder made after
// the start.
func TestRunIngestsCodexRollouts(t *testing.T) {
	claude, codex := t.TempDir(), t.TempDir()
	st, _ := store.Open(filepath.Join(claude, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, claude, codex)
	w.sweepEvery = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	day := filepath.Join(codex, "sessions", "2026", "10", "29")
	os.MkdirAll(day, 0o755)
	p := filepath.Join(day, "rollout-2026-10-29T09-00-00-0c0d0e0f-1111-7222-8333-944455556666.jsonl")
	if !ingest.IsCodexRollout(codex, p) {
		t.Fatal("fixture path is no rollout")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	os.WriteFile(p, []byte(`{"timestamp":"`+now+`","type":"event_msg","payload":{"type":"task_started","turn_id":"t"}}`+"\n"+
		`{"timestamp":"`+now+`","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]}}`+"\n"), 0o644)
	waitFor(t, 5*time.Second, func() bool {
		s, ok, _ := st.GetSession("0c0d0e0f-1111-7222-8333-944455556666")
		return ok && s.Agent == "codex" && s.Status == StatusRunning
	})
	cancel()
	<-done

	timers := map[string]*time.Timer{}
	fsw, _ := fsnotify.NewWatcher()
	defer fsw.Close()
	w.handleEvent(context.Background(), fsw, fsnotify.Event{Name: p, Op: fsnotify.Write}, timers, make(chan string, 1))
	if timers[p] == nil {
		t.Fatalf("a rollout write must arm an ingest: %v", timers)
	}
	timers[p].Stop()
}

// Rollouts under every Codex home are ingested and arm an ingest when written.
func TestWatcherReadsEveryCodexHome(t *testing.T) {
	claude, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	st, _ := store.Open(filepath.Join(claude, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, claude, a, b)
	day := filepath.Join(b, "sessions", "2026", "10", "29")
	os.MkdirAll(day, 0o755)
	p := filepath.Join(day, "rollout-2026-10-29T09-00-00-0c0d0e0f-1111-7222-8333-944455556666.jsonl")
	os.WriteFile(p, []byte(`{"timestamp":"2026-10-29T09:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"go"}]}}`+"\n"), 0o644)
	if err := w.catchUp(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetSession("0c0d0e0f-1111-7222-8333-944455556666"); !ok {
		t.Fatal("a rollout under the second home was not ingested")
	}
	timers := map[string]*time.Timer{}
	fsw, _ := fsnotify.NewWatcher()
	defer fsw.Close()
	w.handleEvent(context.Background(), fsw, fsnotify.Event{Name: p, Op: fsnotify.Write}, timers, make(chan string, 1))
	if timers[p] == nil {
		t.Fatalf("a write under the second home must arm an ingest: %v", timers)
	}
	timers[p].Stop()
}
