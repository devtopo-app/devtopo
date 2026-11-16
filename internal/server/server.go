package server

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/devtopo-app/devtopo"
	"github.com/devtopo-app/devtopo/internal/azure"
	"github.com/devtopo-app/devtopo/internal/calendar"
	"github.com/devtopo-app/devtopo/internal/github"
	"github.com/devtopo-app/devtopo/internal/gitlab"
	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/ingest"
	"github.com/devtopo-app/devtopo/internal/jira"
	"github.com/devtopo-app/devtopo/internal/launch"
	"github.com/devtopo-app/devtopo/internal/linear"
	"github.com/devtopo-app/devtopo/internal/orca"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/update"
	"github.com/devtopo-app/devtopo/internal/usage"
)

// Server renders the today surface from the local store.
type Server struct {
	store *store.Store
	clock func() time.Time
	// narrator writes the detailed summary on demand; nil turns that off.
	narrator summary.Narrator
	// summaryModels is the choice of tool and model behind narrator, for the
	// Settings page (SetSummaryModels); nil when summaries are off.
	summaryModels *summary.Selector
	// pointsMu makes a past day's points one ask at a time.
	pointsMu sync.Mutex
	tmpl     *template.Template
	// linearValidate checks a Linear key before storing it, so "connected" means
	// the key actually works. Injectable so tests do not hit the network.
	linearValidate func(ctx context.Context, key string) error
	// jiraValidate checks the Jira site, email and token, likewise.
	jiraValidate func(ctx context.Context, site, email, token string) error
	// gitlabValidate and azureValidate check a code host's address and token, likewise.
	gitlabValidate func(ctx context.Context, site, token string) error
	azureValidate  func(ctx context.Context, base, token string) error
	// calendarValidate fetches an iCal URL before storing it as a feed, likewise.
	calendarValidate func(ctx context.Context, url string) error
	// syncGitHub/syncLinear/syncCalendar/syncJira run one sync of that source now,
	// reading its credential from the store (a no-op if none is set). They back
	// the sync-on-connect and the sync-on-planner-open refresh, so the user
	// never waits for the slow background loop. Injectable so tests record calls
	// without touching the network. Each returns the count stored.
	syncGitHub func(ctx context.Context) (int, error)
	// ghStatus checks the gh CLI: a github.State* and, when connected, the
	// login. It runs gh, so its answer is cached (ghMu) until a Check again.
	ghStatus     func(ctx context.Context) (state, login string)
	ghMu         sync.Mutex
	ghState      string
	ghLogin      string
	syncLinear   func(ctx context.Context) (int, error)
	syncCalendar func(ctx context.Context) (int, error)
	syncJira     func(ctx context.Context) (int, error)
	syncGitLab   func(ctx context.Context) (int, error)
	syncAzure    func(ctx context.Context) (int, error)
	// moveLinear/moveJira move a ticket in its tracker and return the new state
	// name. Injectable so tests never write to a real tracker.
	moveLinear func(ctx context.Context, key, issueID, stateType string) (string, error)
	moveJira   func(ctx context.Context, site, email, token, key, category string) (string, error)
	// orcaList/orcaSwitch read Orca's terminals and bring one to the front.
	// Injectable so tests never touch the user's Orca.
	orcaList   func(ctx context.Context) ([]orca.Terminal, error)
	orcaSwitch func(ctx context.Context, handle string) error
	// snapshot and sessionsDir find the live sessions and the app each runs in;
	// opener brings that app to the front. Injectable so tests never read the
	// user's processes or touch their windows.
	snapshot    func() ([]host.Proc, error)
	sessionsDir string
	opener      *host.Opener
	// seen caches the last detection for liveTTL, as the UI polls the view.
	seenMu sync.Mutex
	seenAt time.Time
	seen   *detection
	// usage reads the Claude subscription limits. Injectable so tests never
	// read the user's credentials or call Anthropic.
	usage func(ctx context.Context) usage.Result
	// codexUsage and codexTitles read Codex's limits and its thread titles from
	// Codex's home. Injectable so tests never read the user's Codex data.
	codexUsage  func() []usage.Window
	codexTitles func() map[string]string
	// update is the release check the daemon runs (SetUpdate). By default it is
	// one for a dev build, which never asks GitHub, so tests stay offline.
	update *update.Checker
	// askModels lists the models a question can use and askNarrator returns the
	// narrator for one. By default they read the narrator at call time; tests
	// replace them. askMu allows one run at a time; narrativeOff is --no-narrative.
	askModels    func() []summary.Model
	askNarrator  func(id string) (summary.Narrator, bool)
	askMu        sync.Mutex
	narrativeOff atomic.Bool
	// accessKey, when set, is what a browser must hold to use the API (auth.go).
	accessKey string
}

