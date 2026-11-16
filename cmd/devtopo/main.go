// Command devtopo is the capture-core CLI. Subcommands: `app` (the one live
// command - watch, summarize and serve the UI together), `ingest` (a one-shot
// read of Claude transcripts into the local SQLite store), `watch` (the
// always-on daemon that keeps the store current and derives each session's
// status), `summarize` (metrics plus an optional claude -p narrative per
// session), and `serve` (the read-only today surface over localhost HTTP).
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/devtopo-app/devtopo/internal/app"
	"github.com/devtopo-app/devtopo/internal/azure"
	"github.com/devtopo-app/devtopo/internal/calendar"
	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/gitlab"
	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/jira"
	"github.com/devtopo-app/devtopo/internal/launch"
	"github.com/devtopo-app/devtopo/internal/linear"
	"github.com/devtopo-app/devtopo/internal/project"
	"github.com/devtopo-app/devtopo/internal/server"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/update"
	"github.com/devtopo-app/devtopo/internal/watch"
	"github.com/devtopo-app/devtopo/internal/window"
)

// version is set by the release build (-ldflags "-X main.version=v0.1.0").
var version = "dev"

// updateRepo is the GitHub repository (owner/name) whose releases the update
// check reads, set by the release build (-X main.updateRepo=owner/name). Empty,
// as in a build from source, turns the check off.
var updateRepo = ""

