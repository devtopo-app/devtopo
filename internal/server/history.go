package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/devtopo-app/devtopo/internal/prs"
	"github.com/devtopo-app/devtopo/internal/store"
	"github.com/devtopo-app/devtopo/internal/summary"
)

// HistoryMonthDay is one local day of a month with AI time or a finished
// ticket.
type HistoryMonthDay struct {
	Date    string
	AIHours float64
	Tickets int
}

// HistorySession is what one session did on a past day.
type HistorySession struct {
	ID      string
	Name    string // the user's name for it, else its agent's title; "" for neither
	Agent   string
	Project string
	Branch  string
	AIHours float64
	Prompts int
	Points  []string // never null
	// Summarizing says its points are not made yet and POST
	// /api/history/points makes them.
	Summarizing bool
}

// HistoryDay is one local day: its numbers, its AI time by hour, its sessions
// and the tickets finished on it. MeetingHours is nil when the calendar keeps
// nothing for the day, as for a day older than its sync horizon.
type HistoryDay struct {
	Date         string
	First, Last  time.Time
	AIHours      float64
	Prompts      int
	Hours        [24]float64 // AI hours ending in each local hour
	Sessions     []HistorySession
	Tickets      []store.Finish // oldest first
	Reviews      int
	OtherTasks   int
	MeetingHours *float64
}

func (s *Server) registerHistory(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/history/month", s.handleHistoryMonth)
	mux.HandleFunc("GET /api/history/day", s.handleHistoryDay)
	mux.HandleFunc("POST /api/history/points", s.handleHistoryPoints)
}

// handleHistoryMonth serves GET /api/history/month?m=YYYY-MM: the month's
// active days, earliest first.
func (s *Server) handleHistoryMonth(w http.ResponseWriter, r *http.Request) {
	loc := s.clock().Location()
	start, err := time.ParseInLocation("2006-01", r.URL.Query().Get("m"), loc)
	if err != nil {
		http.Error(w, "m must be YYYY-MM", http.StatusBadRequest)
		return
	}
	ai, err := s.store.MonthDays(start.Format("2006-01"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	finishes, err := s.store.Finishes(start, start.AddDate(0, 1, 0))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	days := map[string]*HistoryMonthDay{}
	day := func(date string) *HistoryMonthDay {
		if days[date] == nil {
			days[date] = &HistoryMonthDay{Date: date}
		}
		return days[date]
	}
	for date, secs := range ai {
		day(date).AIHours = secs / 3600
	}
	for _, f := range finishes {
		if f.Kind == "task" {
			day(f.At.In(loc).Format("2006-01-02")).Tickets++
		}
	}
	out := []HistoryMonthDay{}
	for _, d := range days {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	s.writeJSON(w, out)
}

// handleHistoryDay serves GET /api/history/day?d=YYYY-MM-DD.
func (s *Server) handleHistoryDay(w http.ResponseWriter, r *http.Request) {
	now := s.clock()
	start, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("d"), now.Location())
	if err != nil {
		http.Error(w, "d must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	d, err := s.historyDay(start, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, d)
}

// historyDay gathers the day starting at start, a local midnight, as of now.
func (s *Server) historyDay(start, now time.Time) (HistoryDay, error) {
	end := start.AddDate(0, 0, 1)
	d := HistoryDay{Date: start.Format("2006-01-02"), Sessions: []HistorySession{}, Tickets: []store.Finish{}}
	rows, err := s.store.SessionDays(d.Date)
	if err != nil {
		return d, err
	}
	sessions, err := s.store.ListSessions()
	if err != nil {
		return d, err
	}
	prompts, err := s.store.Prompts(start, end)
	if err != nil {
		return d, err
	}
	names, err := s.store.SessionNames()
	if err != nil {
		return d, err
	}
	titles := s.codexTitles()
	past := end.Before(now) || end.Equal(now)
	for _, se := range sessions {
		row, ok := rows[se.ID]
		if !ok {
			continue
		}
		hs := HistorySession{ID: se.ID, Name: names[se.ID], Agent: se.Agent, Project: se.ProjectKey, Branch: se.Branch,
			AIHours: row.AISeconds / 3600, Prompts: prompts[se.ID], Points: []string{}}
		if hs.Name == "" {
			hs.Name = titles[se.ID]
		}
		sum, had, err := s.store.GetSummary(summary.LevelDay, summary.DayKey(se.ID, start))
		if err != nil {
			return d, err
		}
		had = had && sum.SchemaVersion >= summary.PointsSchema // older points are redone
		if had {
			json.Unmarshal([]byte(sum.Structured), &hs.Points)
		}
		hs.Summarizing = !had && past && summary.Ready(s.narrator) && se.TranscriptPath != ""
		d.Sessions = append(d.Sessions, hs)

		d.AIHours += hs.AIHours
		d.Prompts += hs.Prompts
		for h, secs := range row.Hours {
			d.Hours[h] += secs / 3600
		}
		if !row.First.IsZero() && (d.First.IsZero() || row.First.Before(d.First)) {
			d.First = row.First
		}
		if row.Last.After(d.Last) {
			d.Last = row.Last
		}
	}
	sort.SliceStable(d.Sessions, func(i, j int) bool { return d.Sessions[i].AIHours > d.Sessions[j].AIHours })

	finishes, err := s.store.Finishes(start, end)
	if err != nil {
		return d, err
	}
	submitted := map[string]bool{}
	for _, f := range finishes {
		if f.Kind == "task" {
			d.Tickets = append(d.Tickets, f)
		} else {
			submitted[f.URL] = true
			d.Reviews++
		}
	}
	// Plan items marked done that day: own activities, and reviews not also
	// submitted on the code host (a planned review's ref is its PR url), as Today counts.
	plans, err := s.store.ListPlans("")
	if err != nil {
		return d, err
	}
	for _, p := range plans {
		items, err := s.store.GetPlanItems(p.ID)
		if err != nil {
			return d, err
		}
		for _, it := range items {
			if it.DoneAt == nil || it.DoneAt.Before(start) || !it.DoneAt.Before(end) {
				continue
			}
			switch {
			case it.RefID == "":
				d.OtherTasks++
			case prs.IsCodeHost(it.RefSource) && !submitted[it.RefID]:
				d.Reviews++
			}
		}
	}

	events, err := s.store.EventsInWindow(start, end)
	if err != nil {
		return d, err
	}
	if len(events) > 0 {
		h := meetingHours(events, start, end)
		d.MeetingHours = &h
	}
	return d, nil
}

// handleHistoryPoints serves POST /api/history/points?d=YYYY-MM-DD&session=ID:
// the session's points for that past day, narrated on the first ask and
// stored. Asks are made one at a time, so two at once never both narrate.
func (s *Server) handleHistoryPoints(w http.ResponseWriter, r *http.Request) {
	if !summary.Ready(s.narrator) {
		http.Error(w, "summaries are off: neither claude nor codex is available", http.StatusServiceUnavailable)
		return
	}
	now := s.clock()
	day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("d"), now.Location())
	if err != nil || day.AddDate(0, 0, 1).After(now) {
		http.Error(w, "d must be a past day, YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	se, ok, err := s.store.GetSession(r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.pointsMu.Lock()
	// The page may have moved to another day while this ask waited its turn.
	if r.Context().Err() != nil {
		s.pointsMu.Unlock()
		http.Error(w, "the ask was cancelled", http.StatusServiceUnavailable)
		return
	}
	points, err := summary.PastPoints(r.Context(), s.store, se, s.narrator, day)
	s.pointsMu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if points == nil {
		points = []string{}
	}
	s.writeJSON(w, struct{ Points []string }{points})
}
