package gitlab

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/loop"
	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
)

// Fetcher is the slice of Client that Sync needs.
type Fetcher interface {
	ReviewRequests(ctx context.Context, site, token string) ([]prs.Item, error)
	OwnOpenPRs(ctx context.Context, site, token string) ([]prs.Item, error)
	ReviewsSubmitted(ctx context.Context, site, token string, from time.Time) ([]prs.Item, error)
}

// Sync reads the stored site and token; if either is missing it is a no-op (not an
// error). Otherwise it replaces the "gitlab" items with the reviews waiting on you,
// your open MRs, and the reviews you finished in the last 24 hours. A failed fetch
// of the first two returns without replacing, so a transient failure never wipes the
// last good items; the finished reviews just keep their last values. A connected
// source records how the sync went, for Settings. Returns the count stored.
func Sync(ctx context.Context, st *store.Store, f Fetcher) (int, error) {
	n, connected, err := syncOnce(ctx, st, f)
	if connected {
		prs.RecordSync(st, "gitlab", err)
	}
	return n, err
}

func syncOnce(ctx context.Context, st *store.Store, f Fetcher) (n int, connected bool, err error) {
	site, ok, err := st.GetSetting("gitlab_site")
	if err != nil || !ok || site == "" {
		return 0, false, err
	}
	token, ok, err := st.GetSetting("gitlab_token")
	if err != nil || !ok || token == "" {
		return 0, false, err
	}
	reviews, err := f.ReviewRequests(ctx, site, token)
	if err != nil {
		return 0, true, err
	}
	own, err := f.OwnOpenPRs(ctx, site, token)
	if err != nil {
		return 0, true, err
	}
	done, doneErr := f.ReviewsSubmitted(ctx, site, token, time.Now().Add(-24*time.Hour))
	n, err = prs.Store(st, "gitlab", reviews, own, prs.SubmittedOr(st, "gitlab", done, doneErr))
	return n, true, err
}

// DefaultPollEvery is the GitLab poll cadence - slow, like the other connectors.
const DefaultPollEvery = 10 * time.Minute

// RunLoop syncs once immediately, then once per tick, until ctx is cancelled. Until
// the user connects each pass is a cheap no-op, as credentials are re-read each pass.
func RunLoop(ctx context.Context, st *store.Store, f Fetcher, every time.Duration, onPass func(int, error)) error {
	pass := func() {
		n, err := Sync(ctx, st, f)
		if onPass != nil {
			onPass(n, err)
		}
	}
	loop.Every(ctx, every, pass)
	return nil
}
