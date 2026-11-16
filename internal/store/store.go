// Package store is the local SQLite store for derived capture data. It never
// holds raw transcript text, only the facts and numbers the views read.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver

	"github.com/devtopo-app/devtopo/internal/facts"
)

// Project is a repo, the unit the UI groups by.
type Project struct {
	Key  string
	Name string
}

// Session is one agent run. AIActiveSeconds is the absolute total the caller
// computed; LastEventAt is its newest real event, which status reads.
type Session struct {
	ID              string
	ProjectKey      string
	Agent           string
	CWD             string
	Branch          string
	StartedAt       time.Time
	LastActivityAt  time.Time
	LastEventAt     time.Time
	LastEventIsAI   bool
	AIActiveSeconds float64
	Status          string
	Tail            string // most recent event shape: ai_working / ai_done / human
	TranscriptPath  string // the raw .jsonl this session was read from (cleaned)
	// Subagents are its sub-agents' own newest real event and tail shape, kept
	// apart so they never move LastEventAt or Tail. Only ListSessions fills it.
	Subagents []Subagent
}

// Subagent is one sub-agent transcript's newest real event and tail shape.
// Path is its transcript, filled when read back (Apply takes the cursor path).
type Subagent struct {
	LastEventAt time.Time
	Tail        string
	Path        string
}

// Summary is a stored summary at a level (session for now; project/overall
// later). Structured is the metrics JSON; Narrative is the optional prose.
type Summary struct {
	Level         string
	SubjectID     string
	AsOf          time.Time
	Narrative     string
	Structured    string
	SchemaVersion int
}

