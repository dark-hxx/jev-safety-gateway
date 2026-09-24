// Package admin serves the configuration API and the embedded web UI.
// Everything under /api/ requires a bearer token obtained from POST /api/login.
package admin

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"jev-safety-gateway/internal/config"
)

// Info is the console's self-description, served by GET /api/version.
//
// The addresses are filled in by internal/server once the listeners are
// actually bound, which is why this is handed around as a pointer: the console
// should report the endpoints that came up, not the strings that were
// configured.
type Info struct {
	Version string
	Commit  string
	BuiltAt string
	Go      string

	ProxyAddr string
	AdminAddr string
}

// BanSnapshot is the read-only view of the live temporary-ban state the IP
// analytics page needs. *abuse.Tracker satisfies it, so admin can report current
// bans without importing (or coupling to) the abuse package. A nil BanSnapshot
// (as in tests, or if wiring is ever omitted) simply yields no bans.
type BanSnapshot interface {
	Snapshot(now time.Time) map[string]time.Time
}

// Handler is the admin HTTP handler (API + static UI).
type Handler struct {
	store *config.Store
	sess  *sessions
	ui    http.Handler
	mux   *http.ServeMux
	info  *Info
	bans  BanSnapshot
}

// New builds the admin handler. webFS should contain index.html at its root.
// info may be nil, in which case /api/version reports the running Go version
// and zero addresses rather than failing. bans may be nil (no live bans shown).
func New(store *config.Store, webFS fs.FS, info *Info, bans BanSnapshot) *Handler {
	h := &Handler{
		store: store,
		sess:  newSessions(12 * time.Hour),
		ui:    spaFileServer(webFS),
		info:  info,
		bans:  bans,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", h.login)
	mux.HandleFunc("/api/logout", h.auth(h.logout))
	mux.HandleFunc("/api/state", h.auth(h.getState))
	mux.HandleFunc("/api/settings", h.auth(h.settings))
	mux.HandleFunc("/api/keys", h.auth(h.keys))
	mux.HandleFunc("/api/keys/", h.auth(h.keyItem))
	mux.HandleFunc("/api/logs", h.auth(h.logs))
	mux.HandleFunc("/api/logs/models", h.auth(h.logModels))
	mux.HandleFunc("/api/stats", h.auth(h.stats))
	mux.HandleFunc("/api/stats/latency", h.auth(h.latency))
	mux.HandleFunc("/api/stats/ip", h.auth(h.ipStats))
	mux.HandleFunc("/api/setup-status", h.setupStatus)
	mux.HandleFunc("/api/setup", h.setup)
	// Deliberately not wrapped in h.auth: the login screen renders the build
	// identity and the daemon address before any token exists. Nothing here is
	// derived from the audit log — keep it that way, and keep latency (which is)
	// behind auth under /api/stats/latency.
	mux.HandleFunc("/api/version", h.version)
	mux.Handle("/", h.ui)
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// --- middleware ---

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.sess.valid(bearer(r)) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// --- version (public) ---

// version reports the build identity and the bound listen addresses. It is the
// one /api/ route that answers without a token, because the login screen shows
// the build badge and the daemon address before anyone has logged in. The
// payload is limited to facts the caller can already observe; anything derived
// from the audit log stays behind auth.
func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	info := Info{Version: "dev", Go: runtime.Version()}
	if h.info != nil {
		info = *h.info
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"version":    info.Version,
		"commit":     info.Commit,
		"built_at":   info.BuiltAt,
		"go":         info.Go,
		"proxy_addr": info.ProxyAddr,
		"admin_addr": info.AdminAddr,
	})
}

// --- setup / login ---

// setupStatus reports whether an admin password has been set yet.
func (h *Handler) setupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": h.store.AdminHash() == ""})
}