func main() {
	// No command (a double-click, or the Finder, which may pass -psn_...): the tray.
	if len(os.Args) < 2 || strings.HasPrefix(os.Args[1], "-psn") {
		runTray(nil)
		return
	}
	switch os.Args[1] {
	case "version", "--version", "-version", "-v":
		attachConsole()
		fmt.Println("devtopo", version)
	case "app":
		runApp(os.Args[2:])
	case "tray":
		runTray(os.Args[2:])
	case "ingest":
		runIngest(os.Args[2:])
	case "watch":
		runWatch(os.Args[2:])
	case "summarize":
		runSummarize(os.Args[2:])
	case "github":
		runGitHub(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "console-window":
		// internal: the daemon runs this to find the window of a session's
		// console, which needs a process that can give up its own console
		pid, _ := strconv.Atoi(os.Args[len(os.Args)-1])
		h, err := window.ConsoleWindow(pid)
		if err != nil {
			os.Exit(1)
		}
		fmt.Println(h)
	default:
		usage()
	}
}

func usage() {
	attachConsole()
	fmt.Fprintln(os.Stderr, "usage: devtopo [tray|app|ingest|watch|summarize|serve|version] [flags]")
	fmt.Fprintln(os.Stderr, "  (none), tray:   in the background with a tray icon [--no-open]")
	fmt.Fprintln(os.Stderr, "  app:            [--claude-dir DIR] [--db PATH] [--addr 127.0.0.1:8787] [--no-narrative] [--summarize-every DUR]")
	fmt.Fprintln(os.Stderr, "  ingest, watch:  [--claude-dir DIR] [--db PATH]")
	fmt.Fprintln(os.Stderr, "  summarize:      [--db PATH] [--no-narrative]")
	fmt.Fprintln(os.Stderr, "  github:         [--db PATH]")
	fmt.Fprintln(os.Stderr, "  serve:          [--db PATH] [--addr 127.0.0.1:8787]")
	fmt.Fprintln(os.Stderr, "  version, --version: print the version and exit")
	os.Exit(2)
}

// runGitHub does one read-only GitHub sync into the store, for testing the
// connector outside the daemon.
func runGitHub(args []string) {
	st, _, dbPath := openStore("github", args)
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := github.NewClient()
	if !client.Available(ctx) {
		fmt.Println("gh not available or not logged in; nothing synced")
		return
	}
	n, err := github.Sync(ctx, st, client)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("github: synced %d item(s) (db: %s)\n", n, dbPath)
}

// runApp is the one live command, in a terminal: it runs the watcher,
// summarizer and HTTP server together until Ctrl-C. When DevTopo already runs,
// it says where and stops.
func runApp(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url, _, run := startApp(ctx, args)
	if run == nil {
		fmt.Println("DevTopo is already running at", url)
		return
	}
	fmt.Printf("devtopo app live at %s - Ctrl-C to stop\n", url)
	if err := run(); err != nil {
		fatal(err)
	}
	fmt.Println("stopped")
}

// startApp creates the store if absent (it writes, so unlike read-only serve
// it uses the openStore create path), binds the first free port from --addr's
// on, and builds the narrator unless disabled or unavailable. It returns the
// address to open and run, which serves until ctx ends; run is nil when
// DevTopo already runs at that address. The update checker is returned for the
// tray; run starts its loop.
func startApp(ctx context.Context, args []string) (string, *update.Checker, func() error) {
	var (
		noNarrative    bool
		addr           string
		summarizeEvery time.Duration
	)
	st, claudeDir, dbPath := openStore("app", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&noNarrative, "no-narrative", false, "skip the written summaries (claude or codex); write metrics only")
		fs.StringVar(&addr, "addr", "127.0.0.1:8787", "loopback address to serve on; the next free port up to 9 above when taken")
		fs.DurationVar(&summarizeEvery, "summarize-every", summary.DefaultSummarizeEvery, "how often to re-summarize")
	})

	if err := requireLoopback(addr); err != nil {
		fatal(err)
	}
	host, p, _ := net.SplitHostPort(addr)
	port, perr := strconv.Atoi(p)
	if perr != nil {
		fatal(fmt.Errorf("bad --addr %q", addr))
	}
	key, err := accessKey(st)
	if err != nil {
		fatal(err)
	}
	ports := make([]int, len(launch.Ports))
	for i := range ports {
		ports[i] = port + i
	}
	ln, running, err := launch.Listen(host, ports)
	if err != nil || running != "" {
		st.Close()
		if err != nil {
			fatal(err)
		}
		return signedIn("http://"+running, key), nil, nil
	}

	n, sel := narrator(ctx, st, noNarrative)

	// GitHub is optional and read through gh. The loop always runs: while gh is
	// missing or logged out each pass just fails without touching stored data,
	// so installing gh or logging in later needs no restart.
	gh := github.NewClient()
	if !gh.Available(ctx) {
		fmt.Println("gh not available or not logged in; no GitHub data until it is")
	}

	updates := updateChecker(st)
	srv := server.New(st, n)
	srv.SetNarrativeOff(noNarrative)
	srv.SetUpdate(updates)
	srv.SetSummaryModels(sel)
	srv.SetAccessKey(key)

	fmt.Printf("db: %s, watching: %s\n", dbPath, claudeDir)
	return signedIn("http://"+ln.Addr().String(), key), updates, func() error {
		defer st.Close()
		return app.Run(ctx, app.Deps{
			Store:          st,
			Git:            project.GitLookup{},
			ClaudeRoot:     claudeDir,
			CodexRoots:     ingest.CodexHomes(),
			Narrator:       n,
			Handler:        srv.Handler(),
			Listener:       ln,
			SummarizeEvery: summarizeEvery,
			GitHub:         gh,
			Linear:         linear.NewClient(),
			Calendar:       calendar.NewClient(),
			Jira:           jira.NewClient(),
			GitLab:         gitlab.NewClient(),
			Azure:          azure.NewClient(),
			Update:         updates,
		})
	}
}

// updateChecker checks updateRepo for releases newer than this version while
// the update_check setting leaves it on. In a build without a repository or a
// release version it never asks.
func updateChecker(st *store.Store) *update.Checker {
	return update.NewChecker(version, updateRepo, func() bool { return update.EnabledIn(st.GetSetting) })
}

// openStore parses the shared --claude-dir/--db flags (plus any extra flags a
// subcommand registers), ensures the db directory exists, and opens the store.
// It returns the store and the two paths.
func openStore(name string, args []string, extra ...func(*flag.FlagSet)) (*store.Store, string, string) {
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	claudeDir := fs.String("claude-dir", filepath.Join(home, ".claude"), "Claude config directory")
	dbPath := fs.String("db", launch.Data().DB, "SQLite database path")
	for _, e := range extra {
		e(fs)
	}
	_ = fs.Parse(args)

	if err := privateDir(filepath.Dir(*dbPath)); err != nil {
		fatal(err)
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		fatal(err)
	}
	privateFiles(*dbPath, *dbPath+"-wal", *dbPath+"-shm")
	return st, *claudeDir, *dbPath
}

