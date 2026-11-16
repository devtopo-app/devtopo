package ingest_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeGit struct{}

func (fakeGit) Info(cwd string) (project.GitInfo, error) {
	return project.GitInfo{RemoteURL: "git@github.com:acme/webapp.git", Root: `C:\repo`}, nil
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendLine(t *testing.T, p, s string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func TestIngestFileIsIncrementalAndCrossesBoundary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sx.jsonl") // base name "sx" == session id
	l1 := `{"type":"user","sessionId":"sx","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"go"}}` + "\n"
	l2 := `{"type":"assistant","sessionId":"sx","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-21T09:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"}]}}` + "\n"
	l3 := `{"type":"user","sessionId":"sx","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-21T09:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1"}]}}` + "\n"
	write(t, p, l1+l2+l3) // gaps: 0->2 (ai)=2, 2->6 (ai)=4  => 6s

	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := st.GetSession("sx")
	if !ok {
		t.Fatal("session not stored")
	}
	if got.ProjectKey != "github.com/acme/webapp" {
		t.Fatalf("project %q", got.ProjectKey)
	}
	if got.AIActiveSeconds != 6 {
		t.Fatalf("after first ingest want 6, got %v", got.AIActiveSeconds)
	}

	// Append one more assistant turn 3s after the tool_result; the 6->9 gap
	// straddles the checkpoint and must still be counted.
	l4 := `{"type":"assistant","sessionId":"sx","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-21T09:00:09Z","message":{"role":"assistant","content":[]}}` + "\n"
	appendLine(t, p, l4)
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := st.GetSession("sx")
	if got2.AIActiveSeconds != 9 {
		t.Fatalf("want 9 (6 + boundary gap 3), got %v", got2.AIActiveSeconds)
	}
}

func TestIngestFileUsesRecordSessionIDNotFilename(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "filename-not-session.jsonl")
	write(t, p, `{"type":"user","sessionId":"real-sid","cwd":"C:\\repo","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"x"}}`+"\n")
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetSession("real-sid"); !ok {
		t.Fatal("session should be keyed by the record's sessionId")
	}
	if _, ok, _ := st.GetSession("filename-not-session"); ok {
		t.Fatal("session must not be keyed by the file base name when records carry a sessionId")
	}
}

// A session keeps the folder it started in: a later batch whose records name
// another folder (the agent changed directory for a while) must not move it,
// or matching the session to its terminal by folder breaks.
func TestIngestFileKeepsTheFolderTheSessionStartedIn(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	rec := func(cwd, ts string) string {
		return `{"type":"user","sessionId":"s","cwd":"` + cwd + `","timestamp":"` + ts + `","message":{"role":"user","content":"x"}}` + "\n"
	}
	write(t, p, rec(`C:\\repo`, "2026-10-21T09:00:00Z"))
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(rec(`C:\\Users\\me\\.claude\\memory`, "2026-10-21T09:05:00Z"))
	f.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	if se, _, _ := st.GetSession("s"); se.CWD != `C:\repo` {
		t.Fatalf("the session's folder moved to %q", se.CWD)
	}
}

type recordingGit struct{ sawEmpty bool }

func (g *recordingGit) Info(cwd string) (project.GitInfo, error) {
	if cwd == "" {
		g.sawEmpty = true
	}
	return project.GitInfo{RemoteURL: "git@github.com:acme/webapp.git"}, nil
}

func TestIngestFileEmptyCwdDoesNotShellGit(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "se.jsonl")
	// An assistant record with no cwd and no existing session.
	write(t, p, `{"type":"assistant","sessionId":"se","timestamp":"2026-10-21T09:00:00Z","message":{"role":"assistant","content":[]}}`+"\n")
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g := &recordingGit{}
	if _, err := ingest.IngestFile(st, g, p); err != nil {
		t.Fatal(err)
	}
	if g.sawEmpty {
		t.Fatal("git.Info must not be called with an empty cwd")
	}
	s, ok, _ := st.GetSession("se")
	if !ok {
		t.Fatal("session should exist")
	}
	if s.ProjectKey != "se" {
		t.Fatalf("empty-cwd session should fall back to its session id, got project %q", s.ProjectKey)
	}
}

func TestIngestFileNoNewRecordsIsNoop(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sy.jsonl")
	write(t, p, `{"type":"user","sessionId":"sy","cwd":"C:\\repo","timestamp":"2026-10-21T09:00:00Z","message":{"role":"user","content":"x"}}`+"\n")
	st, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Cursor(p)
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil { // second run reads nothing new
		t.Fatal(err)
	}
	after, _ := st.Cursor(p)
	if before != after {
		t.Fatalf("cursor moved on a no-op: %d -> %d", before, after)
	}
}

