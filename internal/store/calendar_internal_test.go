package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// A database from before multiple calendars (one URL in settings, events with
// no feed column) opens with that URL as the first feed, its events kept.
func TestOpenMigratesTheSingleCalendarURLToAFeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE calendar_events (uid TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '',
			start_at TEXT NOT NULL DEFAULT '', end_at TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO settings(key,value) VALUES('calendar_ical_url','https://cal.example.com/private-secret/basic.ics')`,
		`INSERT INTO calendar_events VALUES('u1','Standup','2026-10-28T09:00:00Z','2026-10-28T09:30:00Z')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	feeds, err := st.CalendarFeeds()
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].URL != "https://cal.example.com/private-secret/basic.ics" || feeds[0].ID != legacyCalendarFeedID {
		t.Fatalf("the stored URL must become the first feed: %+v", feeds)
	}
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	ev, _ := st.EventsInWindow(day, day.Add(24*time.Hour))
	if len(ev) != 1 || ev[0].FeedID != feeds[0].ID {
		t.Fatalf("the events must be kept, under the new feed: %+v", ev)
	}
	if v, _, _ := st.GetSetting("calendar_ical_url"); v != "" {
		t.Fatal("the old setting must be cleared")
	}
	st.Close()

	// Opening again changes nothing.
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if feeds, _ := st.CalendarFeeds(); len(feeds) != 1 {
		t.Fatalf("a second Open must not add a feed: %+v", feeds)
	}
}

func TestFeedEventsAreReplacedAndRemovedPerFeed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return day.Add(time.Duration(h) * time.Hour) }
	a, err := st.AddCalendarFeed("Work", "https://cal.example.com/a.ics")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := st.AddCalendarFeed("", "https://cal.example.com/b.ics")
	if _, err := st.AddCalendarFeed("again", "https://cal.example.com/a.ics"); err != ErrDuplicateFeed {
		t.Fatalf("a URL already connected must be refused: %v", err)
	}
	_ = st.ReplaceFeedEvents(a.ID, []CalendarEvent{{UID: "a1", Title: "A1", Start: at(9), End: at(10)}, {UID: "a2", Title: "A2", Start: at(11), End: at(12)}})
	_ = st.ReplaceFeedEvents(b.ID, []CalendarEvent{{UID: "b1", Title: "B1", Start: at(10), End: at(11)}})

	// Replacing one feed leaves the other's events alone.
	_ = st.ReplaceFeedEvents(a.ID, []CalendarEvent{{UID: "a3", Title: "A3", Start: at(13), End: at(14)}})
	ev, _ := st.EventsInWindow(day, day.Add(24*time.Hour))
	if len(ev) != 2 || ev[0].Title != "B1" || ev[1].Title != "A3" || ev[1].Calendar != "Work" {
		t.Fatalf("after replacing feed a: %+v", ev)
	}

	// Removing one feed removes only its events.
	if ok, err := st.RemoveCalendarFeed(a.ID); err != nil || !ok {
		t.Fatalf("remove: ok=%v err=%v", ok, err)
	}
	ev, _ = st.EventsInWindow(day, day.Add(24*time.Hour))
	if len(ev) != 1 || ev[0].Title != "B1" {
		t.Fatalf("after removing feed a: %+v", ev)
	}
	if ok, _ := st.RemoveCalendarFeed(a.ID); ok {
		t.Fatal("removing a missing feed must report false")
	}
	// A sync that finishes after its feed was removed stores nothing.
	_ = st.ReplaceFeedEvents(a.ID, []CalendarEvent{{UID: "a4", Title: "A4", Start: at(15), End: at(16)}})
	if ev, _ = st.EventsInWindow(day, day.Add(24*time.Hour)); len(ev) != 1 {
		t.Fatalf("a removed feed must get no events back: %+v", ev)
	}
	if feeds, _ := st.CalendarFeeds(); len(feeds) != 1 || feeds[0].ID != b.ID {
		t.Fatalf("feeds: %+v", feeds)
	}
}
