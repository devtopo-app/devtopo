package github

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func reviewsFrom(t *testing.T, js string) []Item {
	t.Helper()
	c := &Client{Bin: "gh", run: fakeRun(map[string][]byte{"api": []byte(js)}, nil)}
	items, err := c.ReviewRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// searchReply wraps pull request nodes in the query's reply, for viewer "me".
func searchReply(nodes string) string {
	return `{"data":{"viewer":{"login":"me"},"search":{"nodes":[` + nodes + `]}}}`
}

func TestReviewRequestsReadsSizes(t *testing.T) {
	items := reviewsFrom(t, searchReply(`{"title":"Fix X","url":"https://github.com/o/r/pull/1","updatedAt":"2026-10-28T10:00:00Z","additions":500,"deletions":100,"changedFiles":2,
		"repository":{"nameWithOwner":"o/r"},
		"files":{"nodes":[{"path":"go.sum","additions":300,"deletions":50},{"path":"main.go","additions":200,"deletions":50}]},
		"timelineItems":{"nodes":[]}}`))
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	it := items[0]
	if it.Additions != 200 || it.Deletions != 50 || it.ChangedFiles != 2 {
		t.Fatalf("sizes: %+v", it)
	}
	want := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	if it.Kind != "review_request" || !it.ReviewRequestedOfMe || it.Repo != "o/r" || it.Title != "Fix X" || it.URL != "https://github.com/o/r/pull/1" || it.ExternalID != it.URL || !it.UpdatedAt.Equal(want) {
		t.Fatalf("fields: %+v", it)
	}
}

func TestReviewRequestsAsksForTheOpenOnesRequestedOfMe(t *testing.T) {
	var args []string
	c := &Client{Bin: "gh", run: func(_ context.Context, _ string, a []string) ([]byte, error) {
		args = a
		return []byte(searchReply("")), nil
	}}
	if _, err := c.ReviewRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "api graphql -f query=") || !strings.Contains(joined, "is:pr is:open review-requested:@me") {
		t.Fatalf("args: %s", joined)
	}
}

func TestAllGeneratedIsKnownToBeZero(t *testing.T) {
	it := reviewsFrom(t, searchReply(`{"url":"u","additions":900,"deletions":10,"changedFiles":1,"files":{"nodes":[{"path":"ui_dist/a.js","additions":900,"deletions":10}]}}`))[0]
	if it.Additions != 0 || it.Deletions != 0 || it.ChangedFiles != 1 {
		t.Fatalf("sizes: %+v", it)
	}
}

func TestOver100FilesUsesTotals(t *testing.T) {
	it := reviewsFrom(t, searchReply(`{"url":"u","additions":900,"deletions":100,"changedFiles":150,"files":{"nodes":[{"path":"dist/a.js","additions":100,"deletions":0},{"path":"a.go","additions":1,"deletions":1}]}}`))[0]
	if it.Additions != 800 || it.Deletions != 100 {
		t.Fatalf("sizes: %+v", it)
	}
}

func TestRequestedAtIsYourOwnLatest(t *testing.T) {
	event := func(at, who string) string {
		return `{"createdAt":"2026-10-28T` + at + `:00:00Z","requestedReviewer":` + who + `}`
	}
	me, other, team := `{"login":"me"}`, `{"login":"other"}`, `{}`
	node := func(events ...string) string {
		return `{"url":"u","timelineItems":{"nodes":[` + strings.Join(events, ",") + `]}}`
	}
	got := reviewsFrom(t, searchReply(node(event("10", me), event("12", me), event("13", other), event("14", team))))[0].RequestedAt
	if !got.Equal(time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("requested at: %v", got)
	}
	if got := reviewsFrom(t, searchReply(node(event("13", other), event("14", team))))[0].RequestedAt; !got.IsZero() {
		t.Fatalf("only others' requests must give zero: %v", got)
	}
}

func TestReviewRequestsSkipsEmptyNodes(t *testing.T) {
	items := reviewsFrom(t, searchReply(`{},{"url":"u"}`))
	if len(items) != 1 || items[0].URL != "u" {
		t.Fatalf("items: %+v", items)
	}
}

func TestIsGenerated(t *testing.T) {
	for _, p := range []string{"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "Cargo.lock", "a/b.lock", "x.min.js", "x.min.css", "x.js.map", "dist/a.js", "web/dist/a.js", "internal/server/ui_dist/a.js", "vendor/a.go", "node_modules/a.js", "DIST/A.JS"} {
		if !isGenerated(p) {
			t.Errorf("%s should count as generated", p)
		}
	}
	for _, p := range []string{"main.go", "distance.go", "src/vendored.ts", "lock.go", "README.md"} {
		if isGenerated(p) {
			t.Errorf("%s should not count as generated", p)
		}
	}
}

// gh exits 1 when GitHub answers with data and an errors array, as when one PR's
// files fail; the list it did get is still good.
func TestReviewRequestsKeepsThePartOfAnAnswerThatWorked(t *testing.T) {
	js := `{"data":{"viewer":{"login":"me"},"search":{"nodes":[null,{"url":"u","title":"T","additions":3,"deletions":1,"changedFiles":1,"files":null,"timelineItems":null}]}},"errors":[{"message":"Something went wrong"}]}`
	c := &Client{Bin: "gh", run: func(context.Context, string, []string) ([]byte, error) {
		return []byte(js), errors.New("gh: exit status 1")
	}}
	items, err := c.ReviewRequests(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "u" || items[0].Additions != 3 {
		t.Fatalf("items: %+v", items)
	}
}

func TestReviewRequestsFailsWhenAnErrorLeavesNoList(t *testing.T) {
	for name, out := range map[string]string{
		"no output":   "",
		"not JSON":    "HTTP 502",
		"no data":     `{"errors":[{"message":"Bad credentials"}]}`,
		"no nodes":    `{"data":null,"errors":[{"message":"x"}]}`,
		"empty nodes": `{"data":{"viewer":{"login":"me"},"search":{"nodes":[]}},"errors":[{"message":"x"}]}`,
	} {
		c := &Client{Bin: "gh", run: func(context.Context, string, []string) ([]byte, error) {
			return []byte(out), errors.New("gh: exit status 1")
		}}
		if items, err := c.ReviewRequests(context.Background()); err == nil {
			t.Errorf("%s: want the error, got %+v", name, items)
		}
	}
}

func TestExecRunKeepsStdoutOnAFailedExit(t *testing.T) {
	bin, args := "sh", []string{"-c", "echo partial; exit 1"}
	if _, err := exec.LookPath(bin); err != nil {
		t.Skip("no sh here")
	}
	out, err := execRun(context.Background(), bin, args)
	if err == nil || strings.TrimSpace(string(out)) != "partial" {
		t.Fatalf("out %q err %v", out, err)
	}
}
