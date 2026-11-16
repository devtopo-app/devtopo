package store

import "database/sql"

// StickerOriginSetting records that stickers are placed from the page's edge.
const StickerOriginSetting = "stickers_placed_from_page"

// migrateStickerOrigin moves stickers once from being placed from the window's
// edge to the page's, which starts after the nav, so they stay where they were.
// The nav always opens at 180px wide.
func migrateStickerOrigin(db *sql.DB) error {
	var done string
	err := db.QueryRow(`SELECT value FROM settings WHERE key=?`, StickerOriginSetting).Scan(&done)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if done == "1" {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE stickers SET x = MAX(4, x - 180)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,'1') ON CONFLICT(key) DO UPDATE SET value='1'`, StickerOriginSetting); err != nil {
		return err
	}
	return tx.Commit()
}

// Sticker is a counter or a done-for-today mark on the Today page, pinned where
// the user placed it (X, Y and width W, in pixels from the page's top left, so it
// scrolls with the page). Count
// is its count on the day it was listed for; a done sticker counts 1 when done.
type Sticker struct {
	ID, Kind, Pic, Name string
	X, Y, W             float64
	Count               int
}

// PutSticker adds a sticker or updates its kind, picture, name and layout.
func (s *Store) PutSticker(st Sticker) error {
	_, err := s.db.Exec(
		`INSERT INTO stickers(id,kind,pic,name,x,y,w) VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, pic=excluded.pic, name=excluded.name, x=excluded.x, y=excluded.y, w=excluded.w`,
		st.ID, st.Kind, st.Pic, st.Name, st.X, st.Y, st.W,
	)
	return err
}

// SetStickerCount stores a sticker's count for day (YYYY-MM-DD).
func (s *Store) SetStickerCount(id, day string, n int) error {
	_, err := s.db.Exec(
		`INSERT INTO sticker_days(sticker_id,day,count) VALUES(?,?,?)
		 ON CONFLICT(sticker_id,day) DO UPDATE SET count=excluded.count`,
		id, day, n,
	)
	return err
}

// ListStickers returns every sticker, oldest first, with its count on day.
func (s *Store) ListStickers(day string) ([]Sticker, error) {
	rows, err := s.db.Query(
		`SELECT s.id, s.kind, s.pic, s.name, s.x, s.y, s.w, COALESCE(d.count, 0)
		 FROM stickers s LEFT JOIN sticker_days d ON d.sticker_id = s.id AND d.day = ?
		 ORDER BY s.rowid`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sticker{}
	for rows.Next() {
		var st Sticker
		if err := rows.Scan(&st.ID, &st.Kind, &st.Pic, &st.Name, &st.X, &st.Y, &st.W, &st.Count); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// DeleteSticker removes a sticker and all its counts.
func (s *Store) DeleteSticker(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM sticker_days WHERE sticker_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM stickers WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
