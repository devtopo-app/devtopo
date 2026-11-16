package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// CalendarEvent is one meeting from a connected calendar. FeedID is the feed
// it came from; Calendar is that feed's label (filled by EventsInWindow).
type CalendarEvent struct {
	FeedID   string
	Calendar string
	UID      string
	Title    string
	Start    time.Time
	End      time.Time
}

// CalendarFeed is one connected iCal feed. URL is a secret: only the sync reads
// it, and it is never returned by an API or logged. LastError is the last
// sync's failure, empty after a good sync; it never carries the URL.
type CalendarFeed struct {
	ID         string
	Label      string // the name the user gave it; empty for none
	Name       string // the calendar's own name, from its feed (X-WR-CALNAME)
	URL        string
	CreatedAt  time.Time
	LastSyncAt time.Time
	LastError  string
}

// ErrDuplicateFeed means the URL is already connected.
var ErrDuplicateFeed = errors.New("that calendar is already connected")

// legacyCalendarFeedID is the id the single pre-feeds calendar URL migrates to.
const legacyCalendarFeedID = "cal-1"

// migrateCalendarFeeds moves the single calendar URL stored before multiple
// feeds (the calendar_ical_url setting) into calendar_feeds, keeps its events
// by pointing them at that feed, and clears the setting. It runs on every Open
// and does nothing once the setting is empty.
func migrateCalendarFeeds(db *sql.DB) error {
	var url string
	err := db.QueryRow(`SELECT value FROM settings WHERE key='calendar_ical_url'`).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && url == "") {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := legacyCalendarFeedID
	err = tx.QueryRow(`SELECT id FROM calendar_feeds WHERE url=?`, url).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM calendar_feeds WHERE id=?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			if id, err = newFeedID(); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO calendar_feeds(id,label,url,created_at) VALUES(?,'',?,?)`, id, url, rfc(time.Now().UTC())); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	if _, err := tx.Exec(`UPDATE calendar_events SET feed_id=? WHERE feed_id=''`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE settings SET value='' WHERE key='calendar_ical_url'`); err != nil {
		return err
	}
	return tx.Commit()
}

// newFeedID returns a random feed id.
func newFeedID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "cal-" + hex.EncodeToString(b[:]), nil
}

// AddCalendarFeed stores a new feed under a fresh id and returns it. A URL that
// is already connected is ErrDuplicateFeed.
func (s *Store) AddCalendarFeed(label, url string) (CalendarFeed, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM calendar_feeds WHERE url=?`, url).Scan(&n); err != nil {
		return CalendarFeed{}, err
	}
	if n > 0 {
		return CalendarFeed{}, ErrDuplicateFeed
	}
	id, err := newFeedID()
	if err != nil {
		return CalendarFeed{}, err
	}
	f := CalendarFeed{ID: id, Label: label, URL: url, CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(`INSERT INTO calendar_feeds(id,label,url,created_at) VALUES(?,?,?,?)`, f.ID, f.Label, f.URL, rfc(f.CreatedAt))
	return f, err
}

// CalendarFeeds returns the connected feeds, in the order they were added.
func (s *Store) CalendarFeeds() ([]CalendarFeed, error) {
	rows, err := s.db.Query(`SELECT id,label,name,url,created_at,last_sync_at,last_error FROM calendar_feeds ORDER BY rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarFeed
	for rows.Next() {
		var (
			f                 CalendarFeed
			created, lastSync string
		)
		if err := rows.Scan(&f.ID, &f.Label, &f.Name, &f.URL, &created, &lastSync, &f.LastError); err != nil {
			return nil, err
		}
		f.CreatedAt, f.LastSyncAt = parse(created), parse(lastSync)
		out = append(out, f)
	}
	return out, rows.Err()
}

