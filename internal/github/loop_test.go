package github

import (
	"context"
	"testing"
	"time"
)

func TestGitHubRunLoopPassesThenStops(t *testing.T) {
	st := openStore(t)
	f := fakeFetcher{reviews: []Item{{Kind: "review_request", ExternalID: "rr1", UpdatedAt: time.Now()}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var passes int
	onPass := func(int, error) {
		passes++
		if passes >= 2 {
			cancel()
		}
	}
	if err := RunLoop(ctx, st, f, 10*time.Millisecond, onPass); err != nil {
		t.Fatalf("RunLoop returned %v", err)
	}
	if passes < 2 {
		t.Fatalf("expected >=2 passes, got %d", passes)
	}
}