func TestIngestFileRecordsTailShape(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "st.jsonl")
	// ends in a plain assistant turn -> handed back
	write(t, p,
		`{"type":"user","sessionId":"st","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"st","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:02Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetSession("st")
	if got.Tail != "ai_done" {
		t.Fatalf("tail after handed-back turn: %q", got.Tail)
	}

	// a later batch of only a tool_use carries forward to ai_working
	appendLine(t, p, `{"type":"assistant","sessionId":"st","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:05Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t9"}]}}`+"\n")
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got2, _, _ := st.GetSession("st")
	if got2.Tail != "ai_working" {
		t.Fatalf("tail after tool dispatch: %q", got2.Tail)
	}
}

func TestIngestFileRecordsTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sp.jsonl")
	write(t, p, `{"type":"user","sessionId":"sp","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"x"}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetSession("sp")
	if got.TranscriptPath != filepath.Clean(p) {
		t.Fatalf("transcript path: got %q want %q", got.TranscriptPath, filepath.Clean(p))
	}
}

func TestIngestFileLastEventIsNewestRealEvent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sm.jsonl")
	// The real last event is the assistant turn at 09:00:02. A later system
	// record and a later isMeta user record must NOT advance last_event_at (they
	// are not real events), so status does not read a finished session as fresh.
	write(t, p,
		`{"type":"user","sessionId":"sm","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"sm","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:02Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n"+
			`{"type":"system","sessionId":"sm","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:03Z"}`+"\n"+
			`{"type":"user","isMeta":true,"sessionId":"sm","cwd":"C:\\repo","timestamp":"2026-10-22T09:05:00Z","message":{"role":"user","content":"caveat"}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetSession("sm")
	want := time.Date(2026, 10, 22, 9, 0, 2, 0, time.UTC)
	if !got.LastEventAt.Equal(want) {
		t.Fatalf("last_event_at must be the newest real event %s, got %s", want, got.LastEventAt)
	}
	if got.Tail != "ai_done" {
		t.Fatalf("tail should reflect the assistant turn, got %q", got.Tail)
	}
}

func localDay(ts string) string {
	t, _ := time.Parse(time.RFC3339, ts)
	return t.In(time.Local).Format("2006-01-02")
}

func TestIngestFileAddsDayRowsAcrossBatches(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sd.jsonl")
	write(t, p,
		`{"type":"user","sessionId":"sd","cwd":"C:\\repo","timestamp":"2026-10-22T12:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"sd","cwd":"C:\\repo","timestamp":"2026-10-22T12:00:02Z","message":{"role":"assistant","content":[]}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	appendLine(t, p, `{"type":"assistant","sessionId":"sd","cwd":"C:\\repo","timestamp":"2026-10-22T12:00:05Z","message":{"role":"assistant","content":[]}}`+"\n")
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	days, err := st.SessionDays(localDay("2026-10-22T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	d := days["sd"]
	if d.AISeconds != 5 { // 2 + the 3s gap across the checkpoint
		t.Fatalf("want 5s, got %+v", d)
	}
	if !d.First.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) || !d.Last.Equal(time.Date(2026, 10, 22, 12, 0, 5, 0, time.UTC)) {
		t.Fatalf("first/last: %+v", d)
	}
}

// A store rebuilt under the previous definition still recounts once, so its
// day rows get the hour buckets they predate.
func TestRebuildRunsAgainAfterTheV3Rebuild(t *testing.T) {
	root, parent, _ := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, parent); err != nil {
		t.Fatal(err)
	}
	ps, _, _ := st.GetSession("ps")
	ps.AIActiveSeconds = 999
	if err := st.Apply(store.Delta{Project: store.Project{Key: ps.ProjectKey, Name: "k"}, Session: ps, CursorPath: parent + ".x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting("ai_time_rebuilt_v3", "1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := ingest.IngestAll(st, fakeGit{}, root, root); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.GetSession("ps"); got.AIActiveSeconds != 19 {
		t.Fatalf("a store with only the v3 marker must be recounted to 4s parent + 15s sub-agent, got %v", got.AIActiveSeconds)
	}
	days, _ := st.SessionDays(localDay("2026-10-22T12:00:00Z"))
	var hours float64
	for _, secs := range days["ps"].Hours {
		hours += secs
	}
	if hours != 19 {
		t.Fatalf("the recount must fill the hour buckets, got %v", hours)
	}
}

// A store rebuilt before decisions were counted recounts once more to find them.
func TestRebuildRunsAgainAfterTheV4Rebuild(t *testing.T) {
	root, parent, _ := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, parent); err != nil {
		t.Fatal(err)
	}
	ps, _, _ := st.GetSession("ps")
	ps.AIActiveSeconds = 999
	if err := st.Apply(store.Delta{Project: store.Project{Key: ps.ProjectKey, Name: "k"}, Session: ps, CursorPath: parent + ".x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting("ai_time_rebuilt_v4", "1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := ingest.IngestAll(st, fakeGit{}, root, root); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.GetSession("ps"); got.AIActiveSeconds != 19 {
		t.Fatalf("a store with only the v4 marker must be recounted, got %v", got.AIActiveSeconds)
	}
}

func TestRebuildAITimeRecountsOnce(t *testing.T) {
	root, parent, sub := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	// Stored under an older definition: wrong totals and day rows, facts, and
	// both cursors at the end of their files. "gone" has no transcript left.
	for _, p := range []string{parent, sub} {
		if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
			t.Fatal(err)
		}
	}
	day := localDay("2026-10-22T12:00:00Z")
	ps, _, _ := st.GetSession("ps")
	ps.AIActiveSeconds = 999
	gone := store.Session{ID: "gone", ProjectKey: "k", AIActiveSeconds: 50, TranscriptPath: filepath.Join(root, "gone.jsonl")}
	for _, se := range []store.Session{ps, gone} {
		stale := map[string]facts.Day{day: {AISeconds: se.AIActiveSeconds}}
		if err := st.Apply(store.Delta{Project: store.Project{Key: se.ProjectKey, Name: "k"}, Session: se, Days: stale, CursorPath: se.TranscriptPath + ".x"}); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, _, _, err := ingest.IngestAll(st, fakeGit{}, root, root); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		got, _, _ := st.GetSession("ps")
		if got.AIActiveSeconds != 19 {
			t.Fatalf("%s: want the recounted 4s parent + 15s sub-agent, got %v", when, got.AIActiveSeconds)
		}
		if counts, _ := st.FactCounts("ps"); counts["prompt"] != 1 || counts["assistant_turn"] != 2 {
			t.Fatalf("%s: facts must not be doubled: %v", when, counts)
		}
		days, _ := st.SessionDays(day)
		if days["ps"].AISeconds != 19 || days["gone"].AISeconds != 50 {
			t.Fatalf("%s: day rows %+v", when, days)
		}
		if g, _, _ := st.GetSession("gone"); g.AIActiveSeconds != 50 {
			t.Fatalf("%s: a session whose transcript is gone keeps its total, got %v", when, g.AIActiveSeconds)
		}
	}
	check("after the rebuild")

	// It runs once: the next start ingests incrementally and changes nothing.
	if _, _, _, _, err := ingest.IngestAll(st, fakeGit{}, root, root); err != nil {
		t.Fatal(err)
	}
	check("on the next start")
}

// subagentFixture writes a parent transcript that dispatches a sub-agent (4s of
// AI time, last event 12:00:04) and that sub-agent's transcript (15s of AI
// time, carrying the parent's sessionId), the way Claude Code lays them out.
func subagentFixture(t *testing.T) (root, parent, sub string) {
	t.Helper()
	root = t.TempDir()
	slug := filepath.Join(root, "projects", "slug")
	if err := os.MkdirAll(filepath.Join(slug, "ps", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	parent = filepath.Join(slug, "ps.jsonl")
	write(t, parent,
		`{"type":"user","sessionId":"ps","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-22T12:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"ps","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-22T12:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Agent","id":"a1"}]}}`+"\n"+
			`{"type":"user","sessionId":"ps","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-22T12:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"a1"}]}}`+"\n"+
			`{"type":"assistant","sessionId":"ps","cwd":"C:\\repo","gitBranch":"main","timestamp":"2026-10-22T12:00:04Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	sub = filepath.Join(slug, "ps", "subagents", "agent-x.jsonl")
	write(t, sub,
		`{"type":"user","isSidechain":true,"sessionId":"ps","cwd":"C:\\other","gitBranch":"side","timestamp":"2026-10-22T12:00:05Z","message":{"role":"user","content":"do the thing"}}`+"\n"+
			`{"type":"assistant","isSidechain":true,"sessionId":"ps","cwd":"C:\\other","gitBranch":"side","timestamp":"2026-10-22T12:00:10Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"b1"}]}}`+"\n"+
			`{"type":"user","isSidechain":true,"sessionId":"ps","cwd":"C:\\other","gitBranch":"side","timestamp":"2026-10-22T12:00:20Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1"}]}}`+"\n")
	return root, parent, sub
}

func TestDiscoverClaudeFindsSubagentTranscripts(t *testing.T) {
	root, parent, sub := subagentFixture(t)
	files, err := ingest.DiscoverClaude(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != parent || files[1] != sub {
		t.Fatalf("want the parent then the sub-agent transcript, got %v", files)
	}
}

func TestIngestSubagentAddsTimeToParentOnly(t *testing.T) {
	root, parent, sub := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	for _, p := range []string{parent, sub} {
		if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ := st.GetSession("ps")
	if got.AIActiveSeconds != 19 {
		t.Fatalf("want 4s parent + 15s sub-agent, got %v", got.AIActiveSeconds)
	}
	if !got.LastEventAt.Equal(time.Date(2026, 10, 22, 12, 0, 4, 0, time.UTC)) || got.Tail != "ai_done" ||
		got.Branch != "main" || got.CWD != `C:\repo` || got.TranscriptPath != parent ||
		!got.StartedAt.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("the sub-agent must not change the parent's own fields: %+v", got)
	}
	if counts, _ := st.FactCounts("ps"); counts["prompt"] != 1 || counts["assistant_turn"] != 2 {
		t.Fatalf("the sub-agent must add no facts: %v", counts)
	}
	day := localDay("2026-10-22T12:00:00Z")
	days, _ := st.SessionDays(day)
	if d := days["ps"]; d.AISeconds != 19 || !d.Last.Equal(time.Date(2026, 10, 22, 12, 0, 20, 0, time.UTC)) {
		t.Fatalf("day row: %+v", d)
	}

	// The next sub-agent batch is seeded from the sub-agent's own last event
	// (12:00:20, +3s), not the parent's (12:00:04, which would add 19s).
	appendLine(t, sub, `{"type":"assistant","isSidechain":true,"sessionId":"ps","timestamp":"2026-10-22T12:00:23Z","message":{"role":"assistant","content":[]}}`+"\n")
	// Re-ingesting both reads only what is new: nothing counts twice.
	for _, p := range []string{parent, sub, parent, sub} {
		if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
			t.Fatal(err)
		}
	}
	got, _, _ = st.GetSession("ps")
	days, _ = st.SessionDays(day)
	if got.AIActiveSeconds != 22 || days["ps"].AISeconds != 22 {
		t.Fatalf("want 22s, got session %v day %+v", got.AIActiveSeconds, days["ps"])
	}
}

func TestIngestSubagentBeforeParentIsRetried(t *testing.T) {
	root, parent, sub := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, sub); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.GetSession("ps"); ok {
		t.Fatal("a sub-agent alone must not create its parent session")
	}
	if c, _ := st.Cursor(sub); c != 0 {
		t.Fatalf("the cursor must stay put so the file is retried, got %d", c)
	}
	for _, p := range []string{parent, sub} {
		if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
			t.Fatal(err)
		}
	}
	if got, _, _ := st.GetSession("ps"); got.AIActiveSeconds != 19 {
		t.Fatalf("want 19s once the parent exists, got %v", got.AIActiveSeconds)
	}
}

func TestIngestFileCountsBackgroundTaskAcrossBatches(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sg.jsonl")
	write(t, p,
		`{"type":"user","sessionId":"sg","timestamp":"2026-10-22T12:00:00Z","message":{"role":"user","content":"build it"}}`+"\n"+
			`{"type":"assistant","sessionId":"sg","timestamp":"2026-10-22T12:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"b1","input":{"run_in_background":true}}]}}`+"\n"+
			`{"type":"user","sessionId":"sg","timestamp":"2026-10-22T12:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"b1"}]}}`+"\n"+
			`{"type":"assistant","sessionId":"sg","timestamp":"2026-10-22T12:00:03Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	appendLine(t, p,
		`{"type":"queue-operation","operation":"enqueue","sessionId":"sg","timestamp":"2026-10-22T12:10:01Z","content":"<task-notification><tool-use-id>b1</tool-use-id><status>completed</status></task-notification>"}`+"\n"+
			`{"type":"user","sessionId":"sg","timestamp":"2026-10-22T12:20:00Z","message":{"role":"user","content":"thanks"}}`+"\n")
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetSession("sg")
	if got.AIActiveSeconds != 601 { // 12:00:00 to 12:10:01: the turn, then the build
		t.Fatalf("want 601s, got %v", got.AIActiveSeconds)
	}
	if d := mustDays(t, st, "2026-10-22T12:00:00Z")["sg"]; d.AISeconds != 601 {
		t.Fatalf("day row: %+v", d)
	}
}

func mustDays(t *testing.T, st *store.Store, ts string) map[string]facts.Day {
	t.Helper()
	days, err := st.SessionDays(localDay(ts))
	if err != nil {
		t.Fatal(err)
	}
	return days
}

func TestIngestSubagentRecordsItsOwnLastEventAndTail(t *testing.T) {
	root, parent, sub := subagentFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	subagents := func() []store.Subagent {
		t.Helper()
		for _, p := range []string{parent, sub} {
			if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
				t.Fatal(err)
			}
		}
		all, err := st.ListSessions()
		if err != nil || len(all) != 1 {
			t.Fatalf("want the one parent session, got %v %v", all, err)
		}
		return all[0].Subagents
	}
	at := func(sec int) time.Time { return time.Date(2026, 10, 22, 12, 0, sec, 0, time.UTC) }

	if got := subagents(); len(got) != 1 || !got[0].LastEventAt.Equal(at(20)) || got[0].Tail != facts.TailAIWorking {
		t.Fatalf("want the sub-agent mid-tool at 12:00:20, got %+v", got)
	}
	// A batch with no real event leaves it as it was.
	appendLine(t, sub, `{"type":"system","sessionId":"ps","timestamp":"2026-10-22T12:00:22Z"}`+"\n")
	if got := subagents(); len(got) != 1 || !got[0].LastEventAt.Equal(at(20)) || got[0].Tail != facts.TailAIWorking {
		t.Fatalf("a non-event batch must not move it, got %+v", got)
	}
	appendLine(t, sub, `{"type":"assistant","isSidechain":true,"sessionId":"ps","timestamp":"2026-10-22T12:00:23Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	if got := subagents(); len(got) != 1 || !got[0].LastEventAt.Equal(at(23)) || got[0].Tail != facts.TailAIDone {
		t.Fatalf("want the sub-agent done at 12:00:23, got %+v", got)
	}
}

// continuedFixture lays out a session Claude Code continued under a new id, as
// it does when a session is sent to the background: the old transcript ("old")
// ends with a continued-in record, and the new one ("new") starts with a copy of
// the old history under its own sessionId, then goes on. The old session also
// ran a sub-agent, whose transcript stays under the old id.
func continuedFixture(t *testing.T) (root, oldPath, newPath, sub string) {
	t.Helper()
	root = t.TempDir()
	slug := filepath.Join(root, "projects", "slug")
	if err := os.MkdirAll(filepath.Join(slug, "old", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	history := func(id string) string {
		return `{"type":"user","sessionId":"` + id + `","cwd":"C:\repo","timestamp":"2026-10-22T12:00:00Z","message":{"role":"user","content":"go"}}` + "\n" +
			`{"type":"assistant","sessionId":"` + id + `","cwd":"C:\repo","timestamp":"2026-10-22T12:00:04Z","message":{"role":"assistant","content":[{"type":"text"}],"stop_reason":"end_turn"}}` + "\n"
	}
	oldPath = filepath.Join(slug, "old.jsonl")
	write(t, oldPath, history("old")+
		`{"type":"continued-in","timestamp":"2026-10-22T12:01:00Z","sessionId":"old","continuedInSessionId":"new"}`+"\n")
	newPath = filepath.Join(slug, "new.jsonl")
	write(t, newPath, history("new")+
		`{"type":"user","sessionId":"new","cwd":"C:\repo","timestamp":"2026-10-22T12:05:00Z","message":{"role":"user","content":"and now this"}}`+"\n"+
		`{"type":"assistant","sessionId":"new","cwd":"C:\repo","timestamp":"2026-10-22T12:05:06Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"b1"}],"stop_reason":"tool_use"}}`+"\n")
	sub = filepath.Join(slug, "old", "subagents", "agent-x.jsonl")
	write(t, sub,
		`{"type":"user","isSidechain":true,"sessionId":"old","timestamp":"2026-10-22T12:00:05Z","message":{"role":"user","content":"do the thing"}}`+"\n"+
			`{"type":"assistant","isSidechain":true,"sessionId":"old","timestamp":"2026-10-22T12:00:20Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n")
	return root, oldPath, newPath, sub
}

// DEV-17: a continued session is one session, the new one, in whichever order
// its transcripts are read. It keeps the old one's name and sub-agent time, and
// the copied history counts once.
func TestContinuedSessionIsOneSession(t *testing.T) {
	for _, order := range []string{"old first", "new first"} {
		t.Run(order, func(t *testing.T) {
			root, oldPath, newPath, sub := continuedFixture(t)
			st, _ := store.Open(filepath.Join(root, "db.sqlite"))
			defer st.Close()
			first, second := oldPath, newPath
			if order == "new first" {
				first, second = newPath, oldPath
			}
			// The user named the session while it ran under its old id.
			if err := st.SetSessionName("old", "Payments"); err != nil {
				t.Fatal(err)
			}
			if _, err := ingest.IngestFile(st, fakeGit{}, first); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{second, sub, oldPath, newPath, sub} {
				if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
					t.Fatal(err)
				}
			}
			all, _ := st.ListSessions()
			if len(all) != 1 || all[0].ID != "new" {
				t.Fatalf("want only the new session, got %+v", all)
			}
			// 4s copied history + 6s new turn + 15s of the old session's sub-agent.
			if all[0].AIActiveSeconds != 25 {
				t.Fatalf("want 25s, got %v", all[0].AIActiveSeconds)
			}
			if counts, _ := st.FactCounts("new"); counts["prompt"] != 2 {
				t.Fatalf("the copied prompt counts once: %v", counts)
			}
			if counts, _ := st.FactCounts("old"); len(counts) != 0 {
				t.Fatalf("the old session keeps no facts: %v", counts)
			}
			if days := mustDays(t, st, "2026-10-22T12:00:00Z"); days["new"].AISeconds != 25 || days["old"].AISeconds != 0 {
				t.Fatalf("day rows: %+v", days)
			}
			if names, _ := st.SessionNames(); names["new"] != "Payments" || names["old"] != "" {
				t.Fatalf("the name moves to the new session: %v", names)
			}
		})
	}
}

// A store that already holds both halves of a continued session is recounted
// once, which folds them together.
func TestRebuildFoldsContinuedSessions(t *testing.T) {
	root, oldPath, newPath, sub := continuedFixture(t)
	st, _ := store.Open(filepath.Join(root, "db.sqlite"))
	defer st.Close()
	// Stored before continued-in was read: the old session, with its cursor at
	// the end of its file, beside the new one.
	info, _ := os.Stat(oldPath)
	old := store.Session{ID: "old", ProjectKey: "k", AIActiveSeconds: 4, TranscriptPath: oldPath}
	if err := st.Apply(store.Delta{Project: store.Project{Key: "k", Name: "k"}, Session: old, CursorPath: oldPath, CursorOffset: info.Size()}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{newPath, sub} {
		if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetSetting("ai_time_rebuilt_v5", "1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := ingest.IngestAll(st, fakeGit{}, root, root); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListSessions()
	if len(all) != 1 || all[0].ID != "new" || all[0].AIActiveSeconds != 25 {
		t.Fatalf("want only the new session with 25s, got %+v", all)
	}
}

// A batch of only lines with no time (Claude Code writes titles and modes so)
// keeps the session's last activity, so it stays on Today.
func TestIngestFileKeepsLastActivityOverUntimedLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "su.jsonl")
	write(t, p, `{"type":"user","sessionId":"su","timestamp":"2026-10-22T12:00:00Z","message":{"role":"user","content":"go"}}`+"\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	appendLine(t, p, `{"type":"ai-title","sessionId":"su","aiTitle":"x"}`+"\n"+`{"type":"mode","sessionId":"su","mode":"default"}`+"\n")
	if _, err := ingest.IngestFile(st, fakeGit{}, p); err != nil {
		t.Fatal(err)
	}
	got, _, _ := st.GetSession("su")
	if !got.LastActivityAt.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("last activity: %v", got.LastActivityAt)
	}
}

// A line that cannot be read is stepped past once, not read again on every pass.
func TestIngestFileStepsPastUnreadableLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sb.jsonl")
	write(t, p, "{not json\n")
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	if d, err := ingest.IngestFile(st, fakeGit{}, p); err != nil || d != 1 {
		t.Fatalf("first pass: dropped %d, %v", d, err)
	}
	if d, err := ingest.IngestFile(st, fakeGit{}, p); err != nil || d != 0 {
		t.Fatalf("the line must not be read again: dropped %d, %v", d, err)
	}
}
