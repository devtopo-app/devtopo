// Package app is the unified DevTopo daemon: it runs the transcript watcher,
// the periodic summarizer, and the HTTP server together over one store, so the
// UI is always live and summaries keep pace. Store writes are serialized by the
// store itself (single connection + WAL + busy_timeout), so the three loops
// need no extra locking.
package app

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/devtopo-app/devtopo/internal/azure"
	"github.com/devtopo-app/devtopo/internal/calendar"
	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/gitlab"
	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/jira"
	"github.com/devtopo-app/devtopo/internal/linear"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/update"
	"github.com/devtopo-app/devtopo/internal/watch"
)

// Deps are everything the daemon needs. The listener and handler are passed in
// (not built here) so the caller owns the bind and the routes, and tests can
// use an ephemeral port and a stub handler.
type Deps struct {
	Store          *store.Store
	Git            project.Lookup
	ClaudeRoot     string
	CodexRoots     []string
	Narrator       summary.Narrator
	Handler        http.Handler
	Listener       net.Listener
	SummarizeEvery time.Duration
	// GitHub is optional: when non-nil the daemon also polls GitHub. nil (no gh,
	// or logged out) just means no GitHub data, and the daemon runs without it.
	GitHub      github.Fetcher
	GitHubEvery time.Duration
	// Linear is optional: when non-nil the daemon polls Linear. The loop no-ops
	// until a key is configured, so passing a client is always safe.
	Linear      linear.Fetcher
	LinearEvery time.Duration
	// Calendar is optional: when non-nil the daemon polls the iCal feed. The loop
	// no-ops until a URL is configured.
	Calendar      calendar.Fetcher
	CalendarEvery time.Duration
	// Jira is optional: when non-nil the daemon polls Jira. The loop no-ops until
	// credentials are configured.
	Jira      jira.Fetcher
	JiraEvery time.Duration
	// GitLab and Azure are optional: when non-nil the daemon polls them. Their
	// loops no-op until credentials are configured.
	GitLab      gitlab.Fetcher
	GitLabEvery time.Duration
	Azure       azure.Fetcher
	AzureEvery  time.Duration
	// Update is optional: the release check. It runs only in a build that can
	// check (a release version and a repository); it logs a newer release.
	Update      *update.Checker
	UpdateEvery time.Duration
}

// logFailure logs a failed sync pass. Connector errors carry no token.
func logFailure(source string) func(int, error) {
	var last string // the failure last logged, "" while passes succeed
	return func(_ int, err error) {
		switch {
		case err == nil:
			if last != "" {
				log.Printf("%s sync works again", source)
				last = ""
			}
		case errors.Is(err, context.Canceled):
		case err.Error() != last:
			// A failure is logged when it starts or changes, not on every pass:
			// a missing gh would otherwise fill the log every ten minutes.
			last = err.Error()
			log.Printf("%s sync failed: %v", source, err)
		}
	}
}

// Run starts the watcher, the summarizer loop, and the HTTP server as three
// goroutines and blocks until ctx is cancelled or one of them returns. It then
// cancels the rest, shuts the server down with a short grace period, waits for
// all three, and returns the first non-shutdown error (nil on a clean stop).
func Run(ctx context.Context, d Deps) error {
	// Before any worker: the rebuild must not run alongside the watcher's ingest.
	if err := ingest.RebuildAITime(d.Store); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	every := d.SummarizeEvery
	if every <= 0 {
		every = summary.DefaultSummarizeEvery
	}

	// BaseContext hands requests this ctx (cancelled when the daemon stops), so
	// a running question ends with the app instead of outliving it.
	srv := &http.Server{Handler: d.Handler, BaseContext: func(net.Listener) context.Context { return ctx }}

	workers := []func() error{
		func() error { return watch.New(d.Store, d.Git, d.ClaudeRoot, d.CodexRoots...).Run(ctx) },
		func() error {
			// Archive past plans on each summarizer pass (cheap, no new goroutine):
			// a plan quietly moves to history when its window passes.
			summaries := logFailure("summary")
			return summary.RunLoop(ctx, d.Store, d.Narrator, every, func(_, _ int, err error) {
				summaries(0, err)
				if _, err := d.Store.ArchivePastPlans(time.Now()); err != nil {
					log.Printf("archive past plans: %v", err)
				}
			})
		},
		func() error {
			err := srv.Serve(d.Listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			return err
		},
	}
	if d.GitHub != nil {
		ghEvery := d.GitHubEvery
		if ghEvery <= 0 {
			ghEvery = github.DefaultPollEvery
		}
		workers = append(workers, func() error { return github.RunLoop(ctx, d.Store, d.GitHub, ghEvery, logFailure("github")) })
	}
	if d.Linear != nil {
		linEvery := d.LinearEvery
		if linEvery <= 0 {
			linEvery = linear.DefaultPollEvery
		}
		workers = append(workers, func() error { return linear.RunLoop(ctx, d.Store, d.Linear, linEvery, logFailure("linear")) })
	}
	if d.Jira != nil {
		jiraEvery := d.JiraEvery
		if jiraEvery <= 0 {
			jiraEvery = jira.DefaultPollEvery
		}
		workers = append(workers, func() error { return jira.RunLoop(ctx, d.Store, d.Jira, jiraEvery, logFailure("jira")) })
	}
	if d.GitLab != nil {
		every := d.GitLabEvery
		if every <= 0 {
			every = gitlab.DefaultPollEvery
		}
		workers = append(workers, func() error { return gitlab.RunLoop(ctx, d.Store, d.GitLab, every, logFailure("gitlab")) })
	}
	if d.Azure != nil {
		every := d.AzureEvery
		if every <= 0 {
			every = azure.DefaultPollEvery
		}
		workers = append(workers, func() error { return azure.RunLoop(ctx, d.Store, d.Azure, every, logFailure("azure")) })
	}
	if d.Calendar != nil {
		calEvery := d.CalendarEvery
		if calEvery <= 0 {
			calEvery = calendar.DefaultPollEvery
		}
		workers = append(workers, func() error { return calendar.RunLoop(ctx, d.Store, d.Calendar, calEvery, logFailure("calendar")) })
	}

	if d.Update.Supported() {
		workers = append(workers, func() error { return d.Update.RunLoop(ctx, d.UpdateEvery) })
	}

	done := make(chan error, len(workers))
	for _, w := range workers {
		w := w
		go func() { done <- w() }()
	}

	// Wait for the first worker to finish (a caller cancel makes the loops return
	// nil; a real failure returns an error). Then stop the rest and shut the
	// server down so Serve unblocks, and drain every worker.
	first := <-done
	cancel()
	shutCtx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	scancel()

	errs := []error{first}
	for i := 1; i < len(workers); i++ {
		errs = append(errs, <-done)
	}
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
