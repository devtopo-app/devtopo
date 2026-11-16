package window

import (
	"os"
	"sync"
	"testing"
)

// Each syscall.NewCallback is never freed and the runtime aborts after about
// 2000, so the finders must reuse one callback however often they run,
// including from several goroutines at once.
func TestFindManyTimes(t *testing.T) {
	pid := os.Getpid() // a test binary has no top-level window
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 700; i++ {
				if _, err := FindPID(pid); err != ErrNotFound {
					t.Errorf("FindPID(own pid) = %v, want ErrNotFound", err)
					return
				}
				if _, err := FindTitled(pid, "neutral title"); err != ErrNotFound {
					t.Errorf("FindTitled(own pid) = %v, want ErrNotFound", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
