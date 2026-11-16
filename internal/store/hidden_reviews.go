package store

import (
	"slices"
	"time"
)

// HideReview records that the user hid the review of the PR at url, now at. Hiding
// it again moves the time on.
func (s *Store) HideReview(url string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO hidden_reviews(url,hidden_at) VALUES(?,?) ON CONFLICT(url) DO UPDATE SET hidden_at=excluded.hidden_at`, url, rfc(at))
	return err
}

// UnhideReview forgets that the PR at url was hidden.
func (s *Store) UnhideReview(url string) error {
	_, err := s.db.Exec(`DELETE FROM hidden_reviews WHERE url=?`, url)
	return err
}

// HiddenReviews returns when each hidden PR was hidden, by URL.
func (s *Store) HiddenReviews() (map[string]time.Time, error) {
	rows, err := s.db.Query(`SELECT url,hidden_at FROM hidden_reviews`)
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

// KeepHiddenReviews forgets every hidden PR not in urls.
func (s *Store) KeepHiddenReviews(urls []string) error {
	hidden, err := s.HiddenReviews()
	if err != nil {
		return err
	}
	for url := range hidden {
		if !slices.Contains(urls, url) {
			if err := s.UnhideReview(url); err != nil {
				return err
			}
		}
	}
	return nil
}
