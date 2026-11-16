package calendar

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

type fakeFetcher struct {
	ics []byte
	err error
}

func (f fakeFetcher) Fetch(context.Context, string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.ics, nil
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

const sampleICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VEVENT\r\nUID:a\r\nSUMMARY:Meeting\r\nDTSTART:20261028T100000Z\r\nDTEND:20261028T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestSyncNoURLIsNoop(t *testing.T) {
	st := openStore(t)
	n, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(sampleICS)}, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("no URL must be a no-op, got n=%d err=%v", n, err)
	}
}

func TestSyncStoresEvents(t *testing.T) {
	st := openStore(t)
	if _, err := st.AddCalendarFeed("Work", "https://x/private.ics"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	n, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(sampleICS)}, now)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	ev, _ := st.EventsInWindow(time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	if len(ev) != 1 || ev[0].Title != "Meeting" {
		t.Fatalf("stored events: %+v", ev)
	}
}

func TestSyncLeavesEventsOnFetchError(t *testing.T) {
	st := openStore(t)
	if _, err := st.AddCalendarFeed("Work", "https://x/private.ics"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	if _, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(sampleICS)}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{err: errors.New("down")}, now); err == nil {
		t.Fatal("expected a fetch error")
	}
	if ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour)); len(ev) != 1 {
		t.Fatalf("a fetch error must not wipe events, got %d", len(ev))
	}
}

// seed stores sampleICS's one meeting, the state a bad feed must not wipe.
func seed(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	if _, err := st.AddCalendarFeed("Work", "https://x/private.ics"); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(sampleICS)}, now); err != nil {
		t.Fatal(err)
	}
}

// A feed URL that answers 200 with a web page (an expired link's sign-in page)
// is an error and keeps the stored meetings.
func TestSyncLeavesEventsOnHTMLBody(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	seed(t, st, now)
	html := "<!DOCTYPE html><html><body>Sign in to continue</body></html>"
	if _, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(html)}, now); err == nil {
		t.Fatal("expected an error for an HTML body")
	}
	if ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour)); len(ev) != 1 {
		t.Fatalf("an HTML body must not wipe events, got %d", len(ev))
	}
}

func TestSyncLeavesEventsOnTruncatedFeed(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	seed(t, st, now)
	cut := sampleICS[:strings.Index(sampleICS, "END:VEVENT")]
	if _, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(cut)}, now); err == nil {
		t.Fatal("expected an error for a truncated feed")
	}
	if ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour)); len(ev) != 1 {
		t.Fatalf("a truncated feed must not wipe events, got %d", len(ev))
	}
}

// A feed over the size cap fails in Fetch rather than coming back cut short.
func TestSyncLeavesEventsOnOversizeFeed(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	seed(t, st, now)
	big := sampleICS + strings.Repeat("X", maxFeedBytes)
	c := &Client{httpDo: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(big)), Header: make(http.Header)}, nil
	}}
	if _, err := Sync(context.Background(), st, c, now); err == nil {
		t.Fatal("expected an error for an oversize feed")
	}
	if ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour)); len(ev) != 1 {
		t.Fatalf("an oversize feed must not wipe events, got %d", len(ev))
	}
}

// A valid calendar with no events in it does clear the stored meetings.
func TestSyncValidEmptyCalendarClearsEvents(t *testing.T) {
	st := openStore(t)
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.UTC)
	seed(t, st, now)
	empty := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nEND:VCALENDAR\r\n"
	n, err := Sync(context.Background(), st, fakeFetcher{ics: []byte(empty)}, now)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if ev, _ := st.EventsInWindow(now, now.Add(24*time.Hour)); len(ev) != 0 {
		t.Fatalf("an empty calendar must clear events, got %d", len(ev))
	}
}

func TestFetchRewritesWebcal(t *testing.T) {
	var got string
	c := &Client{httpDo: func(r *http.Request) (*http.Response, error) {
		got = r.URL.String()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sampleICS)), Header: make(http.Header)}, nil
	}}
	if _, err := c.Fetch(context.Background(), "webcal://calendar.example.com/feed.ics"); err != nil {
		t.Fatal(err)
	}
	if got != "https://calendar.example.com/feed.ics" {
		t.Fatalf("webcal must be fetched over https, got %q", got)
	}
}
