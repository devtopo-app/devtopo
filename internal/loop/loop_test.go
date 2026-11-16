package loop

import (
	"context"
	"testing"
	"time"
)

// It passes at once, again on each tick, and stops when ctx ends.
func TestEvery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	passes := 0
	done := make(chan struct{})
	go func() {
		Every(ctx, 5*time.Millisecond, func() {
			passes++
			if passes == 3 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Every did not stop when ctx ended")
	}
	if passes != 3 {
		t.Fatalf("passes: %d", passes)
	}
}
