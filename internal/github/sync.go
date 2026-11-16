package github

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

// Fetcher is the slice of Client that Sync needs; an interface so tests inject a
// fake instead of calling real gh.
type Fetcher interface {
	ReviewRequests(ctx context.Context) ([]Item, error)
	AuthoredPRs(ctx context.Context) ([]Item, error)
	ReviewsSubmitted(ctx context.Context, from time.Time) ([]Item, error)
	OwnOpenPRs(ctx context.Context) ([]Item, error)
}

// Sync fetches reviews-requested, authored PRs, your open PRs' review states and
// the reviews you submitted in the last 24 hours (which covers all of today), and
// replaces the "github" external items with them. If any fetch fails it returns
// the error WITHOUT replacing, so a transient gh failure never wipes the last good
// items. Returns the count stored.
func Sync(ctx context.Context, st *store.Store, f Fetcher) (int, error) {
	reviews, err := f.ReviewRequests(ctx)
	if err != nil {
		return 0, err
	}
	authored, err := f.AuthoredPRs(ctx)
	if err != nil {
		return 0, err
	}
	done, err := f.ReviewsSubmitted(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		return 0, err
	}
	own, err := f.OwnOpenPRs(ctx)
	if err != nil {
		return 0, err
	}
	return prs.Store(st, "github", reviews, authored, done, own)
}
