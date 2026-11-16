// Package github reads a developer's GitHub reality through their own gh CLI:
// reviews requested of them and their recent authored PRs. It is strictly
// read-only (gh api / search / auth status) and stores no token - gh holds the
// auth, exactly as summary.ClaudeCLI leans on the user's claude login.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/proc"
	"github.com/devtopo-app/devtopo/internal/prs"
)

// Item is one GitHub thing, mapped from gh's JSON.
type Item = prs.Item

// Client shells out to gh. run is injectable so tests never call real gh.
type Client struct {
	Bin     string
	Timeout time.Duration
	run     func(ctx context.Context, bin string, args []string) ([]byte, error)
}

// NewClient builds the production client: the gh binary on PATH, a per-call
// timeout, and the real subprocess runner.
func NewClient() *Client {
	return &Client{Bin: "gh", Timeout: 20 * time.Second, run: execRun}
}

func (c *Client) exec(ctx context.Context, args ...string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	return c.run(ctx, c.Bin, args)
}

// Available reports whether gh is installed and logged in (gh auth status
// exits 0 only then). A false here just means no GitHub data, never an error.
func (c *Client) Available(ctx context.Context) bool {
	_, err := c.exec(ctx, "auth", "status")
	return err == nil
}

// The states Status reports.
const (
	StateConnected = "connected"
	StateMissing   = "gh not installed"
	StateLoggedOut = "not logged in"
)

// Status reports whether gh is usable, and as whom: StateConnected with the
// login, StateMissing when there is no gh to run, else StateLoggedOut.
func (c *Client) Status(ctx context.Context) (state, login string) {
	if _, err := c.exec(ctx, "auth", "status"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return StateMissing, ""
		}
		return StateLoggedOut, ""
	}
	login, _ = c.Login(ctx)
	return StateConnected, login
}

