package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// Store is a thread-safe SQLite-backed configuration and log store.
type Store struct {
	db  *sql.DB
	mu  sync.RWMutex
	set Settings // cached settings
}

// Open opens (creating if needed) the SQLite database at path and initializes
// the schema and default rows.
func Open(path string) (*Store, error) {
	// Ensure the parent directory exists (sqlite won't create it).
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // modernc sqlite: serialize writes, avoids lock churn
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS jev_keys (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			label      TEXT NOT NULL DEFAULT '',
			key        TEXT NOT NULL,
			enabled    INTEGER NOT NULL DEFAULT 1,
			calls      INTEGER NOT NULL DEFAULT 0,
			last_used  INTEGER,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			ts         INTEGER NOT NULL,
			method     TEXT NOT NULL,
			path       TEXT NOT NULL,
			kind       TEXT NOT NULL,
			decision   TEXT NOT NULL,
			score      REAL,
			model      TEXT NOT NULL DEFAULT '',
			latency_ms INTEGER NOT NULL DEFAULT 0,
			ip         TEXT NOT NULL DEFAULT '',
			reason     TEXT NOT NULL DEFAULT '',
			snippet    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(ts DESC)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	// Seed default settings if absent.
	if _, err := s.getKV("settings"); errors.Is(err, sql.ErrNoRows) {
		if err := s.saveSettings(DefaultSettings()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) load() error {
	raw, err := s.getKV("settings")
	if err != nil {
		return err
	}
	var set Settings
	if err := json.Unmarshal([]byte(raw), &set); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}
	s.mu.Lock()
	s.set = set
	s.mu.Unlock()
	return nil
}

// --- kv helpers ---

func (s *Store) getKV(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE key=?`, key).Scan(&v)
	return v, err
}

func (s *Store) setKV(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO kv(key,value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// --- settings ---

// Settings returns a copy of the cached settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set
}

func (s *Store) saveSettings(set Settings) error {
	b, err := json.Marshal(set)
	if err != nil {
		return err
	}
	if err := s.setKV("settings", string(b)); err != nil {
		return err
	}
	s.mu.Lock()
	s.set = set
	s.mu.Unlock()
	return nil
}

// UpdateSettings persists new settings after basic validation.
func (s *Store) UpdateSettings(set Settings) error {
	if set.SafetyThreshold < 0 || set.SafetyThreshold > 1 {
		return errors.New("safety_threshold must be between 0 and 1")
	}
	if set.MaxStateChars <= 0 {
		set.MaxStateChars = 16000
	}
	if set.JEVTimeoutMS <= 0 {
		set.JEVTimeoutMS = 8000
	}
	if set.JEVModel == "" {
		set.JEVModel = "jev-latest"
	}
	if set.AbuseEnabled {
		if set.AbuseWindowSec <= 0 {
			set.AbuseWindowSec = 60
		}
		if set.AbuseMaxHarmful <= 0 {
			set.AbuseMaxHarmful = 5
		}
		if set.AbuseBanSec <= 0 {
			set.AbuseBanSec = 300
		}
	}
	return s.saveSettings(set)
}

// --- admin password ---

// AdminHash returns the stored bcrypt hash, or "" if unset.
func (s *Store) AdminHash() string {
	v, err := s.getKV("admin_hash")
	if err != nil {
		return ""
	}
	return v
}

// SetAdminHash stores the bcrypt hash of the admin password.
func (s *Store) SetAdminHash(hash string) error { return s.setKV("admin_hash", hash) }

// SetAdminPasswordPlain hashes and stores a plaintext admin password.
func (s *Store) SetAdminPasswordPlain(pw string) error {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.setKV("admin_hash", string(b))
}

// --- JEV keys ---

func maskKey(k string) string {
	if len(k) <= 12 {
		return "****"
	}
	return k[:9] + "…" + k[len(k)-6:]
}

// ListKeys returns all JEV keys (values masked).
func (s *Store) ListKeys(reveal bool) ([]JEVKey, error) {
	rows, err := s.db.Query(
		`SELECT id,label,key,enabled,calls,last_used,created_at FROM jev_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JEVKey
	for rows.Next() {
		var k JEVKey
		var enabled int
		var lastUsed sql.NullInt64
		var created int64
		if err := rows.Scan(&k.ID, &k.Label, &k.Key, &enabled, &k.Calls, &lastUsed, &created); err != nil {
			return nil, err
		}
		k.Enabled = enabled == 1
		k.Masked = maskKey(k.Key)
		if !reveal {
			k.Key = ""
		}
		if lastUsed.Valid {
			t := time.UnixMilli(lastUsed.Int64)
			k.LastUsed = &t
		}
		k.CreatedAt = time.UnixMilli(created)
		out = append(out, k)
	}
	return out, rows.Err()
}

// EnabledKeys returns the raw values of enabled keys, in id order.
func (s *Store) EnabledKeys() ([]JEVKey, error) {
	rows, err := s.db.Query(
		`SELECT id,key FROM jev_keys WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JEVKey
	for rows.Next() {
		var k JEVKey
		if err := rows.Scan(&k.ID, &k.Key); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AddKey inserts a new JEV key.
func (s *Store) AddKey(label, key string) (int64, error) {
	if key == "" {
		return 0, errors.New("key is required")
	}
	res, err := s.db.Exec(
		`INSERT INTO jev_keys(label,key,enabled,created_at) VALUES(?,?,1,?)`,
		label, key, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetKeyEnabled toggles a key.
func (s *Store) SetKeyEnabled(id int64, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	_, err := s.db.Exec(`UPDATE jev_keys SET enabled=? WHERE id=?`, e, id)
	return err
}

// DeleteKey removes a key.
func (s *Store) DeleteKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM jev_keys WHERE id=?`, id)
	return err
}

// MarkKeyUsed increments the call counter for a key.
func (s *Store) MarkKeyUsed(id int64) {
	_, _ = s.db.Exec(`UPDATE jev_keys SET calls=calls+1,last_used=? WHERE id=?`,
		time.Now().UnixMilli(), id)
}

// --- logs ---

// AddLog appends a request outcome. Failures are ignored (logging is best-effort).
func (s *Store) AddLog(e LogEntry) {
	_, _ = s.db.Exec(
		`INSERT INTO logs(ts,method,path,kind,decision,score,model,latency_ms,ip,reason,snippet)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		e.TS.UnixMilli(), e.Method, e.Path, e.Kind, e.Decision, nullFloat(e.Score),
		e.Model, e.LatencyMS, e.IP, e.Reason, e.Snippet)
}

func nullFloat(f *float64) interface{} {
	if f == nil {
		return nil
	}
	return *f
}

// RecentLogs returns up to limit recent log entries, optionally filtered by decision.
func (s *Store) RecentLogs(limit int, decision string) ([]LogEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT id,ts,method,path,kind,decision,score,model,latency_ms,ip,reason,snippet FROM logs`
	args := []interface{}{}
	if decision != "" {
		q += ` WHERE decision=?`
		args = append(args, decision)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts int64
		var score sql.NullFloat64
		if err := rows.Scan(&e.ID, &ts, &e.Method, &e.Path, &e.Kind, &e.Decision,
			&score, &e.Model, &e.LatencyMS, &e.IP, &e.Reason, &e.Snippet); err != nil {
			return nil, err
		}
		e.TS = time.UnixMilli(ts)
		if score.Valid {
			v := score.Float64
			e.Score = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// StatsSince aggregates decisions since the given time.
func (s *Store) StatsSince(since time.Time) (Stats, error) {
	var st Stats
	rows, err := s.db.Query(
		`SELECT decision, COUNT(*) FROM logs WHERE ts>=? GROUP BY decision`,
		since.UnixMilli())
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			return st, err
		}
		st.Total += n
		switch d {
		case "allow":
			st.Allowed = n
		case "block":
			st.Blocked = n
		case "skip":
			st.Skipped = n
		case "error":
			st.Errors = n
		}
	}
	return st, rows.Err()
}
