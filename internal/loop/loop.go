// Package loop runs a background pass now and then on a fixed interval: the
// shape of every sync and summary loop in DevTopo.
package loop

import (
	"context"
	"time"
)

// Every runs pass at once and then every interval, until ctx ends.
func Every(ctx context.Context, every time.Duration, pass func()) {
	pass()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass()
		}
	}
}
