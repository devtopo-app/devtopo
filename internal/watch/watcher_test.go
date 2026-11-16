package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeGit struct{}

func (fakeGit) Info(cwd string) (project.GitInfo, error) {
	return project.GitInfo{RemoteURL: "git@github.com:acme/webapp.git", Root: `C:\repo`}, nil
}

func TestApplyChangeThenAge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sw.jsonl")
	os.WriteFile(p, []byte(
		`{"type":"user","sessionId":"sw","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"sw","cwd":"C:\\repo","timestamp":"2026-10-22T09:00:02Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n"), 0o644)
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, dir, dir)

	now := time.Date(2026, 10, 22, 9, 0, 3, 0, time.UTC) // 1s after last event
	w.clock = func() time.Time { return now }
	if err := w.applyChange(p); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := st.GetSession("sw"); s.Status != StatusRunning {
		t.Fatalf("fresh -> running, got %q", s.Status)
	}

	now = now.Add(10 * time.Minute) // handed back, aged into the waiting band
	if err := w.sweep(); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := st.GetSession("sw"); s.Status != StatusWaiting {
		t.Fatalf("aged handed-back -> waiting, got %q", s.Status)
	}

	now = now.Add(5 * time.Hour) // quiet past WaitingWindow
	if err := w.sweep(); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := st.GetSession("sw"); s.Status != StatusIdle {
		t.Fatalf("handed-back past WaitingWindow -> idle, got %q", s.Status)
	}
}

func TestApplyChangeMissingFileErrsNoPanic(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, t.TempDir(), t.TempDir())
	if err := w.applyChange(filepath.Join(t.TempDir(), "gone.jsonl")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestSweepRunsWhileASubagentWorks(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lead", "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "lead.jsonl")
	os.WriteFile(p, []byte(
		`{"type":"user","sessionId":"lead","cwd":"C:\repo","timestamp":"2026-10-22T09:00:00Z","message":{"role":"user","content":"go"}}`+"\n"+
			`{"type":"assistant","sessionId":"lead","cwd":"C:\repo","timestamp":"2026-10-22T09:00:02Z","message":{"role":"assistant","content":[{"type":"text"}]}}`+"\n"), 0o644)
	sub := filepath.Join(dir, "lead", "subagents", "agent-x.jsonl")
	os.WriteFile(sub, []byte(
		`{"type":"assistant","sessionId":"lead","timestamp":"2026-10-22T09:05:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"b1"}]}}`+"\n"), 0o644)
	st, _ := store.Open(filepath.Join(dir, "db.sqlite"))
	defer st.Close()
	w := New(st, fakeGit{}, dir, dir)
	w.clock = func() time.Time { return time.Date(2026, 10, 22, 9, 15, 0, 0, time.UTC) }
	if err := w.ingestOne(p); err != nil {
		t.Fatal(err)
	}
	if err := w.applyChange(sub); err != nil {
		t.Fatal(err)
	}
	// Handed back: it waits on you, and the card names the sub-agent still at work.
	if s, _, _ := st.GetSession("lead"); s.Status != StatusWaiting {
		t.Fatalf("handed back while its sub-agent is mid-tool -> waiting, got %q", s.Status)
	}
}
