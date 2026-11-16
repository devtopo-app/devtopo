package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/devtopo-app/devtopo/internal/summary"
	"github.com/devtopo-app/devtopo/internal/watch"
)

// todayView gathers the stored sessions, their day summaries, each
// session's fact counts, and their activity on the clock's local day, and
// builds the view as of the server clock. It is the
// one place the HTML page and the JSON API agree on what "today" is.
func (s *Server) todayView() (TodayView, error) {
	sessions, err := s.store.ListSessions()
	if err != nil {
		return TodayView{}, err
	}
	now := s.clock()
	// Only today's summaries: the cards show today's points (summary.DayKey).
	summaries, err := s.store.SummariesEndingIn(summary.LevelDay, "/"+now.Format("2006-01-02"))
	if err != nil {
		return TodayView{}, err
	}
	days, err := s.store.SessionDays(now.Format("2006-01-02"))
	if err != nil {
		return TodayView{}, err
	}
	external, err := s.store.ListExternalItems("")
	if err != nil {
		return TodayView{}, err
	}
	v := BuildView(sessions, summaries, days, external, now)
	names, err := s.store.SessionNames()
	if err != nil {
		return TodayView{}, err
	}
	nameCards(&v, names, s.codexTitles())
	open, err := s.store.OpenBackgroundTasks()
	if err != nil {
		return TodayView{}, err
	}
	agentCards(&v, sessions, now, open)
	if len(v.Running.Cards) > 0 {
		running := make([]string, len(v.Running.Cards))
		for i, c := range v.Running.Cards {
			running[i] = c.ID
		}
		last, err := s.store.TaskStarts(running...)
		if err != nil {
			return TodayView{}, err
		}
		for i := range v.Running.Cards {
			v.Running.Cards[i].WorkingSince = last[v.Running.Cards[i].ID]
		}
	}
	if err := s.hostCards(&v); err != nil {
		return TodayView{}, err
	}
	y, m, d := now.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	prompts, err := s.store.Prompts(dayStart, dayStart.AddDate(0, 0, 1))
	if err != nil {
		return TodayView{}, err
	}
	for _, n := range prompts {
		v.PromptsToday += n
	}
	decisions, err := s.store.Decisions(dayStart, dayStart.AddDate(0, 0, 1))
	if err != nil {
		return TodayView{}, err
	}
	// A card counts today's prompts and decisions, like its AI hours, not the
	// session's life.
	for _, l := range []Lane{v.Running, v.Waiting, v.Idle} {
		for i := range l.Cards {
			c := &l.Cards[i]
			c.Prompts, c.Decisions = prompts[c.ID], decisions[c.ID]
			c.Signals = todaySignals(c.Signals, c.Prompts, c.Decisions)
		}
	}
	if v.StartedToday, err = s.leaveOutPlannedToday(v.StartedToday, now); err != nil {
		return TodayView{}, err
	}
	if v.YourPRs, err = s.yourPRs(external); err != nil {
		return TodayView{}, err
	}
	return v, nil
}

// todaySignals is a copy of signals with the prompts and decisions set to today's
// counts, each left out at zero, and nil when nothing is left.
func todaySignals(signals map[string]int, prompts, decisions int) map[string]int {
	out := make(map[string]int, len(signals)+2)
	for k, n := range signals {
		out[k] = n
	}
	for k, n := range map[string]int{"prompt": prompts, "decision": decisions} {
		if n > 0 {
			out[k] = n
		} else {
			delete(out, k)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// leaveOutPlannedToday drops the tickets that are an item of a plan whose day
// is now's local day: they already show in that plan.
func (s *Server) leaveOutPlannedToday(started []StartedTicket, now time.Time) ([]StartedTicket, error) {
	plans, err := s.store.ListPlans("")
	if err != nil {
		return nil, err
	}
	planned := map[[2]string]bool{}
	for _, p := range plans {
		if !sameLocalDay(p.WindowStart, now) {
			continue
		}
		items, err := s.store.GetPlanItems(p.ID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			planned[[2]string{it.RefSource, it.RefID}] = true
		}
	}
	out := []StartedTicket{}
	for _, st := range started {
		if !planned[[2]string{st.Source, st.ID}] {
			out = append(out, st)
		}
	}
	return out, nil
}

// writeJSON marshals v into a buffer first, so a marshal error becomes a clean
// 500 rather than a half-written body.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	buf.WriteTo(w)
}

// handleAPIToday serves the whole today view as JSON. Keys are the Go field
// names (PascalCase); AsOf is emitted in UTC.
func (s *Server) handleAPIToday(w http.ResponseWriter, r *http.Request) {
	view, err := s.todayView()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	view.AsOf = view.AsOf.UTC()
	s.writeJSON(w, view)
}

// statusPayload is the compact camelCase status for the bottom status bar.
type statusPayload struct {
	Daemon         string    `json:"daemon"`
	ActiveSessions int       `json:"activeSessions"`
	Waiting        int       `json:"waiting"`
	DayAIHours     float64   `json:"dayAIHours"`
	AsOf           time.Time `json:"asOf"`
}

// handleAPIStatus serves the compact status counts. It is polled every few
// seconds, so it reads only the sessions and today's day rows, not the whole
// Today view: a session's lane is its status (BuildView), and the day's AI
// hours are its sessions' day rows.
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := s.clock()
	days, err := s.store.SessionDays(now.Format("2006-01-02"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := statusPayload{Daemon: "live", AsOf: now.UTC()}
	for _, se := range sessions {
		switch watch.SessionStatus(now, se) {
		case watch.StatusRunning:
			out.ActiveSessions++
		case watch.StatusWaiting:
			out.Waiting++
		}
		if d, ok := days[se.ID]; ok {
			out.DayAIHours += d.AISeconds / 3600
		}
	}
	s.writeJSON(w, out)
}

// handleSessionDetail serves POST /api/sessions/{id}/detail: the fuller summary
// of what the session did today, cached until it has new activity. It is slow
// on a cache miss (a claude call), so the UI asks for it only on a click.
func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	if !summary.Ready(s.narrator) {
		http.Error(w, "summaries are off: neither claude nor codex is available", http.StatusServiceUnavailable)
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
	sum, ok, err := summary.Detail(r.Context(), s.store, se, s.narrator, s.clock())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "no activity today", http.StatusNotFound)
		return
	}
	s.writeJSON(w, struct {
		Text string
		AsOf time.Time
	}{sum.Narrative, sum.AsOf.UTC()})
}

// handleRefreshPoints serves POST /api/sessions/{id}/points: the session's points
// for today, brought up to date now rather than at the next regeneration. It is
// slow when there is new activity (a claude call), so the UI asks only on a click.
func (s *Server) handleRefreshPoints(w http.ResponseWriter, r *http.Request) {
	if !summary.Ready(s.narrator) {
		http.Error(w, "summaries are off: neither claude nor codex is available", http.StatusServiceUnavailable)
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
	sum, ok, err := summary.RefreshPoints(r.Context(), s.store, se, s.narrator, s.clock())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "no activity today", http.StatusNotFound)
		return
	}
	var points []string
	if err := json.Unmarshal([]byte(sum.Structured), &points); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, struct {
		Points []string
		AsOf   time.Time
	}{points, sum.AsOf.UTC()})
}
