package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const ownURL = "https://github.com/o/r/pull/1"

// review is one review node: who, at which hour of the day, in what state.
func review(login, kind string, hour int, state string) string {
	return `{"state":"` + state + `","submittedAt":"2026-10-28T` + time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC).Format("15") + `:00:00Z","author":{"__typename":"` + kind + `","login":"` + login + `"}}`
}

func ownPR(reviews ...string) string {
	return `{"title":"Fix X","url":"` + ownURL + `","updatedAt":"2026-10-28T08:00:00Z","repository":{"nameWithOwner":"o/r"},"reviews":{"nodes":[` + strings.Join(reviews, ",") + `]}}`
}

func ownFrom(t *testing.T, nodes string) []Item {
	t.Helper()
	c := &Client{Bin: "gh", run: fakeRun(map[string][]byte{"api": []byte(searchReply(nodes))}, nil)}
	items, err := c.OwnOpenPRs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func at(hour int) time.Time { return time.Date(2026, 10, 28, hour, 0, 0, 0, time.UTC) }

func TestOwnOpenPRsQuery(t *testing.T) {
	var args []string
	c := &Client{Bin: "gh", run: func(_ context.Context, _ string, a []string) ([]byte, error) {
		args = a
		return []byte(searchReply("")), nil
	}}
	if _, err := c.OwnOpenPRs(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "api graphql -f query=") || !strings.Contains(joined, "is:pr is:open author:@me") {
		t.Fatalf("args: %s", joined)
	}
}

func TestOwnOpenPRsMapsAPlainPR(t *testing.T) {
	it := ownFrom(t, ownPR())[0]
	if it.Kind != "own_pr" || it.ExternalID != "own:"+ownURL || it.URL != ownURL || it.Title != "Fix X" || it.Repo != "o/r" {
		t.Fatalf("fields: %+v", it)
	}
	if it.State != "" || it.Reviewer != "" || !it.ReviewedAt.IsZero() || !it.UpdatedAt.Equal(at(8)) {
		t.Fatalf("no review: %+v", it)
	}
}

func TestOwnOpenPRsLatestReviewPerReviewerCounts(t *testing.T) {
	it := ownFrom(t, ownPR(review("alex", "User", 11, "APPROVED"), review("alex", "User", 10, "CHANGES_REQUESTED")))[0]
	if it.State != "approved" || it.Reviewer != "alex" || !it.ReviewedAt.Equal(at(11)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsMostUrgentWins(t *testing.T) {
	it := ownFrom(t, ownPR(review("alex", "User", 12, "APPROVED"), review("sam", "User", 9, "CHANGES_REQUESTED")))[0]
	if it.State != "changes_requested" || it.Reviewer != "sam" || !it.ReviewedAt.Equal(at(9)) || !it.UpdatedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
	it = ownFrom(t, ownPR(review("alex", "User", 12, "APPROVED"), review("sam", "User", 9, "COMMENTED")))[0]
	if it.State != "commented" || it.Reviewer != "sam" {
		t.Fatalf("commented beats approved: %+v", it)
	}
}

func TestOwnOpenPRsLatestWinsATie(t *testing.T) {
	it := ownFrom(t, ownPR(review("alex", "User", 9, "COMMENTED"), review("sam", "User", 12, "COMMENTED")))[0]
	if it.Reviewer != "sam" || !it.ReviewedAt.Equal(at(12)) {
		t.Fatalf("got: %+v", it)
	}
}

func TestOwnOpenPRsIgnoresWhoShouldNotCount(t *testing.T) {
	for name, r := range map[string]string{
		"a bot":            review("copilot", "Bot", 10, "COMMENTED"),
		"a [bot] login":    review("renovate[bot]", "User", 10, "APPROVED"),
		"you":              review("me", "User", 10, "COMMENTED"),
		"a deleted author": `{"state":"APPROVED","submittedAt":"2026-10-28T10:00:00Z","author":null}`,
		"a pending review": `{"state":"PENDING","submittedAt":null,"author":{"__typename":"User","login":"alex"}}`,
		"a dismissal":      review("alex", "User", 10, "COMMENTED") + "," + review("alex", "User", 11, "DISMISSED"),
	} {
		items := ownFrom(t, ownPR(r))
		if len(items) != 1 || items[0].State != "" || items[0].Reviewer != "" || !items[0].UpdatedAt.Equal(at(8)) {
			t.Errorf("%s must not count: %+v", name, items)
		}
	}
}

func TestOwnOpenPRsSkipsEmptyNodes(t *testing.T) {
	if items := ownFrom(t, `{},`+ownPR()); len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
}

// A partial list would make the PRs missing from it look closed.
func TestOwnOpenPRsRejectsAPartialAnswer(t *testing.T) {
	want := errors.New("gh: exit status 1")
	c := &Client{Bin: "gh", run: func(context.Context, string, []string) ([]byte, error) {
		return []byte(searchReply(ownPR())), want
	}}
	items, err := c.OwnOpenPRs(context.Background())
	if !errors.Is(err, want) || items != nil {
		t.Fatalf("items %+v err %v", items, err)
	}
}

// A PR past the first page must not look closed, so every page is read.
func TestOwnOpenPRsReadsEveryPage(t *testing.T) {
	var afters []string
	page := func(url, pageInfo string) []byte {
		return []byte(`{"data":{"viewer":{"login":"me"},"search":{"pageInfo":` + pageInfo + `,"nodes":[{"url":"` + url + `"}]}}}`)
	}
	c := &Client{Bin: "gh", run: func(_ context.Context, _ string, a []string) ([]byte, error) {
		after := ""
		for _, arg := range a {
			if strings.HasPrefix(arg, "after=") {
				after = strings.TrimPrefix(arg, "after=")
			}
		}
		afters = append(afters, after)
		if after == "" {
			return page("one", `{"hasNextPage":true,"endCursor":"c1"}`), nil
		}
		return page("two", `{"hasNextPage":false,"endCursor":"c2"}`), nil
	}}
	items, err := c.OwnOpenPRs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].URL != "one" || items[1].URL != "two" {
		t.Fatalf("items: %+v", items)
	}
	if strings.Join(afters, ",") != ",c1" {
		t.Fatalf("afters: %q", afters)
	}
}

// A later page failing fails the whole list, as a partial one would mislead.
func TestOwnOpenPRsFailsWhenALaterPageFails(t *testing.T) {
	want := errors.New("gh: exit status 1")
	calls := 0
	c := &Client{Bin: "gh", run: func(context.Context, string, []string) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, want
		}
		return []byte(`{"data":{"viewer":{"login":"me"},"search":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[{"url":"one"}]}}}`), nil
	}}
	if items, err := c.OwnOpenPRs(context.Background()); !errors.Is(err, want) || items != nil {
		t.Fatalf("items %+v err %v", items, err)
	}
}