// New builds a Server over st, with n (nil for none) writing detailed
// summaries. The template is parsed once here.
func New(st *store.Store, n summary.Narrator) *Server {
	oc := orca.NewClient()
	s := &Server{
		store:    st,
		clock:    time.Now,
		narrator: n,
		tmpl:     template.Must(template.New("today").Parse(pageTemplate)),
		linearValidate: func(ctx context.Context, key string) error {
			_, err := linear.NewClient().AssignedIssues(ctx, key)
			return err
		},
		jiraValidate: func(ctx context.Context, site, email, token string) error {
			return jira.NewClient().Myself(ctx, site, email, token)
		},
		gitlabValidate: func(ctx context.Context, site, token string) error {
			_, err := gitlab.NewClient().Me(ctx, site, token)
			return err
		},
		azureValidate: func(ctx context.Context, base, token string) error {
			_, _, err := azure.NewClient().Me(ctx, base, token)
			return err
		},
		calendarValidate: func(ctx context.Context, url string) error {
			_, err := calendar.NewClient().Fetch(ctx, url)
			return err
		},
		syncGitHub: func(ctx context.Context) (int, error) {
			return github.Sync(ctx, st, github.NewClient())
		},
		ghStatus: github.NewClient().Status,
		syncLinear: func(ctx context.Context) (int, error) {
			return linear.Sync(ctx, st, linear.NewClient())
		},
		syncCalendar: func(ctx context.Context) (int, error) {
			return calendar.Sync(ctx, st, calendar.NewClient(), time.Now().UTC())
		},
		syncJira: func(ctx context.Context) (int, error) {
			return jira.Sync(ctx, st, jira.NewClient())
		},
		syncGitLab: func(ctx context.Context) (int, error) {
			return gitlab.Sync(ctx, st, gitlab.NewClient())
		},
		syncAzure: func(ctx context.Context) (int, error) {
			return azure.Sync(ctx, st, azure.NewClient())
		},
		moveLinear: linear.NewClient().MoveIssue,
		moveJira:   jira.NewClient().Transition,
		orcaList:   oc.List,
		orcaSwitch: oc.Switch,

		snapshot:    host.Snapshot,
		sessionsDir: host.SessionsDir(),
		opener:      host.NewOpener(),
		usage:       usage.NewClient().Get,
		codexUsage:  func() []usage.Window { return usage.ReadCodex(ingest.CodexHomes()...) },
		// Read again at most every 30 seconds: the Today view asks on every poll.
		codexTitles: cached(30*time.Second, func() map[string]string { return ingest.CodexTitles(ingest.CodexHomes()...) }),
	}
	s.update = update.NewChecker("dev", "", s.updateEnabled)
	s.askModels, s.askNarrator = s.defaultAskModels, s.defaultAskNarrator
	return s
}

// SetSummaryModels gives the server the Selector the daemon narrates through, so
// the Settings page can show and change it.
func (s *Server) SetSummaryModels(sel *summary.Selector) { s.summaryModels = sel }

// SetClock makes the server read its views as of clock instead of now: for
// demos and screenshots of a set time of day (devtopo serve --at).
func (s *Server) SetClock(clock func() time.Time) { s.clock = clock }

