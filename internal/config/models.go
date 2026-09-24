package config

import "time"

// Settings holds the runtime configuration of the gateway. All fields are
// editable from the admin frontend and persisted in SQLite.
type Settings struct {
	// Enabled is the master switch. When false the gateway forwards everything
	// without calling JEV.
	Enabled bool `json:"enabled"`

	// UpstreamBaseURL is where safe requests are forwarded, e.g.
	// "http://newapi:3000" or "https://api.openai.com".
	UpstreamBaseURL string `json:"upstream_base_url"`

	// JEVBaseURL is the TypeSafe/JEV API base, e.g. "https://api.typesafe.ai".
	JEVBaseURL string `json:"jev_base_url"`

	// JEVModel is the model passed to JEV, e.g. "jev-latest".
	JEVModel string `json:"jev_model"`

	// SafetyInstruction is the noul question sent to JEV. It must be phrased so
	// that a HIGH score means the content is SAFE to process.
	SafetyInstruction string `json:"safety_instruction"`

	// SafetyThreshold is the noul cutoff in [0,1].
	SafetyThreshold float64 `json:"safety_threshold"`

	// BlockIfBelow: when true, block if noul < threshold (instruction phrased as
	// "is it safe?"). When false, block if noul >= threshold (instruction phrased
	// as "is it harmful?").
	BlockIfBelow bool `json:"block_if_below"`

	// FailOpen: when JEV is unreachable/errors, forward the request anyway.
	FailOpen bool `json:"fail_open"`

	// CheckResponse: also run JEV on the upstream response body. Off by default
	// (only the user request is audited). NOTE: enabling this buffers the full
	// upstream response, so streaming (SSE) responses lose their real-time
	// token-by-token delivery.
	CheckResponse bool `json:"check_response"`

	// RejectOversizeBody decides what happens to a request body too large to
	// inspect (see internal/proxy.maxInspectBody). Off by default: such bodies
	// are forwarded untouched, so uploads and fine-tune datasets still work. When
	// on, they are rejected with HTTP 413 instead — fail closed, but it breaks
	// any endpoint whose legitimate payloads exceed the inspection limit.
	RejectOversizeBody bool `json:"reject_oversize_body"`

	// ExpandBase64 decodes base64 runs inside the extracted text before it is
	// sent to JEV (see internal/extract.ExpandBase64), so the same content scores
	// the same however it was encoded. On by default: without it the identical
	// prompt scored 0.15 written out and 0.80 base64-encoded, which made encoding
	// it a free bypass. Off sends the text to JEV exactly as the client wrote it.
	ExpandBase64 bool `json:"expand_base64"`

	// --- Per-IP abuse detection ---

	// AbuseEnabled turns on temporary IP bans for repeated harmful requests.
	AbuseEnabled bool `json:"abuse_enabled"`

	// AbuseWindowSec is the sliding window (seconds) over which harmful hits are
	// counted.
	AbuseWindowSec int `json:"abuse_window_sec"`

	// AbuseMaxHarmful is the number of harmful hits within the window that trips
	// a ban.
	AbuseMaxHarmful int `json:"abuse_max_harmful"`

	// AbuseBanSec is how long (seconds) a tripped IP stays banned. While banned,
	// all its requests are rejected without calling JEV.
	AbuseBanSec int `json:"abuse_ban_sec"`

	// BlockMessage is returned (as JSON error body) when a request is blocked.
	BlockMessage string `json:"block_message"`

	// MaxStateChars caps how much extracted text is sent to JEV.
	MaxStateChars int `json:"max_state_chars"`

	// JEVTimeoutMS is the per-call timeout for the JEV evaluation.
	JEVTimeoutMS int `json:"jev_timeout_ms"`

	// --- Audit retention ---

	// RecordSnippet persists the extracted text ("送检摘要") of each evaluated
	// request into the logs table. Off by default: the log then carries no user
	// content at all, and the console renders a placeholder instead. The debug
	// trace (JEV_DEBUG) is gated by the same switch, so user text cannot leak
	// into stdout while recording is off.
	RecordSnippet bool `json:"record_snippet"`

	// --- Verdict reuse ---

	// DedupEnabled reuses the JEV verdict for an identical submission (same text
	// and same evaluation parameters) within DedupWindowSec, instead of paying
	// for another JEV call. Clients that replay the whole conversation on every
	// turn submit the very same user text dozens of times in a row, so this
	// mainly removes redundant calls, not redundant checks.
	DedupEnabled bool `json:"dedup_enabled"`

	// DedupWindowSec is how long (seconds) a verdict may be reused.
	DedupWindowSec int `json:"dedup_window_sec"`
}

