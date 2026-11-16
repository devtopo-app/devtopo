package server

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/host"
	"github.com/devtopo-app/devtopo/internal/orca"
)

// liveTTL is how long a detection serves the polling view: one process
// snapshot per pass, not per request.
const liveTTL = 10 * time.Second

// detection is what one pass found: the live sessions, the processes, and the
// sessions the Claude desktop app owns (CLI id to the app's id).
type detection struct {
	live    map[string]host.Live
	procs   []host.Proc
	desktop map[string]string
}

// detect finds the live sessions, reusing the last pass for liveTTL unless
// fresh. A failed snapshot reads as nothing live.
func (s *Server) detect(fresh bool) *detection {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if !fresh && s.seen != nil && time.Since(s.seenAt) < liveTTL {
		return s.seen
	}
	procs, err := s.snapshot()
	if err != nil {
		procs = nil
	}
	s.seen = &detection{live: host.Detect(s.sessionsDir, procs), procs: procs, desktop: s.opener.DesktopSessions()}
	s.seenAt = time.Now()
	return s.seen
}

// hostOf is the app a session runs in, or last ran in: the live process's,
// else the stored one, else the desktop app when its files list the session.
func (d *detection) hostOf(id string, stored map[string]string) (h string, live bool) {
	if l, ok := d.live[id]; ok {
		return l.Host, true
	}
	if h = stored[id]; h == "" && d.desktop[id] != "" {
		h = host.ClaudeDesktop
	}
	return h, false
}

// appOwned reports whether the app keeps the session open between turns,
// when no claude process runs for it.
func appOwned(h string) bool {
	return h == host.VSCode || h == host.Cursor || h == host.ClaudeDesktop || h == host.CodexApp
}

// hostCards puts on each card the app its session runs in, remembering it so a
// closed session still knows where it ran, and whether it is live.
func (s *Server) hostCards(v *TodayView) error {
	d := s.detect(false)
	stored, err := s.store.SessionHosts()
	if err != nil {
		return err
	}
	for _, lane := range []*Lane{&v.Running, &v.Waiting, &v.Idle} {
		for i := range lane.Cards {
			c := &lane.Cards[i]
			c.Host, c.Live = d.hostOf(c.ID, stored)
			c.CanFocus = s.opener.CanFocus()
			if c.Host != "" && c.Host != stored[c.ID] {
				if err := s.store.SetSessionHost(c.ID, c.Host); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// handleFocus brings the app running a session to the front, opening the
// session in it where the app can. A session an app keeps open is opened there
// even with no claude process running. Errors are short; no command output is
// passed on. Where no app can be brought to the front (Linux) it is 501.
func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	if !s.opener.CanFocus() {
		http.Error(w, "focusing a window is not supported on this system", http.StatusNotImplemented)
		return
	}
	se, ok, err := s.store.GetSession(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	stored, err := s.store.SessionHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d := s.detect(true)
	h, live := d.hostOf(se.ID, stored)
	if !live && !appOwned(h) {
		http.Error(w, "not running", http.StatusConflict)
		return
	}
	l := d.live[se.ID]
	ctx := r.Context()
	switch h {
	case host.Orca:
		s.focusOrca(w, r, se.ID, se.CWD, se.Branch, se.Agent, se.LastActivityAt)
		return
	case host.VSCode, host.Cursor:
		err = s.opener.Editor(ctx, h, se.CWD, se.ID, host.EditorPID(d.procs, h))
	case host.WindowsTerminal, host.Console, host.Terminal, host.ITerm:
		err = s.opener.Terminal(ctx, h, l.PID)
	case host.ClaudeDesktop:
		pid := l.HostPID
		if !live {
			pid = host.DesktopPID(d.procs)
		}
		err = s.opener.Desktop(ctx, se.ID, pid)
	case host.CodexApp:
		err = s.opener.Codex(ctx, se.ID, host.CodexPID(d.procs))
	default:
		http.Error(w, "running in an unknown app", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// focusOrca switches Orca to the session's terminal. A failing list (no CLI,
// or Orca not running) is 503. The app does not show a failed open, so each
// failure is logged, with the folders Orca listed when none matched.
func (s *Server) focusOrca(w http.ResponseWriter, r *http.Request, id, cwd, branch, agent string, lastActivity time.Time) {
	ts, err := s.orcaList(r.Context())
	if err != nil {
		log.Printf("open in Orca failed for session %s: %v", id, err)
		http.Error(w, "Orca not available", http.StatusServiceUnavailable)
		return
	}
	t, miss := orca.Match(ts, cwd, branch, agent, lastActivity)
	if miss != "" {
		folders := make([]string, len(ts))
		for i, c := range ts {
			folders[i] = c.WorktreePath
		}
		log.Printf("open in Orca failed for session %s: %s (Orca lists: %s)", id, miss, strings.Join(folders, ", "))
		http.Error(w, "not open in Orca: "+miss, http.StatusNotFound)
		return
	}
	if err := s.orcaSwitch(r.Context(), t.Handle); err != nil {
		log.Printf("open in Orca failed for session %s: %v", id, err)
		http.Error(w, "Orca did not switch", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleResume reopens a closed session in its folder. A live session is not
// resumed a second time.
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	se, ok, err := s.store.GetSession(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.detect(true).live[se.ID]; ok {
		http.Error(w, "already running", http.StatusConflict)
		return
	}
	if se.CWD == "" {
		http.Error(w, "no folder to resume in", http.StatusConflict)
		return
	}
	resume := s.opener.Resume
	if se.Agent == "codex" {
		resume = s.opener.ResumeCodex
	}
	if err := resume(r.Context(), se.CWD, se.ID); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}