// RemoveCalendarFeed forgets one feed and only its events. It reports whether
// the feed existed.
func (s *Store) RemoveCalendarFeed(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM calendar_feeds WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM calendar_events WHERE feed_id=?`, id); err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, tx.Commit()
}

// RemoveAllCalendarFeeds forgets every feed and every calendar event.
func (s *Store) RemoveAllCalendarFeeds() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM calendar_feeds`, `DELETE FROM calendar_events`} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetCalendarFeedStatus records a sync attempt of a feed: at is when, errMsg
// its failure, empty for a good sync. The caller keeps the URL out of errMsg.
func (s *Store) SetCalendarFeedStatus(id string, at time.Time, errMsg string) error {
	_, err := s.db.Exec(`UPDATE calendar_feeds SET last_sync_at=?, last_error=? WHERE id=?`, rfc(at), errMsg, id)
	return err
}

// SetCalendarFeedName records the calendar's own name, as its feed states it.
func (s *Store) SetCalendarFeedName(id, name string) error {
	_, err := s.db.Exec(`UPDATE calendar_feeds SET name=? WHERE id=?`, name, id)
	return err
}

// RenameCalendarFeed sets the name the user gave a feed; empty goes back to
// the calendar's own name. It reports whether the feed exists.
func (s *Store) RenameCalendarFeed(id, label string) (bool, error) {
	res, err := s.db.Exec(`UPDATE calendar_feeds SET label=? WHERE id=?`, label, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReplaceFeedEvents swaps one feed's events for events, in one transaction
// (the feed is the source of truth each sync); other feeds' events are left
// alone. A feed removed meanwhile gets no events back.
func (s *Store) ReplaceFeedEvents(feedID string, events []CalendarEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM calendar_events WHERE feed_id=?`, feedID); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM calendar_feeds WHERE id=?`, feedID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		if err := insertEvents(tx, feedID, events); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceCalendarEvents swaps the whole calendar_events table for events, in
// one transaction, each under its own FeedID. Syncs use ReplaceFeedEvents.
func (s *Store) ReplaceCalendarEvents(events []CalendarEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM calendar_events`); err != nil {
		return err
	}
	if err := insertEvents(tx, "", events); err != nil {
		return err
	}
	return tx.Commit()
}

// insertEvents inserts events under feedID, or under each event's own FeedID
// when feedID is empty.
func insertEvents(tx *sql.Tx, feedID string, events []CalendarEvent) error {
	for _, e := range events {
		id := feedID
		if id == "" {
			id = e.FeedID
		}
		if _, err := tx.Exec(
			`INSERT INTO calendar_events(feed_id,uid,title,start_at,end_at) VALUES(?,?,?,?,?)`,
			id, e.UID, e.Title, rfc(e.Start), rfc(e.End),
		); err != nil {
			return err
		}
	}
	return nil
}

// EventsInWindow returns events overlapping [start, end), ordered by start,
// each with its feed's label. An occurrence in more than one feed (the same
// UID at the same start, such as a meeting on two connected calendars) is
// returned once, from the feed connected first.
func (s *Store) EventsInWindow(start, end time.Time) ([]CalendarEvent, error) {
	rows, err := s.db.Query(
		`SELECT e.feed_id,COALESCE(NULLIF(f.label,''),f.name,''),e.uid,e.title,e.start_at,e.end_at
		 FROM calendar_events e LEFT JOIN calendar_feeds f ON f.id=e.feed_id
		 WHERE e.start_at < ? AND e.end_at > ? ORDER BY e.start_at ASC, COALESCE(f.rowid,0) ASC`,
		rfc(end), rfc(start),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarEvent
	seen := map[[2]string]bool{}
	for rows.Next() {
		var (
			e            CalendarEvent
			startAt, end string
		)
		if err := rows.Scan(&e.FeedID, &e.Calendar, &e.UID, &e.Title, &startAt, &end); err != nil {
			return nil, err
		}
		if e.UID != "" {
			k := [2]string{e.UID, startAt}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		e.Start, e.End = parse(startAt), parse(end)
		out = append(out, e)
	}
	return out, rows.Err()
}