// Login returns the authenticated login (for resolving @me semantics / display).
func (c *Client) Login(ctx context.Context) (string, error) {
	out, err := c.exec(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

const reviewRequestsQuery = `query { viewer { login } search(query:"is:pr is:open review-requested:@me", type:ISSUE, first:50) { nodes { ... on PullRequest {
  title url updatedAt additions deletions changedFiles repository { nameWithOwner }
  files(first:100) { nodes { path additions deletions } }
  timelineItems(last:20, itemTypes:[REVIEW_REQUESTED_EVENT]) { nodes { ... on ReviewRequestedEvent { createdAt requestedReviewer { ... on User { login } } } } }
} } } }`

// ReviewRequests returns the open PRs where your review is requested, with their
// size and when you were last asked.
func (c *Client) ReviewRequests(ctx context.Context) ([]Item, error) {
	out, runErr := c.exec(ctx, "api", "graphql", "-f", "query="+reviewRequestsQuery)
	var resp struct {
		Data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
			Search struct {
				Nodes []reviewNode `json:"nodes"`
			} `json:"search"`
		} `json:"data"`
	}
	parseErr := json.Unmarshal(out, &resp)
	items := make([]Item, 0, len(resp.Data.Search.Nodes))
	for _, n := range resp.Data.Search.Nodes {
		if n.URL == "" { // a search result that isn't a PR
			continue
		}
		items = append(items, n.item(resp.Data.Viewer.Login))
	}
	// gh exits non-zero when GitHub answers with data and errors together, as
	// when one PR's files fail. An answer that still lists PRs is kept, so one bad
	// PR can't freeze the list; with no list, the error stands.
	if runErr != nil && (parseErr != nil || len(items) == 0) {
		return nil, runErr
	}
	if parseErr != nil {
		return nil, fmt.Errorf("parse gh json: %w", parseErr)
	}
	return items, nil
}

// reviewNode is one PR from reviewRequestsQuery.
type reviewNode struct {
	ghPR
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changedFiles"`
	Files        struct {
		Nodes []struct {
			Path      string `json:"path"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"nodes"`
	} `json:"files"`
	TimelineItems struct {
		Nodes []struct {
			CreatedAt         string `json:"createdAt"`
			RequestedReviewer struct {
				Login string `json:"login"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

// item maps a node for the viewer: its size leaves generated files out, and
// RequestedAt is the viewer's own latest request. A team's has no login, since it
// can't be told apart from a request to others on the team.
func (n reviewNode) item(viewer string) Item {
	updated, _ := time.Parse(time.RFC3339, n.UpdatedAt)
	it := Item{
		Kind: "review_request", ExternalID: n.URL, Title: n.Title, State: n.State, URL: n.URL,
		Repo: n.Repository.NameWithOwner, ReviewRequestedOfMe: true, UpdatedAt: updated.UTC(),
		Additions: n.Additions, Deletions: n.Deletions, ChangedFiles: n.ChangedFiles,
	}
	for _, f := range n.Files.Nodes {
		if isGenerated(f.Path) {
			it.Additions -= f.Additions
			it.Deletions -= f.Deletions
		}
	}
	it.Additions, it.Deletions = max(it.Additions, 0), max(it.Deletions, 0)
	for _, e := range n.TimelineItems.Nodes {
		if at, err := time.Parse(time.RFC3339, e.CreatedAt); err == nil && e.RequestedReviewer.Login == viewer && viewer != "" && at.After(it.RequestedAt) {
			it.RequestedAt = at.UTC()
		}
	}
	return it
}

// isGenerated reports whether a PR's file doesn't count toward its size.
var isGenerated = prs.IsGenerated

// AuthoredPRs returns your recent authored PRs (contribution figure).
func (c *Client) AuthoredPRs(ctx context.Context) ([]Item, error) {
	out, err := c.exec(ctx, "search", "prs", "--author=@me",
		"--json", "number,title,state,url,repository,updatedAt", "--limit", "50")
	if err != nil {
		return nil, err
	}
	return mapPRs(out, "pr", false)
}

const reviewsSubmittedQuery = `query($from: DateTime!) { viewer { contributionsCollection(from: $from) { pullRequestReviewContributions(first: 100) { nodes { occurredAt pullRequest { title url repository { nameWithOwner } } } } } } }`

// ReviewsSubmitted returns the PRs you reviewed since from, one item per PR at
// its latest review. The id is prefixed so it never clashes with a pending
// review request for the same PR.
func (c *Client) ReviewsSubmitted(ctx context.Context, from time.Time) ([]Item, error) {
	out, err := c.exec(ctx, "api", "graphql", "-f", "query="+reviewsSubmittedQuery, "-f", "from="+from.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			Viewer struct {
				ContributionsCollection struct {
					PullRequestReviewContributions struct {
						Nodes []struct {
							OccurredAt  string `json:"occurredAt"`
							PullRequest ghPR   `json:"pullRequest"`
						} `json:"nodes"`
					} `json:"pullRequestReviewContributions"`
				} `json:"contributionsCollection"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse gh json: %w", err)
	}
	byURL := map[string]Item{}
	for _, n := range resp.Data.Viewer.ContributionsCollection.PullRequestReviewContributions.Nodes {
		t, _ := time.Parse(time.RFC3339, n.OccurredAt)
		p := n.PullRequest
		if prev, ok := byURL[p.URL]; ok && !t.After(prev.UpdatedAt) {
			continue
		}
		byURL[p.URL] = Item{Kind: "review_done", ExternalID: prs.ReviewDonePrefix + p.URL, Title: p.Title, URL: p.URL,
			Repo: p.Repository.NameWithOwner, UpdatedAt: t.UTC()}
	}
	items := make([]Item, 0, len(byURL))
	for _, it := range byURL {
		items = append(items, it)
	}
	return items, nil
}

type ghPR struct {
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	UpdatedAt  string `json:"updatedAt"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func mapPRs(b []byte, kind string, reviewRequested bool) ([]Item, error) {
	var prs []ghPR
	if err := json.Unmarshal(b, &prs); err != nil {
		return nil, fmt.Errorf("parse gh json: %w", err)
	}
	items := make([]Item, 0, len(prs))
	for _, p := range prs {
		t, _ := time.Parse(time.RFC3339, p.UpdatedAt)
		items = append(items, Item{
			Kind: kind, ExternalID: p.URL, Title: p.Title, State: p.State,
			URL: p.URL, Repo: p.Repository.NameWithOwner,
			ReviewRequestedOfMe: reviewRequested, UpdatedAt: t.UTC(),
		})
	}
	return items, nil
}

// execRun runs gh and returns stdout, even on a non-zero exit, whose error carries
// the stderr tail.
func execRun(ctx context.Context, bin string, args []string) ([]byte, error) {
	cmd := proc.KillTreeOnCancel(proc.Hidden(exec.CommandContext(ctx, bin, args...)))
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(errb.String())
		if len(tail) > 300 {
			tail = tail[len(tail)-300:]
		}
		if tail != "" {
			return out.Bytes(), fmt.Errorf("%s: %w: %s", bin, err, tail)
		}
		return out.Bytes(), fmt.Errorf("%s: %w", bin, err)
	}
	return out.Bytes(), nil
}