// DefaultSettings returns sensible defaults for a fresh install.
func DefaultSettings() Settings {
	return Settings{
		Enabled:           true,
		UpstreamBaseURL:   "",
		JEVBaseURL:        "https://api.typesafe.ai",
		JEVModel:          "jev-latest",
		SafetyInstruction: "这段话是否可以提交给llm进行深度推理，不会涉及到风险，比如：色情、暴力、破解、诱导?",
		SafetyThreshold:   0.5,
		BlockIfBelow:      true,
		FailOpen:          true,
		CheckResponse:     false,

		RejectOversizeBody: false,
		ExpandBase64:       true,
		BlockMessage:       "请求内容被安全网关拦截 (blocked by JEV safety gateway).",
		MaxStateChars:     16000,
		JEVTimeoutMS:      8000,
		AbuseEnabled:      true,
		AbuseWindowSec:    60,
		AbuseMaxHarmful:   5,
		AbuseBanSec:       300,
		RecordSnippet:     false,
		DedupEnabled:      true,
		DedupWindowSec:    60,
	}
}

// JEVKey is one TypeSafe API key used in round-robin rotation.
type JEVKey struct {
	ID        int64     `json:"id"`
	Label     string    `json:"label"`
	Key       string    `json:"key"`     // full value; masked on list responses
	Masked    string    `json:"masked"`  // e.g. apikey_2112…445e75
	Enabled   bool      `json:"enabled"`
	Calls     int64     `json:"calls"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// LogEntry records the outcome of one filtered request for the admin UI.
type LogEntry struct {
	ID        int64     `json:"id"`
	TS        time.Time `json:"ts"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	Decision  string    `json:"decision"` // allow | block | skip | error
	Score     *float64  `json:"score,omitempty"`
	Model     string    `json:"model"`
	LatencyMS int64     `json:"latency_ms"`
	IP        string    `json:"ip"`
	Reason    string    `json:"reason"`
	Snippet   string    `json:"snippet"`
}

// Stats is a small aggregate for the dashboard.
type Stats struct {
	Total   int64 `json:"total"`
	Allowed int64 `json:"allowed"`
	Blocked int64 `json:"blocked"`
	Skipped int64 `json:"skipped"`
	Errors  int64 `json:"errors"`
}

// StatBucket is one time bucket of the decision trend. Buckets are aligned to
// the bucket boundary and empty ones are filled with zeros by the store, so the
// series is always continuous over the queried window.
type StatBucket struct {
	TS      int64 `json:"ts"` // unix ms at the bucket's start
	Total   int64 `json:"total"`
	Allowed int64 `json:"allowed"`
	Blocked int64 `json:"blocked"`
}

// ModelCount is one model value seen in the audit log and how often it occurred.
// The console builds its model dropdown from these, so the choices are always
// values that actually appear in the log rather than a hard-coded list.
type ModelCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// ScoreHistogram is the risk-score distribution over all evaluated records of
// the window: five fixed 0.2-wide slots plus the records that carry no score
// (JEV unreachable, or a request rejected before scoring).
type ScoreHistogram struct {
	Counts   []int64 `json:"counts"`   // [0,0.2) [0.2,0.4) [0.4,0.6) [0.6,0.8) [0.8,1.0]
	Unscored int64   `json:"unscored"` // score IS NULL
}

// ScoreSlots is the fixed number of equal-width slots in ScoreHistogram.Counts.
const ScoreSlots = 5

