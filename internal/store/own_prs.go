package store

import (
	"slices"
	"time"
)

// OwnPRPrefix starts the external id of one of your own PRs, before its URL, so
// it never clashes with the "pr" row for the same PR.
const OwnPRPrefix = "own:"

// MarkOwnPRDone records that you marked the PR at url done, now at. This stays in
// the app: it never reaches GitHub, and no stat counts it. Marking it again moves
// the time on.
func (s *Store) MarkOwnPRDone(url string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO own_prs_done(url,done_at) VALUES(?,?) ON CONFLICT(url) DO UPDATE SET done_at=excluded.done_at`, url, rfc(at))
	return err
}

// UnmarkOwnPRDone forgets that the PR at url was marked done.
func (s *Store) UnmarkOwnPRDone(url string) error {
	_, err := s.db.Exec(`DELETE FROM own_prs_done WHERE url=?`, url)
	return err
}

// OwnPRsDone returns when each PR was marked done, by URL.
func (s *Store) OwnPRsDone() (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT url,done_at FROM own_prs_done`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var url, at string
		if err := rows.Scan(&url, &at); err != nil {
			return nil, err
		}
		out[url] = parse(at)
	}
	return out, rows.Err()
}

// KeepOwnPRsDone forgets every mark on a PR not in urls.
func (s *Store) KeepOwnPRsDone(urls []string) error {
	marks, err := s.OwnPRsDone()
	if err != nil {
		return err
	}
	for url := range marks {
		if !slices.Contains(urls, url) {
			if err := s.UnmarkOwnPRDone(url); err != nil {
				return err
			}
		}
	}
	return nil
}
