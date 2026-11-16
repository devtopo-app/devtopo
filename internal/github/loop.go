package github

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/loop"
	"github.com/devtopo-app/devtopo/internal/store"
)

// DefaultPollEvery is a deliberately slow default: a buddy, not a monitor, and
// GitHub rate limits are real.
const DefaultPollEvery = 10 * time.Minute

// RunLoop syncs once immediately, then once per `every` tick, until ctx is
// cancelled (returns nil). onPass (nil-safe) reports each pass.
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
