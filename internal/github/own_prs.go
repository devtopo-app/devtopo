package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

const ownPRsQuery = `query($after: String) { viewer { login } search(query:"is:pr is:open author:@me", type:ISSUE, first:50, after:$after) { pageInfo { hasNextPage endCursor } nodes { ... on PullRequest {
  title url updatedAt repository { nameWithOwner }
  reviews(last:50) { nodes { state submittedAt author { __typename login } } }
} } } }`

// ownReview is one review from ownPRsQuery. Author is null for a deleted account.
type ownReview struct {
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
	Author      *struct {
		Type  string `json:"__typename"`
		Login string `json:"login"`
	} `json:"author"`
}

type ownNode struct {
	ghPR
	Reviews struct {
		Nodes []ownReview `json:"nodes"`
	} `json:"reviews"`
}

// OwnOpenPRs returns your open PRs with their review state, from every page.
// Unlike ReviewRequests, any gh error fails it: a partial list would make the PRs
// missing from it look closed, and closed PRs count as done.
func (c *Client) OwnOpenPRs(ctx context.Context) ([]Item, error) {
	items := []Item{}
	args := []string{"api", "graphql", "-f", "query=" + ownPRsQuery}
	for after := ""; ; {
		out, err := c.exec(ctx, append(args[:len(args):len(args)], afterArgs(after)...)...)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Data struct {
				Viewer struct {
					Login string `json:"login"`
				} `json:"viewer"`
				Search struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []ownNode `json:"nodes"`
				} `json:"search"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, fmt.Errorf("parse gh json: %w", err)
		}
		for _, n := range resp.Data.Search.Nodes {
			if n.URL != "" { // a search result that isn't a PR
				items = append(items, n.item(resp.Data.Viewer.Login))
			}
		}
		page := resp.Data.Search.PageInfo
		if !page.HasNextPage || page.EndCursor == "" {
			return items, nil
		}
		after = page.EndCursor
	}
}

// afterArgs asks gh for the page after a cursor; the first page has none.
func afterArgs(after string) []string {
	if after == "" {
		return nil
	}
	return []string{"-f", "after=" + after}
}

// item maps a node for the viewer. Each other person counts by their latest
// submitted review; the most urgent of those is the PR's state, the latest one
// breaking a tie. UpdatedAt is when anyone counted last reviewed it.
func (n ownNode) item(viewer string) Item {
	updated, _ := time.Parse(time.RFC3339, n.UpdatedAt)
	it := Item{
		Kind: "own_pr", ExternalID: store.OwnPRPrefix + n.URL, Title: n.Title, URL: n.URL,
		Repo: n.Repository.NameWithOwner, UpdatedAt: updated.UTC(),
	}
	byReviewer := map[string]prs.Review{}
	for _, r := range n.Reviews.Nodes {
		at, err := time.Parse(time.RFC3339, r.SubmittedAt)
		if err != nil || r.State == "PENDING" || !r.countsFrom(viewer) {
			continue
		}
		if prev, ok := byReviewer[r.Author.Login]; !ok || at.After(prev.At) {
			byReviewer[r.Author.Login] = prs.Review{Reviewer: r.Author.Login, State: strings.ToLower(r.State), At: at.UTC()}
		}
	}
	reviews := make([]prs.Review, 0, len(byReviewer))
	for _, r := range byReviewer {
		if r.State != "dismissed" { // the latest word was dismissed
			reviews = append(reviews, r)
		}
	}
	it.ApplyReviews(reviews)
	return it
}

// countsFrom reports whether the review is from another person, not a bot or you.
func (r ownReview) countsFrom(viewer string) bool {
	return r.Author != nil && r.Author.Type != "Bot" && !strings.HasSuffix(r.Author.Login, "[bot]") && r.Author.Login != viewer
}