// Handler serves GET / (the today surface) and 404s everything else.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/today", s.handleAPIToday)
	mux.HandleFunc("/api/status", s.handleAPIStatus)
	mux.HandleFunc("GET /api/usage", s.handleUsage)
	s.registerPlanner(mux)
	s.registerHistory(mux)
	s.registerUpdate(mux)
	s.registerAsk(mux)
	s.registerSummaryModels(mux)
	mux.HandleFunc("GET /api/connectors", s.handleConnectors)
	mux.HandleFunc("POST /api/connectors/linear", s.handleConnectLinear)
	mux.HandleFunc("POST /api/connectors/jira", s.handleConnectJira)
	mux.HandleFunc("POST /api/connectors/gitlab", s.handleConnectGitLab)
	mux.HandleFunc("POST /api/connectors/azure", s.handleConnectAzure)
	mux.HandleFunc("POST /api/connectors/calendar", s.handleConnectCalendar)
	mux.HandleFunc("GET /api/calendars", s.handleListCalendars)
	mux.HandleFunc("POST /api/calendars", s.handleConnectCalendar)
	mux.HandleFunc("DELETE /api/calendars/{id}", s.handleRemoveCalendar)
	mux.HandleFunc("PUT /api/calendars/{id}/label", s.handleRenameCalendar)
	mux.HandleFunc("DELETE /api/connectors/{name}", s.handleDisconnect)
	mux.HandleFunc("POST /api/connectors/github/check", s.handleGitHubCheck)
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, launch.PingReply)
	})
	mux.HandleFunc("GET /api/help", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		io.WriteString(w, devtopo.Guide)
	})
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	mux.HandleFunc("POST /api/items/{id}/done", s.handleItemDone)
	mux.HandleFunc("POST /api/tickets/done", s.handleTicketDone)
	mux.HandleFunc("PATCH /api/sessions/{id}", s.handleRenameSession)
	mux.HandleFunc("GET /api/stickers", s.handleListStickers)
	mux.HandleFunc("PUT /api/stickers/{id}", s.handlePutSticker)
	mux.HandleFunc("PUT /api/stickers/{id}/count", s.handleStickerCount)
	mux.HandleFunc("DELETE /api/stickers/{id}", s.handleDeleteSticker)
	mux.HandleFunc("POST /api/sessions/{id}/focus", s.handleFocus)
	mux.HandleFunc("POST /api/sessions/{id}/resume", s.handleResume)
	mux.HandleFunc("POST /api/sessions/{id}/detail", s.handleSessionDetail)
	mux.HandleFunc("POST /api/sessions/{id}/points", s.handleRefreshPoints)
	mux.HandleFunc("/legacy", s.handleLegacy)
	mux.Handle("/", spaHandler())
	return noFraming(sameOriginWrites(s.keyGuard(mux)))
}

