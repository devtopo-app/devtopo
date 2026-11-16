package watch

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
)

// Loop cadence. Debounce coalesces a burst of writes to one file into a single
// ingest; the sweep ticker ages quiet sessions and re-discovers files that the
// file-notification watch may have missed.
const (
	DebounceWindow = 1 * time.Second
	SweepInterval  = 1 * time.Minute
)

// Watcher keeps the store current as Claude writes transcripts and derives each
// session's status. clock and ingestFile are injectable so the time-based
// status aging and the per-file resilience are testable without real clocks or
// real files; production uses time.Now and ingest.IngestFile.
type Watcher struct {
	store      *store.Store
	git        project.Lookup
	root       string
	codexRoots []string
	clock      func() time.Time
	ingestFile func(*store.Store, project.Lookup, string) (int, error)
	// sweepEvery is the catch-up/age ticker period. It is a field (not the
	// SweepInterval const) so a test can run the backup catch-up fast and not
	// depend on fsnotify event timing, which is racy on some platforms.
	sweepEvery time.Duration
}

// New builds a Watcher over st, resolving repos with git, reading transcripts
// under claudeRoot (the Claude config dir, e.g. ~/.claude) and Codex rollouts
// under each of codexRoots (e.g. ~/.codex).
func New(st *store.Store, git project.Lookup, claudeRoot string, codexRoots ...string) *Watcher {
	return &Watcher{
		store:      st,
		git:        git,
		root:       claudeRoot,
		codexRoots: codexRoots,
		clock:      time.Now,
		ingestFile: ingest.IngestFile,
		sweepEvery: SweepInterval,
	}
}

// ingestOne reads one transcript's new records into the store. The path is
// cleaned so the fsnotify spelling and the DiscoverClaude glob spelling key the
// same cursor. It does not sweep.
func (w *Watcher) ingestOne(path string) error {
	dropped, err := w.ingestFile(w.store, w.git, filepath.Clean(path))
	if dropped > 0 {
		log.Printf("watch: skipped %d undecodable line(s) in %s", dropped, path)
	}
	return err
}

// applyChange ingests a changed file and then sweeps, so the just-touched
// session gets its fresh status immediately. It returns the ingest error; on
// an ingest error there is no new data to status, so it does not sweep.
func (w *Watcher) applyChange(path string) error {
	if err := w.ingestOne(path); err != nil {
		return err
	}
	return w.sweep()
}

// sweep recomputes every session's status from the clock and the stored tail
// shape, writing back only the ones that changed. It is how a quiet session
// ages running -> waiting/idle -> done with no new transcript activity.
func (w *Watcher) sweep() error {
	sessions, err := w.store.ListSessions()
	if err != nil {
		return err
	}
	now := w.clock()
	for _, s := range sessions {
		if next := SessionStatus(now, s); next != s.Status {
			if err := w.store.SetStatus(s.ID, next); err != nil {
				return err
			}
		}
	}
	return nil
}

// catchUp ingests every discovered transcript (continuing past a per-file
// error) and then sweeps. It runs at startup and on the ticker, so a new or
// previously-missed file is picked up within SweepInterval even if the
// file-notification watch did not see it.
func (w *Watcher) catchUp() error {
	files, err := ingest.DiscoverClaude(w.root)
	if err != nil {
		return err
	}
	rollouts, err := ingest.DiscoverCodex(w.codexRoots...)
	if err != nil {
		return err
	}
	for _, f := range append(files, rollouts...) {
		if err := w.ingestOne(f); err != nil {
			log.Printf("watch: %v", err)
		}
	}
	return w.sweep()
}

// Run watches the transcript tree until ctx is cancelled. All store access
// happens on this one goroutine: file events arm per-path debounce timers whose
// only job is to send the path to the fired channel, and this loop is the sole
// caller of applyChange/catchUp/sweep.
func (w *Watcher) Run(ctx context.Context) error {
	if err := w.catchUp(); err != nil {
		log.Printf("watch: initial catch-up: %v", err)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	// A missing projects dir is not fatal; the ticker still runs.
	w.watchTree(fsw, filepath.Join(w.root, "projects"))
	for _, root := range w.codexRoots {
		w.watchTree(fsw, filepath.Join(root, "sessions"))
	}

	ticker := time.NewTicker(w.sweepEvery)
	defer ticker.Stop()

	timers := map[string]*time.Timer{}
	defer func() {
		for _, tm := range timers {
			tm.Stop()
		}
	}()
	fired := make(chan string, 64)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, fsw, ev, timers, fired)
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			log.Printf("watch: fsnotify: %v", err)
		case path := <-fired:
			delete(timers, path)
			if err := w.applyChange(path); err != nil {
				log.Printf("watch: %v", err)
			}
		case <-ticker.C:
			if err := w.catchUp(); err != nil {
				log.Printf("watch: sweep: %v", err)
			}
		}
	}
}

// handleEvent runs on the loop goroutine. A new directory gets watches on it and
// everything below it; a write/create of a transcript (re)arms that path's
// debounce timer. The timer callback only sends the path to fired - it never
// touches the store or the map.
func (w *Watcher) handleEvent(ctx context.Context, fsw *fsnotify.Watcher, ev fsnotify.Event, timers map[string]*time.Timer, fired chan string) {
	if ev.Op&fsnotify.Create != 0 {
		if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
			// A new project, session, or subagents dir. Its subdirs may exist
			// before the watch does; a file written in that gap is caught by the
			// next catch-up.
			w.watchTree(fsw, ev.Name)
			return
		}
	}
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	if !ingest.IsTranscript(w.root, ev.Name) && !w.isRollout(ev.Name) {
		return
	}
	path := ev.Name
	if tm, ok := timers[path]; ok {
		tm.Reset(DebounceWindow)
		return
	}
	timers[path] = time.AfterFunc(DebounceWindow, func() {
		select {
		case fired <- path:
		case <-ctx.Done():
		}
	})
}

// watchTree watches dir and every directory below it, which reaches the
// sub-agent transcripts in <slug>/<sessionId>/subagents.
func (w *Watcher) watchTree(fsw *fsnotify.Watcher, dir string) {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			w.addWatch(fsw, p)
		}
		return nil
	})
}

func (w *Watcher) addWatch(fsw *fsnotify.Watcher, dir string) {
	if err := fsw.Add(dir); err != nil {
		log.Printf("watch: add %s: %v", dir, err)
	}
}

// isRollout reports whether path is a rollout under one of the Codex homes.
func (w *Watcher) isRollout(path string) bool {
	for _, root := range w.codexRoots {
		if ingest.IsCodexRollout(root, path) {
			return true
		}
	}
	return false
}
