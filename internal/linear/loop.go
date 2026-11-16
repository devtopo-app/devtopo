package linear

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/loop"
	"github.com/devtopo-app/devtopo/internal/store"
)

// DefaultPollEvery is the Linear poll cadence - slow, like the other connectors.
const DefaultPollEvery = 10 * time.Minute

// RunLoop syncs once immediately, then once per tick, until ctx is cancelled.
// When no key is configured each pass is a cheap no-op, so the loop runs fine
// until the user connects (the key is re-read from the store each pass).
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