// LatencyStats is the latency distribution of the window, from the single
// whole-request duration each log row carries (see AuditView: the gateway
// records one total, not a per-stage split).
//
// Sampled is set when the window held more rows than the store is willing to
// sort: the percentiles are then computed from a time-uniform sample, so they
// are indicative rather than exact. Count always reports the true row count of
// the window, sampled or not.
type LatencyStats struct {
	Count   int64   `json:"count"`
	Sampled bool    `json:"sampled"`
	P50     float64 `json:"p50"`
	P90     float64 `json:"p90"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Avg     float64 `json:"avg"`
	Min     int64   `json:"min"`
	Max     int64   `json:"max"`
}

// IPStat is the per-IP aggregate of the audit log over a window: raw decision
// counts plus a derived, explainable danger rating. It powers the IP risk
// analytics page. Raw counts and the score inputs are all carried so the console
// can show how a rating was reached rather than an opaque number.
type IPStat struct {
	IP      string `json:"ip"`
	Total   int64  `json:"total"`
	Allowed int64  `json:"allowed"`
	Blocked int64  `json:"blocked"`
	Skipped int64  `json:"skipped"`
	Errors  int64  `json:"errors"`

	// Scored is the number of rows that carry a JEV score (allow/block/error that
	// actually reached scoring). AvgScore/MinScore are nil when Scored is 0, so
	// the console renders "—" instead of a fabricated 0.
	Scored   int64    `json:"scored"`
	AvgScore *float64 `json:"avg_score,omitempty"`
	MinScore *float64 `json:"min_score,omitempty"`

	FirstSeen  int64  `json:"first_seen"` // unix ms of the earliest row in the window
	LastSeen   int64  `json:"last_seen"`  // unix ms of the most recent row
	LastReason string `json:"last_reason"`

	// Danger is a deterministic 0–1 rating (see ipDanger); Level buckets it into
	// crit/high/med/low. Both are computed, never stored.
	Danger float64 `json:"danger"`
	Level  string  `json:"level"`

	// Country/ASN are filled from an optional GeoIP resolver at aggregation time
	// (see IPStats). All omitempty: absent when no GeoIP database is configured or
	// the IP did not resolve (private/unknown), so the console shows nothing rather
	// than a fabricated value.
	Country     string `json:"country,omitempty"`      // ISO-3166-1 alpha-2, e.g. "US"
	CountryName string `json:"country_name,omitempty"` // English display name
	ASN         uint   `json:"asn,omitempty"`
	ASNOrg      string `json:"asn_org,omitempty"`
}

// IPStatsSummary is the KPI header for the IP analytics page: totals across all
// distinct IPs in the window, independent of the per-IP limit applied to the
// returned list.
type IPStatsSummary struct {
	DistinctIPs  int64 `json:"distinct_ips"`
	BlockedIPs   int64 `json:"blocked_ips"` // IPs with at least one block
	Crit         int64 `json:"crit"`
	High         int64 `json:"high"`
	TotalBlocked int64 `json:"total_blocked"`
	TotalEvents  int64 `json:"total_events"`
}

// GeoResolver resolves a client IP to its country and ASN. It is implemented by
// package internal/geoip over optional MaxMind databases and passed into IPStats;
// a nil resolver means no GeoIP is configured, so the analytics page keeps its
// "not connected" panels. Defining the interface here (rather than importing
// geoip) keeps config free of the GeoIP dependency, mirroring BanSnapshot in the
// admin package. CountryEnabled/ASNEnabled report which dimensions actually have
// a database open, so a resolver with only one database still works.
type GeoResolver interface {
	Lookup(ip string) (GeoInfo, bool)
	CountryEnabled() bool
	ASNEnabled() bool
	// GatewayLocation reports where this gateway is deployed, for the origin
	// map's central node. ok is false when the operator declared no coordinate,
	// in which case the map draws origins only — no fabricated hub. This is the
	// gateway's own location, not a client's: the map's hub/spoke reading comes
	// from pairing it with Lookup.
	GatewayLocation() (lat, lon float64, ok bool)
}

// GeoInfo is one IP's resolved attribution. Fields are zero when the relevant
// database is absent or the IP did not match (private ranges, unknown).
type GeoInfo struct {
	CountryISO    string // ISO-3166-1 alpha-2, e.g. "US"
	CountryName   string // English name from the database
	CountryNameZH string // zh-CN name when the database carries it
	ASN           uint
	ASNOrg        string
}

// GeoBucket is the per-country aggregate over the window for the geo panel: how
// many events and blocks came from each country. Country is the ISO code (""
// for unresolved/private IPs, grouped under one "unknown" row).
type GeoBucket struct {
	Country string `json:"country"`
	Name    string `json:"name"`
	Total   int64  `json:"total"`
	Blocked int64  `json:"blocked"`
}

// ASNBucket is the per-ASN aggregate over the window for the ASN panel.
type ASNBucket struct {
	ASN     uint   `json:"asn"`
	Org     string `json:"org"`
	Total   int64  `json:"total"`
	Blocked int64  `json:"blocked"`
}

// IPRule is one persisted IP access rule: a manual ban (temporary or permanent),
// a CIDR block, or an allowlist entry. Unlike the in-memory abuse bans (package
// internal/abuse, which reset on restart because they are automatic reactions),
// these survive restarts because they are administrator intent. Matching is by
// exact IP or CIDR containment; an allow rule wins over any block rule and over
// an abuse ban, but never bypasses content filtering (see proxy.ServeHTTP).
type IPRule struct {
	ID        int64      `json:"id"`
	Pattern   string     `json:"pattern"` // canonical single IP or CIDR, e.g. 1.2.3.4 or 10.0.0.0/8
	IsCIDR    bool       `json:"is_cidr"`
	Kind      string     `json:"kind"`                 // "block" | "allow"
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil = permanent (always nil for allow)
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"created_at"`
}
