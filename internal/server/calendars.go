package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// calendarFeedView is one connected calendar for the Settings page. It never
// carries the feed URL (a secret): only the label, the URL's host for telling
// feeds apart, and the last sync's outcome.
type calendarFeedView struct {
	ID         string
	Label      string     // the name the user gave it; '' for none
	Name       string     // the calendar's own name from its feed; '' until synced
	Host       string     // e.g. "calendar.google.com/…"; '' when unreadable
	Status     string     // "ok", "error", or "pending" before the first sync
	Error      string     `json:",omitempty"` // the last sync's failure
	LastSyncAt *time.Time `json:",omitempty"`
}

// maskFeedURL returns only the host of a feed URL, followed by an ellipsis, so
// the secret path and query never leave the server.
func maskFeedURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname() + "/…"
}

func feedView(f store.CalendarFeed) calendarFeedView {
	v := calendarFeedView{ID: f.ID, Label: f.Label, Name: f.Name, Host: maskFeedURL(f.URL), Status: "pending"}
	if !f.LastSyncAt.IsZero() {
		at := f.LastSyncAt
		v.LastSyncAt = &at
		v.Status = "ok"
	}
	if f.LastError != "" {
		v.Status, v.Error = "error", f.LastError
	}
	return v
}

// calendarStatus reports connected when at least one feed is stored; it never
// reads a URL out.
func (s *Server) calendarStatus() string {
	if feeds, err := s.store.CalendarFeeds(); err == nil && len(feeds) > 0 {
		return "connected"
	}
	return "not configured"
}

// handleListCalendars serves GET /api/calendars: the connected feeds, without
// their URLs.
func (s *Server) handleListCalendars(w http.ResponseWriter, r *http.Request) {
	feeds, err := s.store.CalendarFeeds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]calendarFeedView, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, feedView(f))
	}
	s.writeJSON(w, out)
}

// handleConnectCalendar serves POST /api/calendars (and the older POST
// /api/connectors/calendar): it adds a feed from {label, icalUrl}. The URL is
// write-only and verified reachable first, so "connected" is honest. It is
// never logged or returned.
func (s *Server) handleConnectCalendar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label   string `json:"label"`
		ICalURL string `json:"icalUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	icalURL := strings.TrimSpace(body.ICalURL)
	if icalURL == "" {
		http.Error(w, "icalUrl required", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(body.Label)
	if len([]rune(label)) > 60 {
		label = string([]rune(label)[:60])
	}
	if err := s.calendarValidate(r.Context(), icalURL); err != nil {
		http.Error(w, "could not reach that calendar URL - check it and try again", http.StatusBadGateway)
		return
	}
	feed, err := s.store.AddCalendarFeed(label, icalURL)
	if errors.Is(err, store.ErrDuplicateFeed) {
		http.Error(w, "that calendar is already connected", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// See handleConnectLinear for why a sync failure does not fail the connect.
	if s.syncCalendar != nil {
		_, _ = s.syncCalendar(r.Context())
	}
	if feeds, err := s.store.CalendarFeeds(); err == nil {
		for _, f := range feeds {
			if f.ID == feed.ID {
				feed = f
			}
		}
	}
	s.writeJSON(w, feedView(feed))
}

// handleRenameCalendar serves PUT /api/calendars/{id}/label: it sets the name
// the user gave that feed; an empty label goes back to the calendar's own name.
func (s *Server) handleRenameCalendar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(body.Label)
	if len([]rune(label)) > 80 {
		http.Error(w, "that name is too long", http.StatusBadRequest)
		return
	}
	ok, err := s.store.RenameCalendarFeed(r.PathValue("id"), label)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "unknown calendar", http.StatusNotFound)
		return
	}
	s.writeJSON(w, map[string]string{"Status": "renamed"})
}

// handleRemoveCalendar serves DELETE /api/calendars/{id}: it forgets that one
// feed and only its events.
func (s *Server) handleRemoveCalendar(w http.ResponseWriter, r *http.Request) {
	ok, err := s.store.RemoveCalendarFeed(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "unknown calendar", http.StatusNotFound)
		return
	}
	s.writeJSON(w, map[string]string{"Status": "removed"})
}
