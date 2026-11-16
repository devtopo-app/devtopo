package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func fakeRun(out map[string][]byte, errs map[string]error) func(context.Context, string, []string) ([]byte, error) {
	return func(ctx context.Context, bin string, args []string) ([]byte, error) {
		key := args[0] // api | search | auth
		if e, ok := errs[key]; ok {
			return nil, e
		}
		return out[key], nil
	}
}

func TestReviewRequestsParsesGhJSON(t *testing.T) {
	js := `{"data":{"viewer":{"login":"me"},"search":{"nodes":[{"title":"Fix X","url":"https://github.com/acme/api/pull/1","repository":{"nameWithOwner":"acme/api"},"updatedAt":"2026-10-28T09:00:00Z"},` +
		`{"title":"Y","url":"https://github.com/o/r/pull/2","repository":{"nameWithOwner":"o/r"},"updatedAt":"2026-10-28T10:00:00Z"}]}}}`
	c := &Client{Bin: "gh", run: fakeRun(map[string][]byte{"api": []byte(js)}, nil)}
	items, err := c.ReviewRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2, got %d", len(items))
	}
	it := items[0]
	if it.Kind != "review_request" || !it.ReviewRequestedOfMe {
		t.Fatalf("kind/flag: %+v", it)
	}
	if it.Repo != "acme/api" {
		t.Fatalf("repo: %q", it.Repo)
	}
	if it.ExternalID != "https://github.com/acme/api/pull/1" {
		t.Fatalf("external id should be the url: %q", it.ExternalID)
	}
	if it.UpdatedAt.IsZero() {
		t.Fatal("updatedAt not parsed")
	}
}

func TestClientAvailableAndLogin(t *testing.T) {
	c := &Client{Bin: "gh", run: fakeRun(map[string][]byte{"auth": []byte(""), "api": []byte("octocat\n")}, nil)}
	if !c.Available(context.Background()) {
		t.Fatal("exit-0 auth status -> available")
	}
	login, err := c.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "octocat" {
		t.Fatalf("login should be trimmed: %q", login)
	}

	out := &Client{Bin: "gh", run: fakeRun(nil, map[string]error{"auth": errors.New("not logged in")})}
	if out.Available(context.Background()) {
		t.Fatal("logged out -> not available")
	}
}

func TestReviewsSubmittedParsesContributions(t *testing.T) {
	var gotArgs []string
	js := `{"data":{"viewer":{"contributionsCollection":{"pullRequestReviewContributions":{"nodes":[` +
		`{"occurredAt":"2026-10-29T09:00:00Z","pullRequest":{"title":"Fix X","url":"https://github.com/o/r/pull/1","repository":{"nameWithOwner":"o/r"}}},` +
		`{"occurredAt":"2026-10-29T11:00:00Z","pullRequest":{"title":"Fix X","url":"https://github.com/o/r/pull/1","repository":{"nameWithOwner":"o/r"}}},` +
		`{"occurredAt":"2026-10-29T10:00:00Z","pullRequest":{"title":"Add Y","url":"https://github.com/o/s/pull/2","repository":{"nameWithOwner":"o/s"}}}]}}}}}`
	c := &Client{Bin: "gh", run: func(_ context.Context, _ string, args []string) ([]byte, error) {
		gotArgs = args
		return []byte(js), nil
	}}
	items, err := c.ReviewsSubmitted(context.Background(), time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if a := strings.Join(gotArgs, " "); !strings.HasPrefix(a, "api graphql") || !strings.Contains(a, "pullRequestReviewContributions") || !strings.Contains(a, "from=2026-10-28T12:00:00Z") {
		t.Fatalf("gh args: %q", a)
	}
	// One item per PR, at its latest review.
	if len(items) != 2 {
		t.Fatalf("want 2 PRs, got %+v", items)
	}
	got := map[string]Item{}
	for _, it := range items {
		got[it.URL] = it
	}
	x := got["https://github.com/o/r/pull/1"]
	if x.Kind != "review_done" || x.Title != "Fix X" || x.Repo != "o/r" || !x.UpdatedAt.Equal(time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)) || x.ExternalID == x.URL {
		t.Fatalf("review item (its id must not clash with a review request of the same PR): %+v", x)
	}
}

func TestClientStatus(t *testing.T) {
	ctx := context.Background()
	ok := &Client{Bin: "gh", run: fakeRun(map[string][]byte{"auth": []byte(""), "api": []byte("octocat\n")}, nil)}
	if state, login := ok.Status(ctx); state != StateConnected || login != "octocat" {
		t.Fatalf("logged in: %q %q", state, login)
	}
	out := &Client{Bin: "gh", run: fakeRun(nil, map[string]error{"auth": errors.New("exit status 1")})}
	if state, _ := out.Status(ctx); state != StateLoggedOut {
		t.Fatalf("logged out: %q", state)
	}
	missing := &Client{Bin: "gh", run: fakeRun(nil, map[string]error{"auth": fmt.Errorf("gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})})}
	if state, _ := missing.Status(ctx); state != StateMissing {
		t.Fatalf("missing: %q", state)
	}
	real := &Client{Bin: "gh-that-does-not-exist-devtopo", Timeout: 5 * time.Second, run: execRun}
	if state, _ := real.Status(ctx); state != StateMissing {
		t.Fatalf("a binary not on PATH should read as missing: %q", state)
	}
}