// setup sets the initial admin password (only allowed when none exists).
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	if h.store.AdminHash() != "" {
		writeJSON(w, http.StatusConflict, errBody("already initialized"))
		return
	}
	var in struct{ Password string `json:"password"` }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, errBody("password must be at least 6 characters"))
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody("hash failed"))
		return
	}
	if err := h.store.SetAdminHash(hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody("save failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": h.sess.create()})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
		return
	}
	var in struct{ Password string `json:"password"` }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad request"))
		return
	}
	hash := h.store.AdminHash()
	if hash == "" {
		writeJSON(w, http.StatusConflict, errBody("not initialized"))
		return
	}
	if !checkPassword(hash, in.Password) {
		writeJSON(w, http.StatusUnauthorized, errBody("invalid password"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": h.sess.create()})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	h.sess.revoke(bearer(r))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- state (settings + keys + stats snapshot) ---

func (h *Handler) getState(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListKeys(false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	st, _ := h.store.StatsSince(time.Now().Add(-24 * time.Hour))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"settings": h.store.Settings(),
		"keys":     keys,
		"stats24h": st,
	})
}

// --- settings ---

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.store.Settings())
	case http.MethodPut, http.MethodPost:
		var in config.Settings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("bad request"))
			return
		}
		if err := h.store.UpdateSettings(in); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, h.store.Settings())
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
	}
}

// --- keys ---

