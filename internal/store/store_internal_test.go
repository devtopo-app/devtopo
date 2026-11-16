package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAddsTailColumnToExistingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	// Simulate a capture-core database: a sessions table with no tail column.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (
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
		status            TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	st, err := Open(path) // must add the missing tail column
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Apply(Delta{
		Project:    Project{Key: "k", Name: "n"},
		Session:    Session{ID: "s1", ProjectKey: "k", Tail: "ai_working"},
		CursorPath: "p", CursorOffset: 1,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.GetSession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Tail != "ai_working" {
		t.Fatalf("tail after migration: %+v ok=%v", got, ok)
	}

	// Opening again must tolerate the tail column already being present.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open must be idempotent: %v", err)
	}
	st2.Close()
}

func TestOpenAddsTranscriptPathColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	// A pre-transcript_path sessions table (has tail, lacks transcript_path).
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE sessions (
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
		tail              TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	st, err := Open(path) // must add the missing transcript_path column
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rows, err := st.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "transcript_path" {
			has = true
		}
	}
	if !has {
		t.Fatal("Open must add the transcript_path column to an existing sessions table")
	}
}

func TestOpenEnablesWALAndBusyTimeout(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var jm string
	if err := st.db.QueryRow("PRAGMA journal_mode").Scan(&jm); err != nil {
		t.Fatal(err)
	}
	if jm != "wal" {
		t.Fatalf("journal_mode=%q, want wal", jm)
	}
	var bt int
	if err := st.db.QueryRow("PRAGMA busy_timeout").Scan(&bt); err != nil {
		t.Fatal(err)
	}
	if bt != 5000 {
		t.Fatalf("busy_timeout=%d, want 5000", bt)
	}
}

func TestOpenAddsEstimateToExistingExternalItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE external_items (
		source TEXT NOT NULL, external_id TEXT NOT NULL, kind TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', repo TEXT NOT NULL DEFAULT '',
		review_requested_of_me INTEGER NOT NULL DEFAULT 0, assigned_to_me INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(source, external_id))`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.UpsertExternalItem(ExternalItem{Source: "linear", ExternalID: "a", Estimate: 2, Additions: 5, Deletions: 6, ChangedFiles: 7}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ListExternalItems("linear"); len(got) != 1 || got[0].Estimate != 2 || got[0].Additions != 5 || got[0].Deletions != 6 || got[0].ChangedFiles != 7 {
		t.Fatalf("estimate and sizes after migration: %+v", got)
	}
}

func TestOpenMigratesExistingPlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	// A planner DB from before named plans and rows.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE plans (id TEXT PRIMARY KEY, window_start TEXT NOT NULL DEFAULT '', window_end TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active');
		CREATE TABLE plan_items (id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, category TEXT NOT NULL DEFAULT 'work',
			ref_source TEXT NOT NULL DEFAULT '', ref_id TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '', item_order INTEGER NOT NULL DEFAULT 0, intended_hours REAL NOT NULL DEFAULT 0);
		INSERT INTO plans(id,window_start,window_end,created_at,status) VALUES('p1','2026-10-28T12:00:00Z','2026-10-28T14:00:00Z','2026-10-28T08:00:00Z','active');
		INSERT INTO plan_items(id,plan_id,category,text,intended_hours) VALUES('i1','p1','work','investigate flaky test',1.5);
	`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	for range 2 { // the second Open must be a no-op
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		plans, _ := st.ListPlans("")
		if len(plans) != 1 || plans[0].Name != "Plan for Wed 28 Oct" {
			t.Fatalf("an existing plan is named after its date: %+v", plans)
		}
		if rows, _ := st.GetPlanRows("p1"); len(rows) != 2 || rows[0].Name != "Work plan" || rows[1].Name != "Private activities" {
			t.Fatalf("an existing plan gets the default rows: %+v", rows)
		}
		items, _ := st.GetPlanItems("p1")
		if len(items) != 1 || items[0].Start != nil || items[0].IntendedHours != 1.5 {
			t.Fatalf("an existing item becomes an anytime item: %+v", items)
		}
		st.Close()
	}
}

// DEV-22: the status the page polls every few seconds counts each session's
// facts and reads today's by type. Without indexes each count scanned the whole
// table, so with a long history one poll took half a minute and the page sat
// on "connecting to the daemon". The queries must use an index, also in a
// store made before the indexes existed.
func TestFactQueriesUseAnIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE facts (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, type TEXT NOT NULL, timestamp TEXT NOT NULL DEFAULT '', tool_name TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{
		`SELECT type, COUNT(*) FROM facts WHERE session_id='x' GROUP BY type`,
		`SELECT session_id,timestamp FROM facts WHERE type='prompt' AND timestamp>='2026-11-13'`,
		`DELETE FROM facts WHERE session_id='x'`,
		`SELECT session_id,ai_seconds FROM session_days WHERE day='2026-11-16'`,
	} {
		rows, err := s.db.Query(`EXPLAIN QUERY PLAN ` + q)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail + "; "
		}
		rows.Close()
		if !strings.Contains(plan, "INDEX") {
			t.Errorf("%s\n  scans the table: %s", q, plan)
		}
	}
}
