package calendar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devtopo-app/devtopo/internal/store"
)

// Fetcher is the slice of Client that Sync needs.
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Sync syncs every connected feed once; with none it is a no-op (not an
// error). Each feed is synced on its own (see SyncFeed), so one failing feed
// neither blocks the others nor touches their events. It returns the number of
// events stored by the feeds that synced, and the failures joined, each named
// by the feed's label and never its URL.
func Sync(ctx context.Context, st *store.Store, f Fetcher, now time.Time) (int, error) {
	feeds, err := st.CalendarFeeds()
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, feed := range feeds {
		n, err := SyncFeed(ctx, st, f, feed, now)
		if err != nil {
			name := feed.Label
			if name == "" {
				name = feed.ID
			}
			errs = append(errs, fmt.Errorf("calendar %q: %w", name, err))
			continue
		}
		total += n
	}
	return total, errors.Join(errs...)
}

// SyncFeed fetches one feed, parses its events over a horizon around now, and
// replaces that feed's stored events. A fetch or parse error (an HTML login
// page, a truncated or oversize feed) returns without replacing, so a bad
// response never wipes the feed's meetings. A valid feed with no events clears
// them. The outcome is recorded on the feed for Settings to show.
func SyncFeed(ctx context.Context, st *store.Store, f Fetcher, feed store.CalendarFeed, now time.Time) (int, error) {
	n, err := syncFeed(ctx, st, f, feed, now)
	msg := ""
	if err != nil {
		msg = strings.TrimPrefix(err.Error(), "calendar: ")
	}
	if serr := st.SetCalendarFeedStatus(feed.ID, now, msg); serr != nil && err == nil {
		err = serr
	}
	return n, err
}

func syncFeed(ctx context.Context, st *store.Store, f Fetcher, feed store.CalendarFeed, now time.Time) (int, error) {
	raw, err := f.Fetch(ctx, feed.URL)
	if err != nil {
		return 0, err
	}
	events, err := Parse(raw, now.Add(-24*time.Hour), now.Add(60*24*time.Hour))
	if err != nil {
		return 0, err
	}
	ce := make([]store.CalendarEvent, 0, len(events))
	for _, e := range events {
		ce = append(ce, store.CalendarEvent{UID: e.UID, Title: e.Title, Start: e.Start, End: e.End})
	}
	if err := st.ReplaceFeedEvents(feed.ID, ce); err != nil {
		return 0, err
	}
	if name := Name(raw); name != "" && name != feed.Name {
		if err := st.SetCalendarFeedName(feed.ID, name); err != nil {
			return 0, err
		}
	}
	return len(ce), nil
}
