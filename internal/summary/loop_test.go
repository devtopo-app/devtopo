package summary

import (
	"context"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/facts"
)

func TestRunLoopPassesThenStops(t *testing.T) {
	st := openStore(t)
	putSession(t, st, "s1", facts.TailAIDone, "", now.Add(-time.Hour), 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var passes int
	onPass := func(sessions, narrated int, err error) {
		passes++
		if passes >= 2 {
			cancel() // stop after the immediate pass plus one tick
		}
	}

	if err := RunLoop(ctx, st, nil, 10*time.Millisecond, onPass); err != nil {
		t.Fatalf("RunLoop returned %v", err)
	}
	if passes < 2 {
		t.Fatalf("expected at least 2 passes (immediate + a tick), got %d", passes)
	}
}
