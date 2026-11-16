package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestCalendarsAddListRemoveNeverExposeURLs(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, now, nil, nil)
	s.calendarValidate = func(context.Context, string) error { return nil }
	s.syncCalendar = func(context.Context) (int, error) { return 0, nil }

	for _, body := range []string{
		`{"label":"Work","icalUrl":"https://calendar.google.com/calendar/ical/x/private-topsecret1/basic.ics"}`,
		`{"label":"Personal","icalUrl":"webcal://calendar.example.com/private-topsecret2/basic.ics"}`,
	} {
		rec := do(t, s, "POST", "/api/calendars", body)
		if rec.Code != 200 {
			t.Fatalf("add status %d: %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "topsecret") {
			t.Fatalf("the add response must never echo the url: %s", rec.Body.String())
		}
	}
	if rec := do(t, s, "POST", "/api/calendars", `{"label":"Again","icalUrl":"https://calendar.google.com/calendar/ical/x/private-topsecret1/basic.ics"}`); rec.Code != 409 {
		t.Fatalf("a URL already connected -> 409, got %d", rec.Code)
	}

	rec := do(t, s, "GET", "/api/calendars", "")
	if strings.Contains(rec.Body.String(), "topsecret") || strings.Contains(rec.Body.String(), "/calendar/ical") {
		t.Fatalf("the list must never expose a url: %s", rec.Body.String())
	}
	var feeds []calendarFeedView
	if err := json.Unmarshal(rec.Body.Bytes(), &feeds); err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 || feeds[0].Label != "Work" || feeds[0].Host != "calendar.google.com/…" ||
		feeds[1].Label != "Personal" || feeds[1].Host != "calendar.example.com/…" || feeds[0].Status != "pending" {
		t.Fatalf("feeds: %+v", feeds)
	}
	if rec := do(t, s, "GET", "/api/connectors", ""); strings.Contains(rec.Body.String(), "topsecret") {
		t.Fatal("the connectors endpoint must never expose a url")
	}
	if s.calendarStatus() != "connected" {
		t.Fatal("calendar must show as connected")
	}

	// Removing one feed removes only its events.
	stored, _ := s.store.CalendarFeeds()
	_ = s.store.ReplaceFeedEvents(stored[0].ID, []store.CalendarEvent{{UID: "w", Title: "Standup", Start: now, End: now.Add(time.Hour)}})
	_ = s.store.ReplaceFeedEvents(stored[1].ID, []store.CalendarEvent{{UID: "p", Title: "Dentist", Start: now, End: now.Add(time.Hour)}})
	if rec := do(t, s, "DELETE", "/api/calendars/"+feeds[0].ID, ""); rec.Code != 200 {
		t.Fatalf("remove status %d", rec.Code)
	}
	evs, _ := s.store.EventsInWindow(now.Add(-time.Hour), now.Add(2*time.Hour))
	if len(evs) != 1 || evs[0].Title != "Dentist" {
		t.Fatalf("only the removed feed's events must go: %+v", evs)
	}
	if rec := do(t, s, "DELETE", "/api/calendars/"+feeds[0].ID, ""); rec.Code != 404 {
		t.Fatalf("removing it again -> 404, got %d", rec.Code)
	}
	if s.calendarStatus() != "connected" {
		t.Fatal("one feed left, so still connected")
	}
}

func TestCalendarFeedShowsLastSyncError(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, now, nil, nil)
	f, _ := s.store.AddCalendarFeed("Work", "https://cal.example.com/private-topsecret.ics")
	_ = s.store.SetCalendarFeedStatus(f.ID, now, "unexpected status 404")
	rec := do(t, s, "GET", "/api/calendars", "")
	var feeds []calendarFeedView
	_ = json.Unmarshal(rec.Body.Bytes(), &feeds)
	if len(feeds) != 1 || feeds[0].Status != "error" || feeds[0].Error != "unexpected status 404" || feeds[0].LastSyncAt == nil {
		t.Fatalf("feeds: %+v", feeds)
	}
}

func TestMaskFeedURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://calendar.google.com/calendar/ical/a%40b/private-x/basic.ics": "calendar.google.com/…",
		"webcal://cal.example.com/feed.ics?token=x":                           "cal.example.com/…",
		"not a url": "",
	} {
		if got := maskFeedURL(raw); got != want {
			t.Fatalf("%q: got %q want %q", raw, got, want)
		}
	}
}

func TestCalendarShowsItsOwnNameAndCanBeRenamed(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	s := newSeededServer(t, now, nil, nil)
	f, _ := s.store.AddCalendarFeed("", "https://cal.example.com/private-topsecret.ics")
	_ = s.store.SetCalendarFeedName(f.ID, "Team calendar")
	_ = s.store.ReplaceFeedEvents(f.ID, []store.CalendarEvent{{UID: "a", Title: "Standup", Start: now, End: now.Add(time.Hour)}})

	list := func() calendarFeedView {
		t.Helper()
		var feeds []calendarFeedView
		_ = json.Unmarshal(do(t, s, "GET", "/api/calendars", "").Body.Bytes(), &feeds)
		if len(feeds) != 1 {
			t.Fatalf("feeds: %+v", feeds)
		}
		return feeds[0]
	}
	calendarOf := func() string {
		evs, _ := s.store.EventsInWindow(now.Add(-time.Hour), now.Add(2*time.Hour))
		if len(evs) != 1 {
			t.Fatalf("events: %+v", evs)
		}
		return evs[0].Calendar
	}

	if v := list(); v.Name != "Team calendar" || v.Label != "" {
		t.Fatalf("own name: %+v", v)
	}
	if calendarOf() != "Team calendar" {
		t.Fatal("events carry the calendar's own name when it has no label")
	}
	if rec := do(t, s, "PUT", "/api/calendars/"+f.ID+"/label", `{"label":"  Work  "}`); rec.Code != 200 {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if v := list(); v.Label != "Work" || v.Name != "Team calendar" {
		t.Fatalf("after rename: %+v", v)
	}
	if calendarOf() != "Work" {
		t.Fatal("the name the user gave wins")
	}
	if rec := do(t, s, "PUT", "/api/calendars/"+f.ID+"/label", `{"label":""}`); rec.Code != 200 || list().Label != "" || calendarOf() != "Team calendar" {
		t.Fatal("an empty name goes back to the calendar's own name")
	}
	if rec := do(t, s, "PUT", "/api/calendars/nope/label", `{"label":"x"}`); rec.Code != 404 {
		t.Fatalf("unknown calendar -> 404, got %d", rec.Code)
	}
	if rec := do(t, s, "PUT", "/api/calendars/"+f.ID+"/label", `{"label":"`+strings.Repeat("x", 81)+`"}`); rec.Code != 400 {
		t.Fatalf("a too long name -> 400, got %d", rec.Code)
	}
}
