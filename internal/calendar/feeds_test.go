package calendar

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// urlFetcher answers per URL: a body, or an error when the URL is absent.
type urlFetcher map[string]string

func (f urlFetcher) Fetch(_ context.Context, url string) ([]byte, error) {
	body, ok := f[url]
	if !ok {
		return nil, errors.New("calendar: unexpected status 404")
	}
	return []byte(body), nil
}

const otherICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:b\r\nSUMMARY:Dentist\r\nDTSTART:20261028T140000Z\r\nDTEND:20261028T150000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

// Each feed syncs on its own: a feed that fails keeps its own meetings, does
// not stop the next feed, and records its error without the URL.
func TestSyncOneFailingFeedKeepsTheOthers(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	work, _ := st.AddCalendarFeed("Work", "https://x/work-secret.ics")
	home, _ := st.AddCalendarFeed("Personal", "https://x/home-secret.ics")
	both := urlFetcher{work.URL: sampleICS, home.URL: otherICS}
	if n, err := Sync(context.Background(), st, both, now); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}

	// Work now fails and Personal changes: Personal is replaced, Work kept.
	changed := strings.Replace(otherICS, "Dentist", "Gym", 1)
	n, err := Sync(context.Background(), st, urlFetcher{home.URL: changed}, now)
	if err == nil || n != 1 {
		t.Fatalf("want 1 synced and an error, got n=%d err=%v", n, err)
	}
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "Work") {
		t.Fatalf("the error must name the feed by label, never its URL: %v", err)
	}
	ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour))
	if len(ev) != 2 || ev[0].Title != "Meeting" || ev[1].Title != "Gym" {
		t.Fatalf("events: %+v", ev)
	}
	if ev[0].Calendar != "Work" || ev[1].Calendar != "Personal" {
		t.Fatalf("events must carry their feed's label: %+v", ev)
	}
	feeds, _ := st.CalendarFeeds()
	if feeds[0].LastError == "" || feeds[1].LastError != "" || feeds[1].LastSyncAt.IsZero() {
		t.Fatalf("per-feed status: %+v", feeds)
	}
	if strings.Contains(feeds[0].LastError, "secret") || strings.HasPrefix(feeds[0].LastError, "calendar:") {
		t.Fatalf("stored error: %q", feeds[0].LastError)
	}

	// A good sync clears the error.
	if _, err := Sync(context.Background(), st, both, now); err != nil {
		t.Fatal(err)
	}
	if feeds, _ := st.CalendarFeeds(); feeds[0].LastError != "" {
		t.Fatalf("a good sync must clear the error: %+v", feeds[0])
	}
}

// The same meeting on two connected calendars is shown once.
func TestSyncSameEventOnTwoFeedsShowsOnce(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	a, _ := st.AddCalendarFeed("Work", "https://x/a.ics")
	b, _ := st.AddCalendarFeed("Team", "https://x/b.ics")
	if _, err := Sync(context.Background(), st, urlFetcher{a.URL: sampleICS, b.URL: sampleICS}, now); err != nil {
		t.Fatal(err)
	}
	ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour))
	if len(ev) != 1 || ev[0].Calendar != "Work" {
		t.Fatalf("a shared meeting must show once, from the first feed: %+v", ev)
	}
	// Removing the first feed leaves the second's copy.
	if _, err := st.RemoveCalendarFeed(a.ID); err != nil {
		t.Fatal(err)
	}
	ev, _ = st.EventsInWindow(now, now.Add(24*time.Hour))
	if len(ev) != 1 || ev[0].Calendar != "Team" {
		t.Fatalf("the other feed's copy must stay: %+v", ev)
	}
}