// Delta is one file's worth of change, applied in a single transaction. Days
// is the batch's per-local-day activity, keyed "2006-01-02", added onto what
// the session already has for those days. CursorBoundary is the file's own state (last
// event, running background tasks) that seeds its next batch. Subagent, when its
// Tail is set, is the state of the sub-agent transcript at CursorPath.
type Delta struct {
	Project        Project
	Session        Session
	Facts          []facts.Fact
	Days           map[string]facts.Day
	CursorPath     string
	CursorOffset   int64
	CursorBoundary facts.Boundary
	Subagent       Subagent
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	key  TEXT PRIMARY KEY,
	name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id                TEXT PRIMARY KEY,
	project_key       TEXT NOT NULL,
	agent             TEXT NOT NULL DEFAULT '',
	cwd               TEXT NOT NULL DEFAULT '',
	branch            TEXT NOT NULL DEFAULT '',
	started_at        TEXT NOT NULL DEFAULT '',
	last_activity_at  TEXT NOT NULL DEFAULT '',
	last_event_at     TEXT NOT NULL DEFAULT '',
	last_event_is_ai  INTEGER NOT NULL DEFAULT 0,
	ai_active_seconds REAL NOT NULL DEFAULT 0,
	status            TEXT NOT NULL DEFAULT '',
	tail              TEXT NOT NULL DEFAULT '',
	transcript_path   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS facts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	type       TEXT NOT NULL,
	timestamp  TEXT NOT NULL DEFAULT '',
	tool_name  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS cursors (
	path           TEXT PRIMARY KEY,
	byte_offset    INTEGER NOT NULL,
	boundary_at    TEXT NOT NULL DEFAULT '',
	boundary_is_ai INTEGER NOT NULL DEFAULT 0,
	background     TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS summaries (
	level          TEXT NOT NULL,
	subject_id     TEXT NOT NULL,
	as_of          TEXT NOT NULL DEFAULT '',
	narrative      TEXT NOT NULL DEFAULT '',
	structured     TEXT NOT NULL DEFAULT '',
	schema_version INTEGER NOT NULL DEFAULT 1,
	PRIMARY KEY(level, subject_id)
);
CREATE TABLE IF NOT EXISTS external_items (
	source                  TEXT NOT NULL,
	external_id             TEXT NOT NULL,
	kind                    TEXT NOT NULL DEFAULT '',
	title                   TEXT NOT NULL DEFAULT '',
	state                   TEXT NOT NULL DEFAULT '',
	url                     TEXT NOT NULL DEFAULT '',
	repo                    TEXT NOT NULL DEFAULT '',
	review_requested_of_me  INTEGER NOT NULL DEFAULT 0,
	assigned_to_me          INTEGER NOT NULL DEFAULT 0,
	updated_at              TEXT NOT NULL DEFAULT '',
	estimate                REAL NOT NULL DEFAULT 0,
	stage                   TEXT NOT NULL DEFAULT '',
	project                 TEXT NOT NULL DEFAULT '',
	labels                  TEXT NOT NULL DEFAULT '',
	completed_at            TEXT NOT NULL DEFAULT '',
	started_at              TEXT NOT NULL DEFAULT '',
	additions               INTEGER NOT NULL DEFAULT 0,
	deletions               INTEGER NOT NULL DEFAULT 0,
	changed_files           INTEGER NOT NULL DEFAULT 0,
	requested_at            TEXT NOT NULL DEFAULT '',
	reviewer                TEXT NOT NULL DEFAULT '',
	reviewed_at             TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(source, external_id)
);
CREATE TABLE IF NOT EXISTS plans (
	id           TEXT PRIMARY KEY,
	window_start TEXT NOT NULL DEFAULT '',
	window_end   TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL DEFAULT '',
	status       TEXT NOT NULL DEFAULT 'active',
	name         TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS plan_rows (
	id        TEXT PRIMARY KEY,
	plan_id   TEXT NOT NULL,
	name      TEXT NOT NULL DEFAULT '',
	row_order INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS plan_items (
	id             TEXT PRIMARY KEY,
	plan_id        TEXT NOT NULL,
	ref_source     TEXT NOT NULL DEFAULT '',
	ref_id         TEXT NOT NULL DEFAULT '',
	text           TEXT NOT NULL DEFAULT '',
	intended_hours REAL NOT NULL DEFAULT 0,
	row_id         TEXT NOT NULL DEFAULT '',
	start_at       TEXT NOT NULL DEFAULT '',
	done_at        TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS session_days (
	session_id TEXT NOT NULL,
	day        TEXT NOT NULL,
	ai_seconds REAL NOT NULL DEFAULT 0,
	first_at   TEXT NOT NULL DEFAULT '',
	last_at    TEXT NOT NULL DEFAULT '',
	hours      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(session_id, day)
);
-- History and Today read one day's rows; the key starts with the session.
CREATE INDEX IF NOT EXISTS session_days_day ON session_days(day);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS session_names (
	session_id TEXT PRIMARY KEY,
	name       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_hosts (
	session_id TEXT PRIMARY KEY,
	host       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS calendar_feeds (
	id           TEXT PRIMARY KEY,
	label        TEXT NOT NULL DEFAULT '',
	name         TEXT NOT NULL DEFAULT '',
	url          TEXT NOT NULL,
	created_at   TEXT NOT NULL DEFAULT '',
	last_sync_at TEXT NOT NULL DEFAULT '',
	last_error   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS calendar_events (
	feed_id  TEXT NOT NULL DEFAULT '',
	uid      TEXT NOT NULL DEFAULT '',
	title    TEXT NOT NULL DEFAULT '',
	start_at TEXT NOT NULL DEFAULT '',
	end_at   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS hidden_reviews (
	url       TEXT PRIMARY KEY,
	hidden_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS own_prs_done (
	url     TEXT PRIMARY KEY,
	done_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS finishes (
	source      TEXT NOT NULL,
	external_id TEXT NOT NULL,
	kind        TEXT NOT NULL,
	key         TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL DEFAULT '',
	url         TEXT NOT NULL DEFAULT '',
	at          TEXT NOT NULL,
	PRIMARY KEY(source, external_id, at)
);
CREATE TABLE IF NOT EXISTS subagents (
	path          TEXT PRIMARY KEY,
	session_id    TEXT NOT NULL,
	last_event_at TEXT NOT NULL DEFAULT '',
	tail          TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS stickers (
	id   TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	pic  TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	x    REAL NOT NULL DEFAULT 0,
	y    REAL NOT NULL DEFAULT 0,
	w    REAL NOT NULL DEFAULT 0
);
-- The page polls a status that counts each session's facts and reads today's
-- by type; without these each poll scanned the whole table per session.
CREATE INDEX IF NOT EXISTS facts_session_type ON facts(session_id, type);
CREATE INDEX IF NOT EXISTS facts_type_time ON facts(type, timestamp);
CREATE TABLE IF NOT EXISTS continued_sessions (
	old_id TEXT PRIMARY KEY,
	new_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sticker_days (
	sticker_id TEXT NOT NULL,
	day        TEXT NOT NULL,
	count      INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(sticker_id, day)
);
`

// Open opens the database at path and runs the idempotent migrations. It uses
// WAL and a busy timeout so another process reading it (devtopo serve) waits
// rather than failing with "database is locked". Within DevTopo the watcher,
// the summarizer, the connectors and the HTTP handlers all use the store from
// their own goroutines; the single connection is what puts their queries one
// after another.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Add columns an older DB predates. CREATE TABLE IF NOT EXISTS leaves an
	// existing table untouched, so the columns are added here.
	for _, col := range []struct{ table, name, ddl string }{
		{"sessions", "tail", `ALTER TABLE sessions ADD COLUMN tail TEXT NOT NULL DEFAULT ''`},
		{"sessions", "transcript_path", `ALTER TABLE sessions ADD COLUMN transcript_path TEXT NOT NULL DEFAULT ''`},
		{"external_items", "estimate", `ALTER TABLE external_items ADD COLUMN estimate REAL NOT NULL DEFAULT 0`},
		{"external_items", "stage", `ALTER TABLE external_items ADD COLUMN stage TEXT NOT NULL DEFAULT ''`},
		{"external_items", "project", `ALTER TABLE external_items ADD COLUMN project TEXT NOT NULL DEFAULT ''`},
		{"external_items", "labels", `ALTER TABLE external_items ADD COLUMN labels TEXT NOT NULL DEFAULT ''`},
		{"external_items", "completed_at", `ALTER TABLE external_items ADD COLUMN completed_at TEXT NOT NULL DEFAULT ''`},
		{"external_items", "started_at", `ALTER TABLE external_items ADD COLUMN started_at TEXT NOT NULL DEFAULT ''`},
		{"external_items", "additions", `ALTER TABLE external_items ADD COLUMN additions INTEGER NOT NULL DEFAULT 0`},
		{"external_items", "deletions", `ALTER TABLE external_items ADD COLUMN deletions INTEGER NOT NULL DEFAULT 0`},
		{"external_items", "changed_files", `ALTER TABLE external_items ADD COLUMN changed_files INTEGER NOT NULL DEFAULT 0`},
		{"external_items", "requested_at", `ALTER TABLE external_items ADD COLUMN requested_at TEXT NOT NULL DEFAULT ''`},
		{"external_items", "reviewer", `ALTER TABLE external_items ADD COLUMN reviewer TEXT NOT NULL DEFAULT ''`},
		{"external_items", "reviewed_at", `ALTER TABLE external_items ADD COLUMN reviewed_at TEXT NOT NULL DEFAULT ''`},
		{"plans", "name", `ALTER TABLE plans ADD COLUMN name TEXT NOT NULL DEFAULT ''`},
		{"plan_items", "done_at", `ALTER TABLE plan_items ADD COLUMN done_at TEXT NOT NULL DEFAULT ''`},
		{"plan_items", "row_id", `ALTER TABLE plan_items ADD COLUMN row_id TEXT NOT NULL DEFAULT ''`},
		{"plan_items", "start_at", `ALTER TABLE plan_items ADD COLUMN start_at TEXT NOT NULL DEFAULT ''`},
		{"cursors", "boundary_at", `ALTER TABLE cursors ADD COLUMN boundary_at TEXT NOT NULL DEFAULT ''`},
		{"cursors", "boundary_is_ai", `ALTER TABLE cursors ADD COLUMN boundary_is_ai INTEGER NOT NULL DEFAULT 0`},
		{"cursors", "background", `ALTER TABLE cursors ADD COLUMN background TEXT NOT NULL DEFAULT ''`},
		{"session_days", "hours", `ALTER TABLE session_days ADD COLUMN hours TEXT NOT NULL DEFAULT ''`},
		{"calendar_events", "feed_id", `ALTER TABLE calendar_events ADD COLUMN feed_id TEXT NOT NULL DEFAULT ''`},
		{"calendar_feeds", "name", `ALTER TABLE calendar_feeds ADD COLUMN name TEXT NOT NULL DEFAULT ''`},
	} {
		if err := ensureColumn(db, col.table, col.name, col.ddl); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := migratePlans(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateCalendarFeeds(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateStickerOrigin(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migratePlans brings plans from before named plans and rows up to date: an
// unnamed plan is named after its date, and a plan with no rows gets the
// default rows. Its items keep no row and no start, so they land in "anytime
// this day". A plan always keeps at least one row, so this never re-adds rows
// the user removed.
func migratePlans(db *sql.DB) error {
	rows, err := db.Query(`SELECT id,window_start FROM plans WHERE name=''`)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for rows.Next() {
		var id, ws string
		if err := rows.Scan(&id, &ws); err != nil {
			rows.Close()
			return err
		}
		names[id] = defaultPlanName(parse(ws))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for id, name := range names {
		if _, err := db.Exec(`UPDATE plans SET name=? WHERE id=?`, name, id); err != nil {
			return err
		}
	}
	_, err = db.Exec(addDefaultRows)
	return err
}

// addDefaultRows gives every plan that has no rows the two rows a new plan
// starts with. Their ids derive from the plan id.
const addDefaultRows = `INSERT INTO plan_rows(id,plan_id,name,row_order)
	SELECT id||'-work', id, 'Work plan', 0 FROM plans WHERE id NOT IN (SELECT plan_id FROM plan_rows)
	UNION ALL
	SELECT id||'-private', id, 'Private activities', 1 FROM plans WHERE id NOT IN (SELECT plan_id FROM plan_rows)`

// defaultPlanName names a plan after its local date, e.g. "Plan for Wed 28 Oct".
// The app runs on the user's machine, so local time is the user's time.
func defaultPlanName(windowStart time.Time) string {
	return windowStart.Local().Format("Plan for Mon 2 Jan")
}

// ensureColumn adds a column to a table if it is absent. Presence is detected
// via PRAGMA rather than by swallowing an ALTER error, so a real failure is
// never masked.
func ensureColumn(db *sql.DB, table, name, addSQL string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var (
			cid, notnull, pk int
			cname, ctype     string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if cname == name {
			has = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if has {
		return nil
	}
	_, err = db.Exec(addSQL)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Summary returns the stored project and session counts and the total AI active
// seconds across all sessions, for a one-line ingest report.
func (s *Store) Summary() (projects, sessions int, aiSeconds float64, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&projects); err != nil {
		return 0, 0, 0, err
	}
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		return 0, 0, 0, err
	}
	if err = s.db.QueryRow(`SELECT COALESCE(SUM(ai_active_seconds),0) FROM sessions`).Scan(&aiSeconds); err != nil {
		return 0, 0, 0, err
	}
	return projects, sessions, aiSeconds, nil
}

// Apply writes one file's delta - project, session, facts, cursor - in a single
// transaction. A failure rolls the whole thing back, so the cursor never
// advances without its data and no data is written twice.
func (s *Store) Apply(d Delta) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if _, err = tx.Exec(
		`INSERT INTO projects(key,name) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET name=excluded.name`,
		d.Project.Key, d.Project.Name,
	); err != nil {
		return err
	}

	se := d.Session
	if _, err = tx.Exec(
		`INSERT INTO sessions(id,project_key,agent,cwd,branch,started_at,last_activity_at,last_event_at,last_event_is_ai,ai_active_seconds,status,tail,transcript_path)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   project_key=excluded.project_key, agent=excluded.agent, cwd=excluded.cwd, branch=excluded.branch,
		   started_at=excluded.started_at, last_activity_at=excluded.last_activity_at, last_event_at=excluded.last_event_at,
		   last_event_is_ai=excluded.last_event_is_ai, ai_active_seconds=excluded.ai_active_seconds, status=excluded.status, tail=excluded.tail,
		   transcript_path=excluded.transcript_path`,
		se.ID, se.ProjectKey, se.Agent, se.CWD, se.Branch,
		rfc(se.StartedAt), rfc(se.LastActivityAt), rfc(se.LastEventAt),
		b2i(se.LastEventIsAI), se.AIActiveSeconds, se.Status, se.Tail, se.TranscriptPath,
	); err != nil {
		return err
	}

	for _, f := range d.Facts {
		if _, err = tx.Exec(
			`INSERT INTO facts(session_id,type,timestamp,tool_name) VALUES(?,?,?,?)`,
			se.ID, f.Type, rfc(f.Timestamp), f.ToolName,
		); err != nil {
			return err
		}
	}

	for day, add := range d.Days {
		if err = addDay(tx, se.ID, day, add); err != nil {
			return err
		}
	}

	bg, err := json.Marshal(background{d.CursorBoundary.Pending, d.CursorBoundary.Spans, d.CursorBoundary.Tasks})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		`INSERT INTO cursors(path,byte_offset,boundary_at,boundary_is_ai,background) VALUES(?,?,?,?,?)
		 ON CONFLICT(path) DO UPDATE SET byte_offset=excluded.byte_offset, boundary_at=excluded.boundary_at, boundary_is_ai=excluded.boundary_is_ai, background=excluded.background`,
		d.CursorPath, d.CursorOffset, rfc(d.CursorBoundary.Time), b2i(d.CursorBoundary.IsAI), string(bg),
	); err != nil {
		return err
	}

	if d.Subagent.Tail != "" {
		if _, err = tx.Exec(
			`INSERT INTO subagents(path,session_id,last_event_at,tail) VALUES(?,?,?,?)
			 ON CONFLICT(path) DO UPDATE SET session_id=excluded.session_id, last_event_at=excluded.last_event_at, tail=excluded.tail`,
			d.CursorPath, se.ID, rfc(d.Subagent.LastEventAt), d.Subagent.Tail,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// addDay adds a batch's day onto the stored row: AI time and its hours sum and
// first/last widen. The widening compares parsed times, since RFC3339Nano
// drops trailing zeros and its strings do not sort as times.
func addDay(tx *sql.Tx, sessionID, day string, add facts.Day) error {
	var (
		ai                 float64
		first, last, hours string
	)
	err := tx.QueryRow(`SELECT ai_seconds,first_at,last_at,hours FROM session_days WHERE session_id=? AND day=?`, sessionID, day).
		Scan(&ai, &first, &last, &hours)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	add.AISeconds += ai
	for h, secs := range parseHours(hours) {
		add.Hours[h] += secs
	}
	hoursJSON, err := json.Marshal(add.Hours)
	if err != nil {
		return err
	}
	if f := parse(first); !f.IsZero() && (add.First.IsZero() || f.Before(add.First)) {
		add.First = f
	}
	if l := parse(last); l.After(add.Last) {
		add.Last = l
	}
	_, err = tx.Exec(
		`INSERT INTO session_days(session_id,day,ai_seconds,first_at,last_at,hours) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(session_id,day) DO UPDATE SET ai_seconds=excluded.ai_seconds, first_at=excluded.first_at, last_at=excluded.last_at, hours=excluded.hours`,
		sessionID, day, add.AISeconds, rfc(add.First), rfc(add.Last), string(hoursJSON),
	)
	return err
}

// parseHours reads a day row's hours; a row from before hours were kept has
// none.
func parseHours(s string) [24]float64 {
	var h [24]float64
	json.Unmarshal([]byte(s), &h)
	return h
}

// MonthDays returns the AI seconds of every local day of month ("2006-01")
// that has any day row, summed over sessions and keyed "2006-01-02".
func (s *Store) MonthDays(month string) (map[string]float64, error) {
	rows, err := s.db.Query(`SELECT day,SUM(ai_seconds) FROM session_days WHERE substr(day,1,7)=? GROUP BY day`, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var (
			day string
			ai  float64
		)
		if err := rows.Scan(&day, &ai); err != nil {
			return nil, err
		}
		out[day] = ai
	}
	return out, rows.Err()
}

// ResetAITime clears what the ingest derived from each session's transcripts -
// its AI total, day rows, facts, and the cursors of its transcript and its
// sub-agents' - and sets the marker setting, in one transaction. The next
// ingest then reads those transcripts again from the start.
func (s *Store) ResetAITime(sessions []Session, marker string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, se := range sessions {
		subagents := strings.TrimSuffix(se.TranscriptPath, ".jsonl") + string(filepath.Separator) + "subagents" + string(filepath.Separator)
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`UPDATE sessions SET ai_active_seconds=0 WHERE id=?`, []any{se.ID}},
			{`DELETE FROM session_days WHERE session_id=?`, []any{se.ID}},
			{`DELETE FROM facts WHERE session_id=?`, []any{se.ID}},
			{`DELETE FROM cursors WHERE path=? OR substr(path,1,length(?))=?`, []any{se.TranscriptPath, subagents, subagents}},
		} {
			if _, err := tx.Exec(q.sql, q.args...); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,'1') ON CONFLICT(key) DO UPDATE SET value=excluded.value`, marker); err != nil {
		return err
	}
	return tx.Commit()
}

// SessionDays returns every session's row for one local day ("2006-01-02"),
// keyed by session id.
func (s *Store) SessionDays(day string) (map[string]facts.Day, error) {
	rows, err := s.db.Query(`SELECT session_id,ai_seconds,first_at,last_at,hours FROM session_days WHERE day=?`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]facts.Day{}
	for rows.Next() {
		var (
			id, first, last, hours string
			d                      facts.Day
		)
		if err := rows.Scan(&id, &d.AISeconds, &first, &last, &hours); err != nil {
			return nil, err
		}
		d.First, d.Last, d.Hours = parse(first), parse(last), parseHours(hours)
		out[id] = d
	}
	return out, rows.Err()
}

// background is the JSON form of a Boundary's background-task state.
type background struct {
	Pending map[string]time.Time `json:"pending,omitempty"`
	Spans   []facts.Span         `json:"spans,omitempty"`
	Tasks   map[string]string    `json:"tasks,omitempty"`
}

// CursorBoundary returns the state stored with a transcript's cursor: its last
// event and its background tasks. It is not Valid when the cursor is absent or
// kept no boundary.
func (s *Store) CursorBoundary(path string) (facts.Boundary, error) {
	var (
		at, bgJSON string
		isAI       int
	)
	err := s.db.QueryRow(`SELECT boundary_at,boundary_is_ai,background FROM cursors WHERE path=?`, path).Scan(&at, &isAI, &bgJSON)
	if err == sql.ErrNoRows {
		return facts.Boundary{}, nil
	}
	if err != nil {
		return facts.Boundary{}, err
	}
	var bg background
	if bgJSON != "" {
		if err := json.Unmarshal([]byte(bgJSON), &bg); err != nil {
			return facts.Boundary{}, err
		}
	}
	return facts.Boundary{Time: parse(at), IsAI: isAI != 0, Valid: at != "", Pending: bg.Pending, Spans: bg.Spans, Tasks: bg.Tasks}, nil
}

// OpenBackgroundTasks returns how many background tasks each transcript has
// running, by its path. Only a task with a known id counts: a launch recorded
// before ids were kept can't be told stopped from running.
func (s *Store) OpenBackgroundTasks() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT path,background FROM cursors WHERE background<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var path, bgJSON string
		if err := rows.Scan(&path, &bgJSON); err != nil {
			return nil, err
		}
		var bg background
		if json.Unmarshal([]byte(bgJSON), &bg) == nil {
			for _, id := range bg.Tasks {
				if _, ok := bg.Pending[id]; ok {
					out[path]++
				}
			}
			if _, ok := out[path]; !ok {
				out[path] = 0
			}
		}
	}
	return out, rows.Err()
}

// AdvanceCursor moves path's cursor to offset and leaves the rest of it: for
// a stretch of lines that held nothing to record.
func (s *Store) AdvanceCursor(path string, offset int64) error {
	_, err := s.db.Exec(`INSERT INTO cursors(path,byte_offset) VALUES(?,?)
		ON CONFLICT(path) DO UPDATE SET byte_offset=excluded.byte_offset`, path, offset)
	return err
}

// Cursor returns the byte offset for a transcript path, or 0 when absent.
func (s *Store) Cursor(path string) (int64, error) {
	var off int64
	err := s.db.QueryRow(`SELECT byte_offset FROM cursors WHERE path=?`, path).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return off, err
}

// GetSession returns the stored session; the bool is false when it is new.
func (s *Store) GetSession(id string) (Session, bool, error) {
	var (
		se                       Session
		started, lastAct, lastEv string
		isAI                     int
	)
	err := s.db.QueryRow(
		`SELECT id,project_key,agent,cwd,branch,started_at,last_activity_at,last_event_at,last_event_is_ai,ai_active_seconds,status,tail,transcript_path
		 FROM sessions WHERE id=?`, id,
	).Scan(&se.ID, &se.ProjectKey, &se.Agent, &se.CWD, &se.Branch, &started, &lastAct, &lastEv, &isAI, &se.AIActiveSeconds, &se.Status, &se.Tail, &se.TranscriptPath)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	se.StartedAt = parse(started)
	se.LastActivityAt = parse(lastAct)
	se.LastEventAt = parse(lastEv)
	se.LastEventIsAI = isAI != 0
	return se, true, nil
}

// ListSessions returns every stored session, parsed like GetSession.
func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(
		`SELECT id,project_key,agent,cwd,branch,started_at,last_activity_at,last_event_at,last_event_is_ai,ai_active_seconds,status,tail,transcript_path
		 FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var (
			se                       Session
			started, lastAct, lastEv string
			isAI                     int
		)
		if err := rows.Scan(&se.ID, &se.ProjectKey, &se.Agent, &se.CWD, &se.Branch, &started, &lastAct, &lastEv, &isAI, &se.AIActiveSeconds, &se.Status, &se.Tail, &se.TranscriptPath); err != nil {
			return nil, err
		}
		se.StartedAt = parse(started)
		se.LastActivityAt = parse(lastAct)
		se.LastEventAt = parse(lastEv)
		se.LastEventIsAI = isAI != 0
		out = append(out, se)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attachSubagents(out)
}

// attachSubagents fills each session's Subagents from the subagents table.
func (s *Store) attachSubagents(sessions []Session) error {
	rows, err := s.db.Query(`SELECT session_id,last_event_at,tail,path FROM subagents ORDER BY last_event_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[string][]Subagent{}
	for rows.Next() {
		var id, last, tail, path string
		if err := rows.Scan(&id, &last, &tail, &path); err != nil {
			return err
		}
		byID[id] = append(byID[id], Subagent{LastEventAt: parse(last), Tail: tail, Path: path})
	}
	for i := range sessions {
		sessions[i].Subagents = byID[sessions[i].ID]
	}
	return rows.Err()
}

// SetStatus updates only a session's status column.
func (s *Store) SetStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status=? WHERE id=?`, status, id)
	return err
}

// FactCounts returns the number of stored facts per type for a session. An
// unknown session yields a non-nil empty map.
func (s *Store) FactCounts(sessionID string) (map[string]int, error) {
	out := make(map[string]int)
	rows, err := s.db.Query(`SELECT type, COUNT(*) FROM facts WHERE session_id=? GROUP BY type`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return nil, err
		}
		out[typ] = n
	}
	return out, rows.Err()
}

// TaskStarts returns when each session's current task can have started, by session
// id: its latest prompt, its latest pick-up of a background task's news, or its
// latest end of turn, whichever is last. A task never starts before the last
// hand-back, so that is the floor when the agent resumed some other way.
// Given ids, only those sessions are read (the running ones, through the
// facts index), not every fact ever stored.
func (s *Store) TaskStarts(ids ...string) (map[string]time.Time, error) {
	q, args := `SELECT session_id,timestamp FROM facts WHERE type IN ('prompt','resume','turn_end')`, []any{}
	if len(ids) > 0 {
		q += ` AND session_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		if t := parse(at); t.After(out[id]) {
			out[id] = t
		}
	}
	return out, rows.Err()
}

// Prompts returns the number of prompt facts in [from, to) by session id. The
// SQL bound only narrows: RFC3339Nano strings do not sort as times within a
// second, so the exact range is checked on parsed times.
func (s *Store) Prompts(from, to time.Time) (map[string]int, error) {
	return s.factsInRange("prompt", from, to)
}

// Decisions returns the number of questions answered in [from, to) by session id.
func (s *Store) Decisions(from, to time.Time) (map[string]int, error) {
	return s.factsInRange("decision", from, to)
}

func (s *Store) factsInRange(kind string, from, to time.Time) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT session_id,timestamp FROM facts WHERE type=? AND timestamp>=?`, kind, rfc(from.Add(-time.Second)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := map[string]int{}
	for rows.Next() {
		var id, ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		if t := parse(ts); !t.Before(from) && t.Before(to) {
			n[id]++
		}
	}
	return n, rows.Err()
}

// UpsertSummary writes or replaces the summary at (level, subject_id).
func (s *Store) UpsertSummary(sum Summary) error {
	_, err := s.db.Exec(
		`INSERT INTO summaries(level,subject_id,as_of,narrative,structured,schema_version)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(level,subject_id) DO UPDATE SET
		   as_of=excluded.as_of, narrative=excluded.narrative,
		   structured=excluded.structured, schema_version=excluded.schema_version`,
		sum.Level, sum.SubjectID, rfc(sum.AsOf), sum.Narrative, sum.Structured, sum.SchemaVersion,
	)
	return err
}

// GetSummary returns the stored summary; the bool is false when it is absent.
func (s *Store) GetSummary(level, id string) (Summary, bool, error) {
	var (
		sum  Summary
		asOf string
	)
	err := s.db.QueryRow(
		`SELECT level,subject_id,as_of,narrative,structured,schema_version
		 FROM summaries WHERE level=? AND subject_id=?`, level, id,
	).Scan(&sum.Level, &sum.SubjectID, &asOf, &sum.Narrative, &sum.Structured, &sum.SchemaVersion)
	if err == sql.ErrNoRows {
		return Summary{}, false, nil
	}
	if err != nil {
		return Summary{}, false, err
	}
	sum.AsOf = parse(asOf)
	return sum, true, nil
}

// ListSummaries returns every stored summary at a level.
func (s *Store) ListSummaries(level string) ([]Summary, error) {
	return s.SummariesEndingIn(level, "")
}

// SummariesEndingIn returns level's summaries whose subject ends in suffix,
// such as one day's ("/2006-01-02"), so a view of today does not load every
// past day.
func (s *Store) SummariesEndingIn(level, suffix string) ([]Summary, error) {
	q, args := `SELECT level,subject_id,as_of,narrative,structured,schema_version
		 FROM summaries WHERE level=?`, []any{level}
	if suffix != "" {
		q += ` AND substr(subject_id, -length(?)) = ?`
		args = append(args, suffix, suffix)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var (
			sum  Summary
			asOf string
		)
		if err := rows.Scan(&sum.Level, &sum.SubjectID, &asOf, &sum.Narrative, &sum.Structured, &sum.SchemaVersion); err != nil {
			return nil, err
		}
		sum.AsOf = parse(asOf)
		out = append(out, sum)
	}
	return out, rows.Err()
}

// ExternalItem is one item pulled from a connected tool (GitHub, later Linear /
// Jira / Calendar). Only derived fields are stored, never the raw payload.
type ExternalItem struct {
	Source              string
	ExternalID          string
	Kind                string
	Title               string
	State               string
	URL                 string
	Repo                string
	ReviewRequestedOfMe bool
	AssignedToMe        bool
	UpdatedAt           time.Time
	Estimate            float64 // story points from a ticket tracker, 0 when unset
	Stage               string  // "started" | "unstarted" | "triage" | "backlog" | "completed" | "canceled" | "" (tickets only)
	Project             string
	Labels              []string
	CompletedAt         time.Time // when a ticket was completed, zero when not
	// Additions and Deletions are a review's lines without generated files, and
	// RequestedAt your latest own review request. ChangedFiles over 0 means known.
	Additions, Deletions, ChangedFiles int
	RequestedAt                        time.Time
	StartedAt                          time.Time // when a ticket entered its started stage, zero when not started
	// Reviewer and ReviewedAt are the review behind an own_pr's State.
	Reviewer   string
	ReviewedAt time.Time
}

// labelSep joins labels in one column; a unit separator never appears in a label.
const labelSep = "\x1f"

func splitLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, labelSep)
}

// ReplaceExternalItems atomically replaces every row for source with items, in
// one transaction, so an item that vanished upstream (a review you finished) is
// dropped on the next sync.
func (s *Store) ReplaceExternalItems(source string, items []ExternalItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM external_items WHERE source=?`, source); err != nil {
		return err
	}
	for _, it := range items {
		if _, err := tx.Exec(
			`INSERT INTO external_items(source,external_id,kind,title,state,url,repo,review_requested_of_me,assigned_to_me,updated_at,estimate,stage,project,labels,completed_at,started_at,additions,deletions,changed_files,requested_at,reviewer,reviewed_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			source, it.ExternalID, it.Kind, it.Title, it.State, it.URL, it.Repo,
			b2i(it.ReviewRequestedOfMe), b2i(it.AssignedToMe), rfc(it.UpdatedAt), it.Estimate, it.Stage,
			it.Project, strings.Join(it.Labels, labelSep), rfc(it.CompletedAt), rfc(it.StartedAt), it.Additions, it.Deletions, it.ChangedFiles, rfc(it.RequestedAt), it.Reviewer, rfc(it.ReviewedAt),
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(recordFinishes, source); err != nil {
		return err
	}
	return tx.Commit()
}

// recordFinishes copies a source's completed tickets and submitted reviews
// into finishes, which no sync replaces, so a day keeps what was finished on
// it after the trackers stop returning it.
const recordFinishes = `INSERT OR IGNORE INTO finishes(source,external_id,kind,key,title,url,at)
	SELECT source, external_id, kind, repo, title, url, CASE kind WHEN 'task' THEN completed_at ELSE updated_at END
	FROM external_items
	WHERE source=? AND ((kind='task' AND stage='completed' AND completed_at<>'') OR (kind='review_done' AND updated_at<>''))`

// Finish is a ticket completed (Kind "task") or a PR review submitted (Kind
// "review_done") at At. Key is the ticket key or the PR's repo.
type Finish struct {
	Source string
	Kind   string
	Key    string
	Title  string
	URL    string
	At     time.Time
}

// Finishes returns the finishes in [from, to), oldest first. The SQL bound
// only narrows, as in Prompts.
func (s *Store) Finishes(from, to time.Time) ([]Finish, error) {
	rows, err := s.db.Query(`SELECT source,kind,key,title,url,at FROM finishes WHERE at>=? ORDER BY at ASC`, rfc(from.Add(-time.Second)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Finish
	for rows.Next() {
		var (
			f  Finish
			at string
		)
		if err := rows.Scan(&f.Source, &f.Kind, &f.Key, &f.Title, &f.URL, &at); err != nil {
			return nil, err
		}
		if f.At = parse(at); !f.At.Before(from) && f.At.Before(to) {
			out = append(out, f)
		}
	}
	return out, rows.Err()
}

// UpsertExternalItem writes or replaces one item at (source, external_id).
func (s *Store) UpsertExternalItem(it ExternalItem) error {
	_, err := s.db.Exec(
		`INSERT INTO external_items(source,external_id,kind,title,state,url,repo,review_requested_of_me,assigned_to_me,updated_at,estimate,stage,project,labels,completed_at,started_at,additions,deletions,changed_files,requested_at,reviewer,reviewed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(source,external_id) DO UPDATE SET
		   kind=excluded.kind, title=excluded.title, state=excluded.state, url=excluded.url,
		   repo=excluded.repo, review_requested_of_me=excluded.review_requested_of_me,
		   assigned_to_me=excluded.assigned_to_me, updated_at=excluded.updated_at, estimate=excluded.estimate,
		   stage=excluded.stage, project=excluded.project, labels=excluded.labels,
		   completed_at=excluded.completed_at, started_at=excluded.started_at, additions=excluded.additions,
		   deletions=excluded.deletions, changed_files=excluded.changed_files, requested_at=excluded.requested_at,
		   reviewer=excluded.reviewer, reviewed_at=excluded.reviewed_at`,
		it.Source, it.ExternalID, it.Kind, it.Title, it.State, it.URL, it.Repo,
		b2i(it.ReviewRequestedOfMe), b2i(it.AssignedToMe), rfc(it.UpdatedAt), it.Estimate, it.Stage,
		it.Project, strings.Join(it.Labels, labelSep), rfc(it.CompletedAt), rfc(it.StartedAt), it.Additions, it.Deletions, it.ChangedFiles, rfc(it.RequestedAt), it.Reviewer, rfc(it.ReviewedAt),
	)
	return err
}

// ListExternalItems returns a source's items, newest UpdatedAt first. source ""
// returns every source.
func (s *Store) ListExternalItems(source string) ([]ExternalItem, error) {
	rows, err := s.db.Query(
		`SELECT source,external_id,kind,title,state,url,repo,review_requested_of_me,assigned_to_me,updated_at,estimate,stage,project,labels,completed_at,started_at,additions,deletions,changed_files,requested_at,reviewer,reviewed_at
		 FROM external_items WHERE ?='' OR source=? ORDER BY updated_at DESC`, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalItem
	for rows.Next() {
		var (
			it                                                       ExternalItem
			rr, am                                                   int
			updated, labels, completed, started, requested, reviewed string
		)
		if err := rows.Scan(&it.Source, &it.ExternalID, &it.Kind, &it.Title, &it.State, &it.URL, &it.Repo, &rr, &am, &updated, &it.Estimate, &it.Stage, &it.Project, &labels, &completed, &started, &it.Additions, &it.Deletions, &it.ChangedFiles, &requested, &it.Reviewer, &reviewed); err != nil {
			return nil, err
		}
		it.Labels = splitLabels(labels)
		it.CompletedAt = parse(completed)
		it.StartedAt = parse(started)
		it.RequestedAt = parse(requested)
		it.ReviewedAt = parse(reviewed)
		it.ReviewRequestedOfMe = rr != 0
		it.AssignedToMe = am != 0
		it.UpdatedAt = parse(updated)
		out = append(out, it)
	}
	return out, rows.Err()
}

// Plan is an intention for a window of time.
type Plan struct {
	ID          string
	Name        string
	WindowStart time.Time
	WindowEnd   time.Time
	CreatedAt   time.Time
	Status      string // "active" | "past"
}

// PlanRow is one user-named row of a plan's timeline ("Work plan", ...).
type PlanRow struct {
	ID     string
	PlanID string
	Name   string
	Order  int
}

// PlanItem is one block in a plan: a referenced external item, or the user's
// own activity when it has no ref. It sits on RowID from Start for
// IntendedHours; a nil Start means "anytime this day", with no row.
type PlanItem struct {
	ID            string
	PlanID        string
	RowID         string
	RefSource     string // e.g. "github" when it references an external item
	RefID         string
	Text          string
	Start         *time.Time
	IntendedHours float64
	DoneAt        *time.Time // when an own activity or review was marked done; nil when not
}

// CreatePlan inserts a plan with the default rows.
func (s *Store) CreatePlan(p Plan) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO plans(id,name,window_start,window_end,created_at,status) VALUES(?,?,?,?,?,?)`,
		p.ID, p.Name, rfc(p.WindowStart), rfc(p.WindowEnd), rfc(p.CreatedAt), p.Status,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(addDefaultRows); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdatePlan replaces a plan's name and window by id.
func (s *Store) UpdatePlan(p Plan) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old string
	if err := tx.QueryRow(`SELECT window_start FROM plans WHERE id=?`, p.ID).Scan(&old); err != nil {
		return err
	}
	// Moving the plan to another day takes its placed blocks along, here in one
	// transaction rather than one request per block from the browser.
	if days := dayDelta(parse(old), p.WindowStart); days != 0 {
		rows, err := tx.Query(`SELECT id,start_at FROM plan_items WHERE plan_id=? AND start_at<>''`, p.ID)
		if err != nil {
			return err
		}
		moved := map[string]string{}
		for rows.Next() {
			var id, start string
			if err := rows.Scan(&id, &start); err != nil {
				rows.Close()
				return err
			}
			moved[id] = rfc(parse(start).Local().AddDate(0, 0, days))
		}
		rows.Close()
		for id, start := range moved {
			if _, err := tx.Exec(`UPDATE plan_items SET start_at=? WHERE id=?`, start, id); err != nil {
				return err
			}
		}
	}
	// An edited plan is active again, even if the archiver had moved it to past.
	if _, err := tx.Exec(
		`UPDATE plans SET name=?, window_start=?, window_end=?, status='active' WHERE id=?`,
		p.Name, rfc(p.WindowStart), rfc(p.WindowEnd), p.ID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// dayDelta is the number of local calendar days from a to b.
func dayDelta(a, b time.Time) int {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return int(time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC).Sub(time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)).Hours() / 24)
}

// ListPlans returns plans, earliest WindowStart first. status "" returns all.
func (s *Store) ListPlans(status string) ([]Plan, error) {
	q := `SELECT id,name,window_start,window_end,created_at,status FROM plans`
	var args []any
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY window_start ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		var (
			p                 Plan
			ws, we, createdAt string
		)
		if err := rows.Scan(&p.ID, &p.Name, &ws, &we, &createdAt, &p.Status); err != nil {
			return nil, err
		}
		p.WindowStart, p.WindowEnd, p.CreatedAt = parse(ws), parse(we), parse(createdAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlanExists reports whether a plan id exists.
func (s *Store) PlanExists(id string) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM plans WHERE id=?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// GetPlanRows returns a plan's rows ordered by Order.
func (s *Store) GetPlanRows(planID string) ([]PlanRow, error) {
	rows, err := s.db.Query(`SELECT id,plan_id,name,row_order FROM plan_rows WHERE plan_id=? ORDER BY row_order ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanRow
	for rows.Next() {
		var r PlanRow
		if err := rows.Scan(&r.ID, &r.PlanID, &r.Name, &r.Order); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddPlanRow inserts a row.
func (s *Store) AddPlanRow(r PlanRow) error {
	_, err := s.db.Exec(`INSERT INTO plan_rows(id,plan_id,name,row_order) VALUES(?,?,?,?)`, r.ID, r.PlanID, r.Name, r.Order)
	return err
}

// RenamePlanRow sets a row's name.
func (s *Store) RenamePlanRow(id, name string) error {
	_, err := s.db.Exec(`UPDATE plan_rows SET name=? WHERE id=?`, name, id)
	return err
}

// DeletePlanRow removes a row and the items on it.
func (s *Store) DeletePlanRow(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM plan_items WHERE row_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM plan_rows WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const planItemCols = `id,plan_id,row_id,ref_source,ref_id,text,start_at,intended_hours,done_at`

// scanPlanItem reads one planItemCols row.
func scanPlanItem(sc interface{ Scan(...any) error }) (PlanItem, error) {
	var (
		it          PlanItem
		start, done string
	)
	if err := sc.Scan(&it.ID, &it.PlanID, &it.RowID, &it.RefSource, &it.RefID, &it.Text, &start, &it.IntendedHours, &done); err != nil {
		return it, err
	}
	if start != "" {
		t := parse(start)
		it.Start = &t
	}
	if done != "" {
		t := parse(done)
		it.DoneAt = &t
	}
	return it, nil
}

// GetPlanItems returns a plan's items in the order they were added.
func (s *Store) GetPlanItems(planID string) ([]PlanItem, error) {
	rows, err := s.db.Query(`SELECT `+planItemCols+` FROM plan_items WHERE plan_id=? ORDER BY rowid ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanItem
	for rows.Next() {
		it, err := scanPlanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetPlanItem returns one plan item; the bool is false when it is absent.
func (s *Store) GetPlanItem(id string) (PlanItem, bool, error) {
	it, err := scanPlanItem(s.db.QueryRow(`SELECT `+planItemCols+` FROM plan_items WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return PlanItem{}, false, nil
	}
	return it, err == nil, err
}

// SetPlanItemDone marks an item done at doneAt, or not done when it is nil.
func (s *Store) SetPlanItemDone(id string, doneAt *time.Time) error {
	_, err := s.db.Exec(`UPDATE plan_items SET done_at=? WHERE id=?`, rfcPtr(doneAt), id)
	return err
}

// AddPlanItem inserts a plan item.
func (s *Store) AddPlanItem(it PlanItem) error {
	_, err := s.db.Exec(
		`INSERT INTO plan_items(id,plan_id,row_id,ref_source,ref_id,text,start_at,intended_hours)
		 VALUES(?,?,?,?,?,?,?,?)`,
		it.ID, it.PlanID, it.RowID, it.RefSource, it.RefID, it.Text, rfcPtr(it.Start), it.IntendedHours,
	)
	return err
}

// UpdatePlanItem replaces a plan item's placement, length and text by id.
func (s *Store) UpdatePlanItem(it PlanItem) error {
	_, err := s.db.Exec(
		`UPDATE plan_items SET row_id=?, start_at=?, intended_hours=?, text=? WHERE id=?`,
		it.RowID, rfcPtr(it.Start), it.IntendedHours, it.Text, it.ID,
	)
	return err
}

// DeletePlanItem removes one item.
func (s *Store) DeletePlanItem(id string) error {
	_, err := s.db.Exec(`DELETE FROM plan_items WHERE id=?`, id)
	return err
}

// DeletePlan removes a plan, its rows and its items.
func (s *Store) DeletePlan(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM plan_items WHERE plan_id=?`, `DELETE FROM plan_rows WHERE plan_id=?`, `DELETE FROM plans WHERE id=?`} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ArchivePastPlans flips active plans whose window ended before the start of
// now's day (in now's location) to "past" (kept, not deleted), so a plan stays
// for the whole of its day. Returns how many it archived.
func (s *Store) ArchivePastPlans(now time.Time) (int, error) {
	y, m, d := now.Date()
	res, err := s.db.Exec(
		`UPDATE plans SET status='past' WHERE status='active' AND window_end<>'' AND window_end<?`,
		rfc(time.Date(y, m, d, 0, 0, 0, 0, now.Location())),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetSetting upserts a local setting (e.g. a connector credential). Values are
// stored on this machine only and never logged or returned by an API.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

// MergeContinued folds a session Claude Code continued under a new id into
// that new session, in one transaction. The new session's transcript holds a
// copy of the old history, so what was read from the old transcript - the
// session row, its facts, day rows, sub-agents and summaries - is dropped rather
// than added. The user's name for it, and the app it ran in, move to the new
// session unless that has its own. The pair is kept, so the old session's
// sub-agent transcripts are counted toward the new one (see ContinuedAs).
func (s *Store) MergeContinued(oldID, newID string) error {
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`INSERT INTO continued_sessions(old_id,new_id) VALUES(?1,?2) ON CONFLICT(old_id) DO UPDATE SET new_id=excluded.new_id`,
		`INSERT OR IGNORE INTO session_names(session_id,name) SELECT ?2, name FROM session_names WHERE session_id=?1`,
		`INSERT OR IGNORE INTO session_hosts(session_id,host) SELECT ?2, host FROM session_hosts WHERE session_id=?1`,
		`DELETE FROM session_names WHERE session_id=?1`,
		`DELETE FROM session_hosts WHERE session_id=?1`,
		`DELETE FROM sessions WHERE id=?1`,
		`DELETE FROM facts WHERE session_id=?1`,
		`DELETE FROM session_days WHERE session_id=?1`,
		`DELETE FROM subagents WHERE session_id=?1`,
		// Its day points and details too, keyed "<id>/<day>".
		`DELETE FROM summaries WHERE subject_id=?1 OR substr(subject_id, 1, length(?1)+1) = ?1 || '/'`,
	} {
		if _, err := tx.Exec(q, oldID, newID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ContinuedAs returns the session a session id was continued in, following a
// chain of continuations to its end, or id itself when it was not continued.
func (s *Store) ContinuedAs(id string) (string, error) {
	seen := map[string]bool{id: true}
	for {
		var next string
		err := s.db.QueryRow(`SELECT new_id FROM continued_sessions WHERE old_id=?`, id).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) || seen[next] {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		seen[next] = true
		id = next
	}
}

// SetSessionName stores the name the user gave a session, apart from the
// sessions table so ingest never overwrites it. An empty name clears it.
func (s *Store) SetSessionName(id, name string) error {
	if name == "" {
		_, err := s.db.Exec(`DELETE FROM session_names WHERE session_id=?`, id)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO session_names(session_id,name) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET name=excluded.name`,
		id, name,
	)
	return err
}

// SessionNames returns the user's session names by session id.
func (s *Store) SessionNames() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT session_id, name FROM session_names`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// SetSessionHost stores the app a session was last seen running in, apart
// from the sessions table so ingest never overwrites it.
func (s *Store) SetSessionHost(id, host string) error {
	_, err := s.db.Exec(
		`INSERT INTO session_hosts(session_id,host) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET host=excluded.host`,
		id, host,
	)
	return err
}

// SessionHosts returns the last seen app of each session by session id.
func (s *Store) SessionHosts() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT session_id, host FROM session_hosts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, host string
		if err := rows.Scan(&id, &host); err != nil {
			return nil, err
		}
		out[id] = host
	}
	return out, rows.Err()
}

// GetSetting returns a setting's value; ok is false when it is absent.
func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func rfc(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parse(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func rfcPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return rfc(*t)
}
