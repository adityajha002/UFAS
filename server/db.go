package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const driverName = "sqlite"

const schema = `
CREATE TABLE IF NOT EXISTS faculty (
  id           TEXT PRIMARY KEY,
  employee_id  TEXT NOT NULL UNIQUE,
  name         TEXT NOT NULL,
  public_key   BLOB,
  code_hash    BLOB,
  code_expires INTEGER
);
CREATE TABLE IF NOT EXISTS challenges (
  id         TEXT PRIMARY KEY,
  faculty_id TEXT NOT NULL,
  nonce      TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS attendance (
  faculty_id  TEXT NOT NULL,
  day         TEXT NOT NULL,
  marked_at   INTEGER NOT NULL,
  lat_micro   INTEGER,
  lon_micro   INTEGER,
  accuracy_cm INTEGER,
  distance_m  INTEGER,
  note        TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (faculty_id, day)
);
CREATE TABLE IF NOT EXISTS attendance_state (
  faculty_id TEXT NOT NULL,
  day TEXT NOT NULL,
  is_in INTEGER NOT NULL DEFAULT 0,
  started_at INTEGER,
  today_worked_s INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (faculty_id, day)
);
CREATE TABLE IF NOT EXISTS exit_requests (
  id              TEXT PRIMARY KEY,
  faculty_id      TEXT NOT NULL,
  day             TEXT NOT NULL,
  requested_at    INTEGER NOT NULL,
  lat_micro       INTEGER NOT NULL,
  lon_micro       INTEGER NOT NULL,
  accuracy_cm     INTEGER NOT NULL,
  distance_m      INTEGER NOT NULL,
  challenge_id    TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'pending',
  approved_at     INTEGER,
  approved_by     TEXT,
  attendance_day  TEXT
);
CREATE INDEX IF NOT EXISTS idx_exit_requests_faculty_day ON exit_requests(faculty_id, day);
CREATE INDEX IF NOT EXISTS idx_exit_requests_pending ON exit_requests(status, requested_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_one_pending_exit_request
ON exit_requests(faculty_id, day)
WHERE status = 'pending';
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  endpoint TEXT NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  status INTEGER NOT NULL,
  outcome TEXT NOT NULL,
  faculty_id TEXT,
  challenge_id TEXT,
  remote_ip TEXT,
  user_agent TEXT,
  lat_micro INTEGER,
  lon_micro INTEGER,
  accuracy_cm INTEGER,
  distance_m INTEGER,
  policy TEXT
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS config_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  config_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log(ts);
CREATE INDEX IF NOT EXISTS idx_audit_log_faculty ON audit_log(faculty_id, ts);
CREATE INDEX IF NOT EXISTS idx_audit_log_endpoint ON audit_log(endpoint, ts);
CREATE VIEW IF NOT EXISTS attendance_report AS
  SELECT f.employee_id, f.name, a.day,
         datetime(a.marked_at, 'unixepoch', '+5 hours', '+30 minutes') AS marked_at_ist,
         a.distance_m, a.note
  FROM attendance a JOIN faculty f ON f.id = a.faculty_id;
`

var errNotFound = errors.New("employee not found")

// openDB opens the SQLite file with a single connection: writes are serialised,
// which is what makes the "consume once" steps atomic.
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	for _, stmt := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		schema,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("init db: %w", err)
		}
	}
	return db, nil
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---- admin operations (used by the CLI and by tests) ----

func addFaculty(db *sql.DB, employeeID, name string) (string, error) {
	id := randHex(16)
	_, err := db.Exec(`INSERT INTO faculty (id, employee_id, name) VALUES (?, ?, ?)`, id, employeeID, name)
	return id, err
}

// issueCode creates a one-time enrollment code valid for 15 minutes.
func issueCode(db *sql.DB, employeeID string, now time.Time) (string, error) {
	code := randHex(10) // 80 bits
	res, err := db.Exec(`UPDATE faculty SET code_hash=?, code_expires=? WHERE employee_id=?`,
		hashCode(code), now.Add(15*time.Minute).Unix(), employeeID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", errNotFound
	}
	return code, nil
}

func revokeFaculty(db *sql.DB, employeeID string) error {
	res, err := db.Exec(`UPDATE faculty SET public_key=NULL, code_hash=NULL, code_expires=NULL WHERE employee_id=?`, employeeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errNotFound
	}
	return nil
}

func deleteFaculty(db *sql.DB, employeeID string) error {
	return withTx(context.Background(), db, func(tx *sql.Tx) error {
		var facultyID string
		if err := tx.QueryRow(`SELECT id FROM faculty WHERE employee_id=?`, employeeID).Scan(&facultyID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if _, err := tx.Exec(`DELETE FROM attendance WHERE faculty_id=?`, facultyID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM challenges WHERE faculty_id=?`, facultyID); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM faculty WHERE id=?`, facultyID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errNotFound
		}
		return nil
	})
}

// markManual records attendance without a phone (lat/lon/accuracy/distance stay NULL).
func markManual(db *sql.DB, employeeID, day, note string, now time.Time) error {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	res, err := db.Exec(`INSERT INTO attendance (faculty_id, day, marked_at, note)
		SELECT id, ?, ?, ? FROM faculty WHERE employee_id=?
		ON CONFLICT(faculty_id, day) DO NOTHING`, day, now.Unix(), note, employeeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var one int
	if err := db.QueryRow(`SELECT 1 FROM faculty WHERE employee_id=?`, employeeID).Scan(&one); err != nil {
		return errNotFound
	}
	return fmt.Errorf("%s already has attendance on %s", employeeID, day)
}