// sameOriginWrites guards the daemon against other sites open in the browser.
// Every request, reads included, must name the daemon by a loopback name in
// its Host header (localhost, 127.0.0.1 or [::1], any port): a site that
// points its own name at 127.0.0.1 (DNS rebinding) sends its own name as Host,
// so it can neither read the store nor write to it. On top of that, any
// non-GET request carrying an Origin must come from this very origin, so a
// page on another site cannot POST to it either. Requests with no Origin
// (curl) pass the Origin check.
func sameOriginWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "request refused: not a loopback host", http.StatusForbidden)
			return
		}
		o := r.Header.Get("Origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && o != "" && o != "http://"+r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether a Host header names this machine's loopback:
// localhost, 127.0.0.1 or ::1, with or without a port (IPv6 in brackets).
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// handleLegacy renders the server-side HTML page, kept at /legacy as the no-JS
// fallback for both the app daemon and the detached serve command.
func (s *Server) handleLegacy(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/legacy" {
		http.NotFound(w, r)
		return
	}
	view, err := s.todayView()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Render into a buffer so a template error becomes a clean 500 rather than a
	// half-written page.
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// pageTemplate is the whole page: inline CSS (light turquoise + dark Dracula),
// the state-of-now line, the day totals, and the three lanes. All dynamic text
// is auto-escaped by html/template; session data is never wrapped in
// template.HTML.
const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="15">
<title>DevTopo - Today</title>
<style>
:root{
  --bg:#ffffff; --panel:#f2f4f4; --border:#e4e8e8; --fg:#1c2524; --dim:#5c6a68;
  --accent:#0f9488; --running:#0f9488; --waiting:#b4690e; --idle:#5c6a68;
}
@media (prefers-color-scheme:dark){:root{
  --bg:#282a36; --panel:#343746; --border:#44475a; --fg:#f8f8f2; --dim:#6272a4;
  --accent:#8be9fd; --running:#8be9fd; --waiting:#ffb86c; --idle:#6272a4;
}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif}
.wrap{max-width:1100px;margin:0 auto;padding:24px 20px 48px}
header h1{font-size:15px;letter-spacing:.08em;text-transform:uppercase;color:var(--dim);margin:0 0 4px}
.state{font-size:20px;font-weight:600;margin:0 0 14px}
.totals{color:var(--dim);margin:0 0 24px}
.totals strong{color:var(--fg)}
.lanes{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:16px}
.lane h2{font-size:12px;letter-spacing:.06em;text-transform:uppercase;margin:0 0 10px;padding-bottom:6px;border-bottom:2px solid var(--border)}
.lane.running h2{color:var(--running);border-color:var(--running)}
.lane.waiting h2{color:var(--waiting);border-color:var(--waiting)}
.lane.idle h2{color:var(--idle)}
.lane.idle{opacity:.75}
.card{background:var(--panel);border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:10px}
.card .proj{font-weight:600;word-break:break-all}
.card .meta{color:var(--dim);font-size:12px;margin-top:2px}
.card .narr{margin:8px 0 0;padding-left:18px;font-size:13px}
.empty{color:var(--dim);font-size:13px}
footer{color:var(--dim);font-size:12px;margin-top:28px}
</style>
</head>
<body>
<div class="wrap">
<header>
  <h1>DevTopo - Today</h1>
  <p class="state">{{.StateOfNow}}</p>
  <p class="totals"><strong>{{printf "%.1f" .DayAIHours}}</strong> AI-hours today across {{.DaySessions}} session(s) active today, over a <strong>{{printf "%.1f" .DaySpanHours}}</strong> hour span.</p>
</header>
<div class="lanes">
  {{template "lane" .Running}}
  {{template "lane" .Waiting}}
  {{template "lane" .Idle}}
</div>
<footer>As of {{.AsOf.Format "15:04:05 MST"}}. Refreshes every 15s. Reading your local DevTopo store; nothing leaves this machine.</footer>
</div>
</body>
</html>
{{define "lane"}}
<section class="lane {{if eq .Status "running"}}running{{else if eq .Status "waiting"}}waiting{{else}}idle{{end}}">
  <h2>{{.Title}} ({{len .Cards}})</h2>
  {{if .Cards}}{{range .Cards}}
  <div class="card">
    <div class="proj">{{.Project}}</div>
    <div class="meta">{{.Branch}} &middot; {{printf "%.1f" .AIHours}} AI-h &middot; {{printf "%.1f" .WallHours}} wall-h &middot; {{.Prompts}} prompts &middot; {{.LastActivity.Format "15:04 MST"}}</div>
    {{if .TodayPoints}}<ul class="narr">{{range .TodayPoints}}<li>{{.}}</li>{{end}}</ul>{{end}}
  </div>
  {{end}}{{else}}
  <p class="empty">Nothing here.</p>
  {{end}}
</section>
{{end}}
`

// cached returns read's answer, read again once it is older than ttl.
func cached[T any](ttl time.Duration, read func() T) func() T {
	var (
		mu   sync.Mutex
		val  T
		at   time.Time
		have bool
	)
	return func() T {
		mu.Lock()
		defer mu.Unlock()
		if !have || time.Since(at) > ttl {
			val, at, have = read(), time.Now(), true
		}
		return val
	}
}