func (h *Handler) keys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := h.store.ListKeys(false)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, keys)
	case http.MethodPost:
		var in struct {
			Label string `json:"label"`
			Key   string `json:"key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody("bad request"))
			return
		}
		id, err := h.store.AddKey(strings.TrimSpace(in.Label), strings.TrimSpace(in.Key))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"id": id})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
	}
}

// keyItem handles /api/keys/{id} and /api/keys/{id}/enable|disable.
func (h *Handler) keyItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/keys/")
	parts := strings.Split(rest, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid id"))
		return
	}

	// Action sub-path: enable / disable.
	if len(parts) == 2 {
		switch parts[1] {
		case "enable":
			_ = h.store.SetKeyEnabled(id, true)
		case "disable":
			_ = h.store.SetKeyEnabled(id, false)
		default:
			writeJSON(w, http.StatusNotFound, errBody("unknown action"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if r.Method == http.MethodDelete {
		if err := h.store.DeleteKey(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, errBody("method not allowed"))
}

// --- logs & stats ---

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	entries, total, err := h.store.QueryLogs(config.LogFilter{
		Limit:    limit,
		Offset:   offset,
		Decision: q.Get("decision"),
		Model:    strings.TrimSpace(q.Get("model")),
		Path:     strings.TrimSpace(q.Get("path")),
		IP:       strings.TrimSpace(q.Get("ip")),
		Query:    strings.TrimSpace(q.Get("q")),
		Since:    since,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": entries, "total": total})
}

// logModels serves the model choices for the audit filter dropdown: the model
// values that actually occur in the log, with counts, most frequent first. Only
// the `since` window scopes the list — see config.Store.LogModels for why the
// other filters deliberately do not.
func (h *Handler) logModels(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	models, err := h.store.LogModels(since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": models})
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	hours := hoursParam(r)
	now := time.Now()
	since := now.Add(-time.Duration(hours) * time.Hour)
	st, err := h.store.StatsSince(since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	bucketSeconds := config.BucketSeconds(hours)
	series, err := h.store.StatsSeries(since, now, bucketSeconds)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	hist, err := h.store.ScoreHistogram(since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	// The existing aggregate fields keep their names and meaning; everything
	// below is additive, so older clients of this endpoint are unaffected.
	writeJSON(w, http.StatusOK, struct {
		config.Stats
		BucketSeconds int                 `json:"bucket_seconds"`
		Series        []config.StatBucket `json:"series"`
		ScoreBuckets  []int64             `json:"score_buckets"`
		Unscored      int64               `json:"unscored"`
	}{
		Stats:         st,
		BucketSeconds: bucketSeconds,
		Series:        series,
		ScoreBuckets:  hist.Counts,
		Unscored:      hist.Unscored,
	})
}

// latency reports the latency distribution of the window from the audit log.
// Unlike /api/version this one needs a token: percentiles over the request log
// leak traffic volume and timing to anyone who can reach the admin port.
func (h *Handler) latency(w http.ResponseWriter, r *http.Request) {
	st, err := h.store.LatencySince(time.Now().Add(-time.Duration(hoursParam(r)) * time.Hour))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// ipStats serves the IP risk analytics page: the per-IP aggregate of the window
// (most dangerous first), a window-wide summary, and the current live temporary
// bans read straight from the abuse tracker. Like /api/stats/latency it needs a
// token — it exposes per-client traffic volume and block history.
//
// The live bans come from the in-memory tracker, so they reflect this process
// only (single-instance by design) and reset on restart. When no tracker was
// wired (nil bans), the ban list is simply empty rather than an error.
func (h *Handler) ipStats(w http.ResponseWriter, r *http.Request) {
	hours := hoursParam(r)
	now := time.Now()
	since := now.Add(-time.Duration(hours) * time.Hour)

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, summary, err := h.store.IPStats(since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	bans := h.bannedList(now)

	writeJSON(w, http.StatusOK, struct {
		config.IPStatsSummary
		Hours    int             `json:"hours"`
		Items    []config.IPStat `json:"items"`
		Bans     []banEntry      `json:"bans"`
		BanCount int             `json:"ban_count"`
	}{
		IPStatsSummary: summary,
		Hours:          hours,
		Items:          items,
		Bans:           bans,
		BanCount:       len(bans),
	})
}

// banEntry is one live temporary ban as reported to the console.
type banEntry struct {
	IP        string `json:"ip"`
	Until     int64  `json:"until"`      // unix ms the ban expires
	RemainSec int64  `json:"remain_sec"` // seconds left, floored at 0
}

// bannedList renders the tracker's live bans, soonest-to-expire last, as of now.
// It always returns a non-nil slice so the JSON is [] rather than null, and a nil
// tracker yields an empty list rather than panicking.
func (h *Handler) bannedList(now time.Time) []banEntry {
	out := []banEntry{}
	if h.bans == nil {
		return out
	}
	for ip, until := range h.bans.Snapshot(now) {
		remain := until.Sub(now)
		if remain < 0 {
			remain = 0
		}
		out = append(out, banEntry{
			IP:        ip,
			Until:     until.UnixMilli(),
			RemainSec: int64(remain.Seconds()),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Until != out[j].Until {
			return out[i].Until > out[j].Until
		}
		return out[i].IP < out[j].IP
	})
	return out
}

// --- helpers ---

// maxWindowHours caps the window any stats endpoint will honour. A caller asking
// for a decade of history should not be able to turn one request into a scan of
// the whole log table.
const maxWindowHours = 720 // 30 days

// hoursParam reads the `hours` window selector shared by /api/stats and
// /api/stats/latency: default 24, clamped to [1, maxWindowHours].
func hoursParam(r *http.Request) int {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		return 24
	}
	if hours > maxWindowHours {
		return maxWindowHours
	}
	return hours
}

// spaFileServer serves the embedded console and falls back to index.html for
// GET/HEAD paths that name no real file. The console is a vue-router
// history-mode SPA, so deep links such as /audit or /settings must reach the app
// rather than a 404 from a plain file server.
//
// The fallback deliberately excludes /api/: an unknown API path is a real 404,
// not a page (the mux routes every registered /api/ handler before this one, so
// only unregistered ones get here).
func spaFileServer(webFS fs.FS) http.Handler {
	files := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			files.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "api" || strings.HasPrefix(name, "api/") {
			http.NotFound(w, r)
			return
		}
		if name != "" {
			if info, err := fs.Stat(webFS, name); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		serveIndex(w, r, webFS)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, webFS fs.FS) {
	body, err := fs.ReadFile(webFS, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		return
	}
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }
