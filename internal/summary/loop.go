package summary

import (
	"context"
	"time"

	"github.com/devtopo-app/devtopo/internal/loop"
	"github.com/devtopo-app/devtopo/internal/store"
)

// DefaultSummarizeEvery is how often the live daemon re-summarizes by default.
const DefaultSummarizeEvery = time.Minute

// RunLoop summarizes every session once immediately, then once per `every`
// tick, until ctx is cancelled (then it returns nil). onPass, when non-nil,
// reports each pass. The slow claude calls happen on this goroutine, so they
// never sit on the watcher loop.
func RunLoop(ctx context.Context, st *store.Store, n Narrator, every time.Duration, onPass func(sessions, narrated int, err error)) error {
	pass := func() {
		sessions, narrated, err := SummarizeAll(ctx, st, n, time.Now())
		if onPass != nil {
			onPass(sessions, narrated, err)
		}
	}
	loop.Every(ctx, every, pass)
	return nil
}
