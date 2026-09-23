package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// Decode on top of the defaults so a key that is absent from the stored blob
	// (a setting added by a newer build) keeps its default instead of silently
	// becoming the zero value — a bool would otherwise read as "off".
	set := DefaultSettings()
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
	if set.DedupEnabled && set.DedupWindowSec <= 0 {
		set.DedupWindowSec = 60
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

// LogFilter describes filtering and pagination for a log query. Zero-valued
// fields mean "no constraint" (except Limit/Offset which are normalized).
//
// The three text conditions differ on purpose, matching how the console offers
// them: Model comes from a dropdown of values that exist in the log, so it is an
// exact match (a substring would also drag in every longer model sharing the
// prefix); IP is typed and matches by prefix, so both a full address and a
// "194.26." style fragment work; Path is typed and matches as a substring, so a
// leading fragment finds the endpoints under it.
type LogFilter struct {
	Limit    int    // page size; normalized to [1,1000], default 50
	Offset   int    // records to skip; negatives treated as 0
	Decision string // exact decision match: allow|block|skip|error; "" = any
	Model    string // exact match on model; "" = any
	Path     string // substring match on path; "" = any
	IP       string // prefix match on ip ("194.26.*" and "127.0.0.1" both work); "" = any
	Query    string // keyword substring across path/model/ip/reason/snippet; "" = any
	Since    int64  // unix ms lower bound (ts >= Since); <=0 = no bound
}

// QueryLogs returns a page of log entries matching filter (newest first) plus
// the total number of matching entries ignoring Limit/Offset. All user-supplied
// values are bound as parameters to avoid SQL injection.
func (s *Store) QueryLogs(f LogFilter) ([]LogEntry, int64, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// Shared WHERE clause + args for both the count and the page query.
	where := ""
	args := []interface{}{}
	add := func(cond string, val interface{}) {
		if where == "" {
			where = " WHERE "
		} else {
			where += " AND "
		}
		where += cond
		args = append(args, val)
	}
	if f.Decision != "" {
		add("decision=?", f.Decision)
	}
	if f.Model != "" {
		add("model=?", f.Model)
	}
	if f.Path != "" {
		add("path LIKE ? ESCAPE '\\'", "%"+likeEscape(f.Path)+"%")
	}
	if f.IP != "" {
		if prefix := ipPrefix(f.IP); prefix != "" {
			add("ip LIKE ? ESCAPE '\\'", likeEscape(prefix)+"%")
		}
	}
	if f.Query != "" {
		kw := "%" + likeEscape(f.Query) + "%"
		add("(path LIKE ? ESCAPE '\\' OR model LIKE ? ESCAPE '\\' OR ip LIKE ? ESCAPE '\\' OR reason LIKE ? ESCAPE '\\' OR snippet LIKE ? ESCAPE '\\')", kw)
		// add() only appends one arg; append the remaining four for the OR group.
		args = append(args, kw, kw, kw, kw)
	}
	if f.Since > 0 {
		add("ts>=?", f.Since)
	}

	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	pageArgs := append(append([]interface{}{}, args...), limit, offset)
	rows, err := s.db.Query(
		`SELECT id,ts,method,path,kind,decision,score,model,latency_ms,ip,reason,snippet FROM logs`+
			where+` ORDER BY id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []LogEntry{}
	for rows.Next() {
		var e LogEntry
		var ts int64
		var score sql.NullFloat64
		if err := rows.Scan(&e.ID, &ts, &e.Method, &e.Path, &e.Kind, &e.Decision,
			&score, &e.Model, &e.LatencyMS, &e.IP, &e.Reason, &e.Snippet); err != nil {
			return nil, 0, err
		}
		e.TS = time.UnixMilli(ts)
		if score.Valid {
			v := score.Float64
			e.Score = &v
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// modelChoicesMax bounds the model dropdown: enough to be useful, small enough
// that the response and the rendered option list stay bounded.
const modelChoicesMax = 200

// LogModels returns the model values present in the log with their occurrence
// counts, most frequent first, for building the console's model dropdown. Only
// the time window applies — the choice list is deliberately not narrowed by the
// other active filters, because selecting one model would then hide every other
// choice and the operator could not switch. Records with no model (a skip, or a
// request rejected before scoring) contribute no choice rather than an empty one.
func (s *Store) LogModels(since int64) ([]ModelCount, error) {
	where := " WHERE model<>''"
	args := []interface{}{}
	if since > 0 {
		where += " AND ts>=?"
		args = append(args, since)
	}
	args = append(args, modelChoicesMax)

	rows, err := s.db.Query(
		`SELECT model, COUNT(*) FROM logs`+where+
			` GROUP BY model ORDER BY COUNT(*) DESC, model ASC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelCount{}
	for rows.Next() {
		var m ModelCount
		if err := rows.Scan(&m.Value, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// likeEscape escapes SQL LIKE wildcards so user keywords match literally.
// Pairs with `ESCAPE '\'` in the query.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ipPrefix turns a typed IP filter into a LIKE prefix. A trailing "*" is the
// wildcard the console advertises ("194.26.*"), so it is dropped; the rest is
// matched as a literal prefix, which means a complete address still matches only
// itself. An empty result carries no constraint, so the caller skips the clause.
func ipPrefix(s string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "*"))
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

// BucketSeconds derives the trend-series bucket size from the requested window.
// It is a pure function of hours, so the granularity is reproducible and is
// returned to clients rather than guessed by them. The steps keep the point
// count of a series bounded to roughly 100 regardless of the window.
func BucketSeconds(hours int) int {
	switch {
	case hours <= 6:
		return 5 * 60 // 5m
	case hours <= 24:
		return 15 * 60 // 15m
	case hours <= 72:
		return 60 * 60 // 1h
	case hours <= 168:
		return 3 * 60 * 60 // 3h
	default:
		return 24 * 60 * 60 // 1d
	}
}

// StatsSeries returns one StatBucket per bucket spanning [since, until], oldest
// first. Buckets are aligned to the bucket boundary (so consecutive queries
// agree on bucket edges) and those without matching rows are emitted with zero
// counts, which keeps the returned series continuous: callers can plot it
// directly without filling gaps themselves.
//
// The first bucket starts at the boundary at or before `since`, so when `since`
// is not itself on a boundary that leading bucket covers a fraction of a bucket
// before the window. The rows queried are still bounded by `since`, so the sum
// over the series always equals the decision totals of the same window.
func (s *Store) StatsSeries(since, until time.Time, bucketSeconds int) ([]StatBucket, error) {
	if bucketSeconds <= 0 {
		bucketSeconds = BucketSeconds(24)
	}
	step := int64(bucketSeconds) * 1000
	sinceMS := since.UnixMilli()
	untilMS := until.UnixMilli()
	if untilMS < sinceMS {
		return []StatBucket{}, nil
	}
	start := sinceMS - sinceMS%step // floor to the bucket boundary (ms are positive)
	count := int((untilMS-start)/step) + 1

	series := make([]StatBucket, count)
	for i := range series {
		series[i].TS = start + int64(i)*step
	}

	rows, err := s.db.Query(
		`SELECT ts, decision FROM logs WHERE ts>=? AND ts<=?`,
		sinceMS, untilMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		var decision string
		if err := rows.Scan(&ts, &decision); err != nil {
			return nil, err
		}
		idx := int((ts - start) / step)
		if idx < 0 || idx >= len(series) {
			continue
		}
		b := &series[idx]
		b.Total++
		switch decision {
		case "allow":
			b.Allowed++
		case "block":
			b.Blocked++
		}
	}
	return series, rows.Err()
}

// ScoreHistogram buckets the risk score of every evaluated record in the window
// (decision allow/block/error — "skip" records were never sent to JEV) into
// ScoreSlots equal slots of 0.2, plus a count of evaluated records carrying no
// score at all. Counting evaluated rather than blocked-only records matters:
// a block is just "score on one side of the threshold", so a blocked-only
// histogram collapses into a single slot and shows nothing.
func (s *Store) ScoreHistogram(since time.Time) (ScoreHistogram, error) {
	h := ScoreHistogram{Counts: make([]int64, ScoreSlots)}
	rows, err := s.db.Query(
		`SELECT CASE
			WHEN score IS NULL THEN -1
			WHEN score < 0.2 THEN 0
			WHEN score < 0.4 THEN 1
			WHEN score < 0.6 THEN 2
			WHEN score < 0.8 THEN 3
			ELSE 4
		 END AS slot, COUNT(*)
		 FROM logs WHERE ts>=? AND decision IN ('allow','block','error') GROUP BY slot`,
		since.UnixMilli())
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var slot int
		var n int64
		if err := rows.Scan(&slot, &n); err != nil {
			return h, err
		}
		if slot < 0 || slot >= ScoreSlots {
			h.Unscored += n
			continue
		}
		h.Counts[slot] = n
	}
	return h, rows.Err()
}
