package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/update"
)

type fakeGit struct{}

func (fakeGit) Info(cwd string) (project.GitInfo, error) {
	return project.GitInfo{RemoteURL: "git@github.com:acme/webapp.git", Root: `C:\repo`}, nil
}

type fakeGHFetcher struct{ items []github.Item }

func (f fakeGHFetcher) ReviewRequests(context.Context) ([]github.Item, error) { return f.items, nil }
func (f fakeGHFetcher) AuthoredPRs(context.Context) ([]github.Item, error)    { return nil, nil }
func (f fakeGHFetcher) OwnOpenPRs(context.Context) ([]github.Item, error)     { return nil, nil }
func (f fakeGHFetcher) ReviewsSubmitted(context.Context, time.Time) ([]github.Item, error) {
	return nil, nil
}

func TestAppArchivesPastPlans(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	if err := st.CreatePlan(store.Plan{ID: "p", WindowStart: now.Add(-27 * time.Hour), WindowEnd: now.Add(-25 * time.Hour), CreatedAt: now.Add(-27 * time.Hour), Status: "active"}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(),
			Handler: http.NewServeMux(), Listener: ln, SummarizeEvery: 10 * time.Millisecond,
		})
	}()
	waitFor(t, 2*time.Second, func() bool {
		past, _ := st.ListPlans("past")
		return len(past) == 1
	})
}

func TestAppRunWithGitHubLoop(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := fakeGHFetcher{items: []github.Item{{Kind: "review_request", ExternalID: "rr1", Title: "review me", UpdatedAt: time.Now()}}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(),
			Handler: http.NewServeMux(), Listener: ln,
			GitHub: f, GitHubEvery: 10 * time.Millisecond, SummarizeEvery: time.Second,
		})
	}()
	// the github loop should sync the fake item into the store
	waitFor(t, 2*time.Second, func() bool {
		items, _ := st.ListExternalItems("github")
		return len(items) >= 1
	})
}

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

func TestAppRunStartsAndStopsCleanly(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(), Narrator: nil,
			Handler: mux, Listener: ln, SummarizeEvery: 20 * time.Millisecond,
		})
	}()

	addr := "http://" + ln.Addr().String() + "/"
	waitFor(t, 2*time.Second, func() bool {
		resp, err := http.Get(addr)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop within 3s of cancel")
	}
}

type blockingNarrator struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingNarrator) Narrate(ctx context.Context, prompt string) (string, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return "n", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestAppRunWatcherUnblockedBySlowNarrator(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	// a transcript that derives "waiting" (recent assistant text, strictly later
	// than the prompt), so it warrants narration and the summarizer calls Narrate.
	now := time.Now().UTC()
	userTS := now.Add(-6 * time.Minute).Format(time.RFC3339)
	asstTS := now.Add(-5 * time.Minute).Format(time.RFC3339)
	body := `{"type":"user","sessionId":"sess","cwd":"C:\\repo","timestamp":"` + userTS + `","message":{"role":"user","content":"do it"}}` + "\n" +
		`{"type":"assistant","sessionId":"sess","cwd":"C:\\repo","timestamp":"` + asstTS + `","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "sess.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	bn := &blockingNarrator{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(bn.release)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: root, Narrator: bn,
			Handler: http.NewServeMux(), Listener: ln, SummarizeEvery: 10 * time.Millisecond,
		})
	}()

	// The narrator entering proves the summarizer reached the waiting session.
	select {
	case <-bn.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("narrator never entered - summarizer did not reach the waiting session")
	}
	// Narration is blocked now; the watcher must independently have ingested and
	// set a status, proving narration is not sitting on the watcher goroutine.
	waitFor(t, 2*time.Second, func() bool {
		ss, _ := st.ListSessions()
		return len(ss) == 1 && ss[0].Status != ""
	})
}

func TestAppRebuildsAITimeBeforeStarting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the workers stop at once; the rebuild still runs first
	Run(ctx, Deps{Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(), Handler: http.NewServeMux(), Listener: ln})
	if v, _, _ := st.GetSetting("ai_time_rebuilt_v6"); v != "1" {
		t.Fatalf("rebuild marker: %q", v)
	}
}

func TestAppRunsTheUpdateCheck(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls := 0
	c := update.NewChecker("v1.0.0", "example/tool", func() bool { return true })
	c.StartDelay = 0
	c.Fetch = func(context.Context, string) (update.Release, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return update.Release{Tag: "v1.1.0"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(),
			Handler: http.NewServeMux(), Listener: ln, SummarizeEvery: time.Second,
			Update: c,
		})
	}()
	waitFor(t, 2*time.Second, func() bool { return c.Status().Available })
}

func TestAppRunsWithAnUnsupportedUpdateCheck(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := update.NewChecker("dev", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Deps{
			Store: st, Git: fakeGit{}, ClaudeRoot: t.TempDir(),
			Handler: http.NewServeMux(), Listener: ln, SummarizeEvery: time.Second,
			Update: c,
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("a dev build's update check stopped the daemon: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	<-done
}

// A failing sync is logged when it starts, changes or ends, not on every pass.
func TestLogFailureLogsChangesOnly(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	f := logFailure("github")
	for _, err := range []error{errors.New("gh not installed"), errors.New("gh not installed"), errors.New("gh not installed"), nil, nil, errors.New("rate limited")} {
		f(0, err)
	}
	out := buf.String()
	if strings.Count(out, "gh not installed") != 1 || strings.Count(out, "works again") != 1 || strings.Count(out, "rate limited") != 1 {
		t.Fatalf("log:\n%s", out)
	}
}
