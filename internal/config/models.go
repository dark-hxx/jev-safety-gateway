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
		BlockMessage:      "请求内容被安全网关拦截 (blocked by JEV safety gateway).",
		MaxStateChars:     16000,
		JEVTimeoutMS:      8000,
		AbuseEnabled:      true,
		AbuseWindowSec:    60,
		AbuseMaxHarmful:   5,
		AbuseBanSec:       300,
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

// ScoreHistogram is the risk-score distribution over all evaluated records of
// the window: five fixed 0.2-wide slots plus the records that carry no score
// (JEV unreachable, or a request rejected before scoring).
type ScoreHistogram struct {
	Counts   []int64 `json:"counts"`   // [0,0.2) [0.2,0.4) [0.4,0.6) [0.6,0.8) [0.8,1.0]
	Unscored int64   `json:"unscored"` // score IS NULL
}

// ScoreSlots is the fixed number of equal-width slots in ScoreHistogram.Counts.
const ScoreSlots = 5