func runIngest(args []string) {
	st, claudeDir, dbPath := openStore("ingest", args)
	defer st.Close()

	projects, sessions, aiSeconds, dropped, err := ingest.IngestAll(st, project.GitLookup{}, claudeDir, ingest.CodexHomes()...)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("ingested: %d projects, %d sessions, %.2f AI-hours", projects, sessions, aiSeconds/3600)
	if dropped > 0 {
		fmt.Printf(", %d undecodable lines skipped", dropped)
	}
	fmt.Printf(" (db: %s)\n", dbPath)
}

func runWatch(args []string) {
	st, claudeDir, dbPath := openStore("watch", args)
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("watching %s (db: %s) - Ctrl-C to stop\n", claudeDir, dbPath)
	if err := watch.New(st, project.GitLookup{}, claudeDir, ingest.CodexHomes()...).Run(ctx); err != nil {
		fatal(err)
	}
	fmt.Println("stopped")
}

// narrator chooses claude or codex for each session from the settings, among
// the ones installed and logged in. Both results are nil when summaries are off.
func narrator(ctx context.Context, st *store.Store, off bool) (summary.Narrator, *summary.Selector) {
	if off {
		fmt.Println("narrative disabled (--no-narrative); writing metrics only")
		return nil, nil
	}
	sel := summary.NewSelector(st.GetSetting, summary.DefaultBackends())
	sel.Probe(ctx)
	if !summary.Ready(sel) {
		fmt.Println("neither claude nor codex is installed and logged in; writing metrics only")
	}
	return sel, sel
}

func runSummarize(args []string) {
	var noNarrative bool
	st, _, dbPath := openStore("summarize", args, func(fs *flag.FlagSet) {
		fs.BoolVar(&noNarrative, "no-narrative", false, "skip the written summaries (claude or codex); write metrics only")
	})
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	n, _ := narrator(ctx, st, noNarrative)
	sessions, narrated, err := summary.SummarizeAll(ctx, st, n, time.Now())
	if err != nil {
		fatal(err)
	}
	fmt.Printf("summarized: %d sessions, %d narrated (db: %s)\n", sessions, narrated, dbPath)
}

func runServe(args []string) {
	// serve parses its own flags (not openStore) because it must NOT create the
	// db dir or file; it only reads an existing store.
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", launch.Data().DB, "SQLite database path")
	addr := fs.String("addr", "127.0.0.1:8787", "loopback address to serve on")
	at := fs.String("at", "", "show the views as of this local time (2006-01-02T15:04), ticking on from there; for demos and screenshots")
	_ = fs.Parse(args)
	var clock func() time.Time
	if *at != "" {
		t0, err := time.ParseInLocation("2006-01-02T15:04", *at, time.Local)
		if err != nil {
			fatal(fmt.Errorf("bad --at %q: want 2006-01-02T15:04", *at))
		}
		start := time.Now()
		clock = func() time.Time { return t0.Add(time.Since(start)) }
	}

	if _, err := os.Stat(*dbPath); err != nil {
		fatal(fmt.Errorf("no database at %s; run devtopo watch or ingest first", *dbPath))
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	if err := requireLoopback(*addr); err != nil {
		fatal(err)
	}
	key, err := accessKey(st)
	if err != nil {
		fatal(err)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal(fmt.Errorf("cannot bind %s: %w", *addr, err))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	view := server.New(st, nil)
	view.SetUpdate(updateChecker(st))
	view.SetAccessKey(key)
	if clock != nil {
		view.SetClock(clock)
	}
	srv := &http.Server{Handler: view.Handler()}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	fmt.Printf("serving the today surface at %s (db: %s) - Ctrl-C to stop\n", signedIn("http://"+*addr, key), *dbPath)

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		fmt.Println("stopped")
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "devtopo:", err)
	os.Exit(1)
}
