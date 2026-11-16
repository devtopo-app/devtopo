package calendar

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/loop"
	"github.com/devtopo-app/devtopo/internal/store"
)

// DefaultPollEvery is the calendar poll cadence.
const DefaultPollEvery = 15 * time.Minute

// RunLoop syncs once immediately, then once per tick, until ctx is cancelled.
// With no feed connected each pass is a no-op.
func RunLoop(ctx context.Context, st *store.Store, f Fetcher, every time.Duration, onPass func(int, error)) error {
	pass := func() {
		n, err := Sync(ctx, st, f, time.Now())
		if onPass != nil {
			onPass(n, err)
		}
	}
	loop.Every(ctx, every, pass)
	return nil
}
