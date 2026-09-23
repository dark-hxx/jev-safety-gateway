// Package proxy implements the filtering reverse proxy: it reads the request
// body, extracts the user input, asks JEV whether it is safe, and either
// forwards the request to the configured upstream or returns 403.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jev-gateway/internal/abuse"
	"jev-gateway/internal/config"
	"jev-gateway/internal/extract"
	"jev-gateway/internal/jev"
	"jev-gateway/internal/logx"
	"jev-gateway/internal/scorecache"
)

// Handler is the filtering reverse proxy HTTP handler.
type Handler struct {
	store *config.Store
	jev   *jev.Client
	abuse *abuse.Tracker
	cache *scorecache.Cache
	http  *http.Client // used only when response auditing is enabled
}

// New builds the proxy handler.
func New(store *config.Store, client *jev.Client) *Handler {
	return &Handler{
		store: store,
		jev:   client,
		abuse: abuse.New(),
		cache: scorecache.New(),
		http:  &http.Client{Timeout: 10 * time.Minute},
	}
}

const (
	// maxInspectBody is the largest request body the gateway buffers in order to
	// inspect it. Larger bodies are streamed through untouched (or rejected,
	// when RejectOversizeBody is set) but are never truncated: a truncated body
	// is corrupt JSON, so the upstream would reject every large client request
	// and the client would never learn the gateway was at fault.
	maxInspectBody = 8 << 20

	// maxResponseBody caps the upstream response buffered when auditing
	// responses (CheckResponse).
	maxResponseBody = 32 << 20
)

// requestBody is a request body that can be inspected and then forwarded
// byte-for-byte. Bodies larger than maxInspectBody keep only a prefix for
// logging; the unread remainder is still on the client connection and is
// streamed through after that prefix, so the upstream always receives exactly
// the bytes the client sent.
type requestBody struct {
	prefix  []byte
	rest    io.ReadCloser // unread remainder; nil once the whole body is buffered
	inspect bool          // prefix holds the complete body, so it can be checked
	size    int64         // original Content-Length, or -1 when unknown
}

// readBody buffers the request body when it is small enough to inspect, and
// otherwise leaves the remainder streaming (see requestBody).
func readBody(r *http.Request) (*requestBody, error) {
	b := &requestBody{size: r.ContentLength}
	if r.ContentLength > maxInspectBody {
		b.rest = r.Body // far too large to buffer: forward it as it arrives
		return b, nil
	}
	prefix, err := io.ReadAll(io.LimitReader(r.Body, maxInspectBody))
	if err != nil {
		return nil, err
	}
	b.prefix = prefix
	// With no Content-Length, filling the cap means more may still be coming.
	if r.ContentLength < 0 && int64(len(prefix)) == maxInspectBody {
		b.rest = r.Body
		return b, nil
	}
	_ = r.Body.Close()
	b.inspect = true
	return b, nil
}

// oversize reports whether the body was too large to buffer and inspect.
func (b *requestBody) oversize() bool { return b.rest != nil }

// describe renders the body size for logs.
func (b *requestBody) describe() string {
	if b.size < 0 {
		return "unknown length"
	}
	return strconv.FormatInt(b.size, 10) + " bytes"
}

// reader returns a fresh reader over the whole body (prefix plus remainder).
func (b *requestBody) reader() io.ReadCloser {
	if b.rest == nil {
		return io.NopCloser(bytes.NewReader(b.prefix))
	}
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(b.prefix), b.rest), b.rest}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	set := h.store.Settings()

	ip := clientIP(r)
	logx.Debugf("→ %s %s ip=%s ct=%q", r.Method, r.URL.Path, ip, r.Header.Get("Content-Type"))

	if set.UpstreamBaseURL == "" {
		http.Error(w, `{"error":{"message":"gateway upstream not configured"}}`, http.StatusBadGateway)
		return
	}

	// Fast path: reject IPs already banned for abuse, without calling JEV.
	if set.Enabled && set.AbuseEnabled {
		if banned, until := h.abuse.Banned(ip, start); banned {
			logx.Debugf("  ip=%s is banned until %s → 429", ip, until.Format("15:04:05"))
			h.store.AddLog(config.LogEntry{
				TS: start, Method: r.Method, Path: r.URL.Path, Kind: "abuse",
				Decision: "block", LatencyMS: time.Since(start).Milliseconds(),
				IP: ip, Reason: "IP 因滥用被临时封禁至 " + until.Format("15:04:05"),
			})
			h.writeBanned(w, set, until)
			return
		}
	}

	// Read and retain the body so we can both inspect and forward it. A body too
	// large to inspect is streamed through untouched rather than truncated.
	body, err := readBody(r)
	if err != nil {
		http.Error(w, `{"error":{"message":"failed to read request body"}}`, http.StatusBadRequest)
		return
	}
	logx.Debugf("  body=%d bytes inspect=%v", len(body.prefix), body.inspect)

	decision, kind, model, score, reason, snippet := h.decide(r, set, body)

	// An un-inspectable body is forwarded untouched by default; operators who
	// would rather fail closed reject it outright instead.
	oversizeReject := false
	if body.oversize() && set.Enabled && set.RejectOversizeBody {
		decision, oversizeReject = "block", true
		reason = "oversize body rejected: " + reason
	}
	logx.Debugf("  decision=%s kind=%s model=%q score=%s reason=%q snippet=%s",
		decision, kind, model, scoreStr(score), reason, debugSnippet(set, snippet))

	// A genuine harmful-content block (score present) counts as a strike; enough
	// strikes within the window ban the IP.
	if set.Enabled && set.AbuseEnabled && decision == "block" && score != nil {
		if tripped, until := h.abuse.Strike(ip, start, set.AbuseWindowSec, set.AbuseMaxHarmful, set.AbuseBanSec); tripped {
			reason += "；已触发滥用封禁至 " + until.Format("15:04:05")
		}
	}

	// Log the outcome (best-effort).
	h.store.AddLog(config.LogEntry{
		TS:        start,
		Method:    r.Method,
		Path:      r.URL.Path,
		Kind:      kind,
		Decision:  decision,
		Score:     score,
		Model:     model,
		LatencyMS: time.Since(start).Milliseconds(),
		IP:        ip,
		Reason:    reason,
		Snippet:   auditSnippet(set, snippet),
	})

	if decision == "block" {
		if oversizeReject {
			_ = r.Body.Close() // the body was never read
			h.writeTooLarge(w)
			return
		}
		h.writeBlocked(w, set, score)
		return
	}

	// Response auditing buffers the full upstream reply (losing streaming); only
	// used when explicitly enabled. Otherwise stream through transparently.
	if set.Enabled && set.CheckResponse {
		h.forwardChecked(w, r, start, set, body)
		return
	}
	h.forward(w, r, set, body)
}

// decide runs the safety evaluation and returns the decision plus metadata.
// decision is one of: allow, block, skip, error.
func (h *Handler) decide(r *http.Request, set config.Settings, body *requestBody) (decision, kind, model string, score *float64, reason, snippet string) {
	if !set.Enabled {
		return "skip", "disabled", peekModel(body.prefix), nil, "gateway disabled", ""
	}
	// Only inspect write methods; forward the rest untouched.
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return "skip", "non-write", "", nil, "non-write method", ""
	}
	if !body.inspect {
		return "skip", "oversize", peekModel(body.prefix), nil,
			"body too large to inspect (" + body.describe() + ")", ""
	}
	// Only inspect bodies we can parse: JSON and multipart/form-data (the
	// form-accepting endpoints: audio transcriptions, image edits, uploads).
	// Anything else — raw binary uploads, text/plain — forwards untouched.
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") && !strings.HasPrefix(ct, "multipart/form-data") {
		return "skip", "non-json", "", nil, "uninspectable content-type", ""
	}

	res := extract.Extract(r.URL.Path, r.Header.Get("Content-Type"), body.prefix)
	if !res.Checkable || strings.TrimSpace(res.Text) == "" {
		return "skip", res.Kind, res.Model, nil, "no extractable input", ""
	}

	stateText := extract.Clamp(res.Text, set.MaxStateChars)
	// The exact text sent to JEV, kept for the audit trail and the debug trace.
	// Whether the audit row stores it is up to the operator (auditSnippet).
	snippet = preview(stateText, 200)
	key := cacheKey(set, stateText)
	now := time.Now()

	// An identical submission — same text, same evaluation parameters — reuses the
	// previous verdict inside the dedup window instead of paying for another JEV
	// call. Only successful evaluations are cached: an error is re-evaluated every
	// time, so FailOpen/FailClosed keep their meaning.
	if val, ok := h.cachedScore(set, key, now); ok {
		score = &val
		decision, reason = classify(set, val, reusedMarker)
		return decision, res.Kind, res.Model, score, reason, snippet
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(set.JEVTimeoutMS+2000)*time.Millisecond)
	defer cancel()

	val, err := h.jev.Score(ctx, jev.Params{
		BaseURL:     set.JEVBaseURL,
		Model:       set.JEVModel,
		State:       stateText,
		Instruction: set.SafetyInstruction,
		TimeoutMS:   set.JEVTimeoutMS,
	})
	if err != nil {
		log.Printf("jev evaluation error: %v", err)
		if set.FailOpen {
			return "error", res.Kind, res.Model, nil, "JEV error, fail-open: " + err.Error(), snippet
		}
		return "block", res.Kind, res.Model, nil, "JEV error, fail-closed: " + err.Error(), snippet
	}

	if set.DedupEnabled {
		h.cache.Put(key, val, now)
	}
	score = &val
	decision, reason = classify(set, val, "")
	return decision, res.Kind, res.Model, score, reason, snippet
}

// cacheKey identifies one submission to the safety model: the text plus every
// parameter that can change the verdict. Two submissions sharing a key must
// score identically, so a stored score may be reused verbatim.
func cacheKey(set config.Settings, stateText string) string {
	return scorecache.Key(
		set.JEVBaseURL,
		set.JEVModel,
		set.SafetyInstruction,
		strconv.FormatFloat(set.SafetyThreshold, 'g', -1, 64),
		strconv.FormatBool(set.BlockIfBelow),
		stateText,
	)
}

// cachedScore returns a reusable verdict for key, or false when dedup is off or
// nothing was cached inside the (currently configured) window.
func (h *Handler) cachedScore(set config.Settings, key string, now time.Time) (float64, bool) {
	if !set.DedupEnabled {
		return 0, false
	}
	return h.cache.Get(key, dedupWindow(set), now)
}

// dedupWindow is how long a verdict may be reused. A non-positive configured
// window falls back to the default, matching config.UpdateSettings.
func dedupWindow(set config.Settings) time.Duration {
	sec := set.DedupWindowSec
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// harmfulScore reports whether a score crosses the block threshold in the
// direction the configured instruction is phrased for.
func harmfulScore(set config.Settings, score float64) bool {
	if set.BlockIfBelow {
		return score < set.SafetyThreshold
	}
	return score >= set.SafetyThreshold
}

// classify maps a safety score to the decision and reason shared by a fresh and
// a reused verdict. mark is appended to the reason: callers pass reusedMarker for
// a cached verdict and "" for a freshly evaluated one.
func classify(set config.Settings, score float64, mark string) (decision, reason string) {
	if harmfulScore(set, score) {
		return "block", "safety score below threshold" + mark
	}
	return "allow", "safe" + mark
}

// reusedMarker is appended to the reason of a verdict that came from the dedup
// cache rather than a fresh JEV call, so the console explains the missing call.
const reusedMarker = "；命中相同内容缓存"

// snippetSuppressed replaces the submission text in the debug trace while
// snippet recording is off.
const snippetSuppressed = "<未记录：已关闭送检摘要记录>"

// auditSnippet returns the text to persist in the audit row. With record_snippet
// off (the default) the gateway stores no user content at all: the row carries an
// empty snippet and the console renders a placeholder instead.
func auditSnippet(set config.Settings, snippet string) string {
	if !set.RecordSnippet {
		return ""
	}
	return snippet
}

// debugSnippet renders the submission text for the debug trace. With recording
// off the text must not reach stdout — but printing an empty string would read as
// "nothing was extracted", so the trace says the text was withheld instead.
func debugSnippet(set config.Settings, snippet string) string {
	if !set.RecordSnippet && snippet != "" {
		return strconv.Quote(snippetSuppressed)
	}
	return strconv.Quote(snippet)
}

// preview returns a single-line, truncated copy of s for logging.
func preview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace/newlines
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func (h *Handler) writeBlocked(w http.ResponseWriter, set config.Settings, score *float64) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-JEV-Gateway", "blocked")
	if score != nil {
		w.Header().Set("X-JEV-Score", formatScore(*score))
	}
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": set.BlockMessage,
			"type":    "jev_safety_block",
			"code":    "content_blocked",
		},
	})
}

// writeBanned rejects a request from a currently-banned IP.
func (h *Handler) writeBanned(w http.ResponseWriter, set config.Settings, until time.Time) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-JEV-Gateway", "banned")
	w.Header().Set("Retry-After", formatRetryAfter(until))
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": "检测到大量攻击性请求，来源 IP 已被临时封禁 (rate limited by JEV safety gateway).",
			"type":    "jev_abuse_block",
			"code":    "ip_temporarily_banned",
		},
	})
}

// writeTooLarge rejects a request body that is too large to inspect. Used only
// when RejectOversizeBody is on, so that an operator can fail closed instead of
// forwarding an unchecked body.
func (h *Handler) writeTooLarge(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-JEV-Gateway", "blocked")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": "请求体超过安全网关可检查的体积上限，已被拒绝 (request body too large to inspect by JEV safety gateway).",
			"type":    "jev_oversize_body",
			"code":    "request_too_large",
		},
	})
}

func formatRetryAfter(until time.Time) string {
	secs := int(time.Until(until).Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// forward proxies the (approved) request to the upstream base URL, preserving
// path, query, headers and streaming the response back.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, set config.Settings, body *requestBody) {
	target, err := url.Parse(set.UpstreamBaseURL)
	if err != nil {
		http.Error(w, `{"error":{"message":"invalid upstream url"}}`, http.StatusBadGateway)
		return
	}

	rp := &httputil.ReverseProxy{
		FlushInterval: 100 * time.Millisecond, // stream SSE responses promptly
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			// Join upstream base path with the incoming path.
			req.URL.Path = singleJoin(target.Path, req.URL.Path)
			req.Host = target.Host
			// Restore the body and keep its original framing: -1 means the client
			// sent no Content-Length, so the request must stay chunked.
			req.Body = body.reader()
			req.ContentLength = body.size
		},
		ModifyResponse: func(resp *http.Response) error {
			// The gateway's verdict belongs on the response to the client, not on
			// the request forwarded upstream.
			resp.Header.Set("X-JEV-Gateway", "allow")
			logx.Debugf("  ← upstream %d %s (stream passthrough)", resp.StatusCode, resp.Request.URL)
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("upstream proxy error: %v", err)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte(`{"error":{"message":"upstream request failed"}}`))
		},
	}
	logx.Debugf("  → forwarding to %s (stream)", singleJoin(target.String(), r.URL.Path))
	rp.ServeHTTP(w, r)
}

// hopHeaders are per-connection headers that must not be forwarded.
var hopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		if hopHeaders[k] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// forwardChecked performs a buffered round-trip to the upstream, then audits the
// response body with JEV before returning it. Used only when CheckResponse is on.
func (h *Handler) forwardChecked(w http.ResponseWriter, r *http.Request, start time.Time, set config.Settings, body *requestBody) {
	target, err := url.Parse(set.UpstreamBaseURL)
	if err != nil {
		http.Error(w, `{"error":{"message":"invalid upstream url"}}`, http.StatusBadGateway)
		return
	}
	outURL := *target
	outURL.Path = singleJoin(target.Path, r.URL.Path)
	outURL.RawQuery = r.URL.RawQuery

	out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), body.reader())
	if err != nil {
		http.Error(w, `{"error":{"message":"build upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	copyHeaders(out.Header, r.Header)
	out.Host = target.Host
	// Preserve the client's framing: -1 keeps the request chunked.
	out.ContentLength = body.size

	resp, err := h.http.Do(out)
	if err != nil {
		log.Printf("upstream request error: %v", err)
		http.Error(w, `{"error":{"message":"upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	logx.Debugf("  ← upstream %d %s resp=%d bytes (buffered for audit)", resp.StatusCode, outURL.String(), len(respBody))

	// Only audit successful, textual responses; pass errors straight through.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res := extract.Output(resp.Header.Get("Content-Type"), respBody)
		if res.Checkable {
			stateText := extract.Clamp(res.Text, set.MaxStateChars)
			key := cacheKey(set, stateText)
			now := time.Now()

			blocked := false
			reason := "response safe"
			var score *float64
			var jerr error

			// Same dedup rule as the request path: an identical audited response
			// reuses the previous verdict instead of paying for another JEV call.
			if val, ok := h.cachedScore(set, key, now); ok {
				score = &val
				blocked = harmfulScore(set, val)
				if blocked {
					reason = "response score below threshold" + reusedMarker
				} else {
					reason = "response safe" + reusedMarker
				}
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), time.Duration(set.JEVTimeoutMS+2000)*time.Millisecond)
				val, err := h.jev.Score(ctx, jev.Params{
					BaseURL:     set.JEVBaseURL,
					Model:       set.JEVModel,
					State:       stateText,
					Instruction: set.SafetyInstruction,
					TimeoutMS:   set.JEVTimeoutMS,
				})
				cancel()
				jerr = err

				if jerr != nil {
					log.Printf("jev response evaluation error: %v", jerr)
					if !set.FailOpen {
						blocked = true
						reason = "response JEV error, fail-closed"
					} else {
						reason = "response JEV error, fail-open"
					}
				} else {
					if set.DedupEnabled {
						h.cache.Put(key, val, now)
					}
					score = &val
					if harmfulScore(set, val) {
						blocked = true
						reason = "response score below threshold"
					}
				}
			}

			h.store.AddLog(config.LogEntry{
				TS: start, Method: r.Method, Path: r.URL.Path, Kind: "response",
				Decision: decisionWord(blocked, jerr), Score: score,
				LatencyMS: time.Since(start).Milliseconds(), IP: clientIP(r), Reason: reason,
				Snippet: auditSnippet(set, preview(res.Text, 200)),
			})
			if blocked {
				h.writeBlocked(w, set, score)
				return
			}
		}
	}

	// Relay the upstream response verbatim.
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-JEV-Gateway", "allow")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

func decisionWord(blocked bool, jerr error) string {
	if blocked {
		return "block"
	}
	if jerr != nil {
		return "error"
	}
	return "allow"
}

func singleJoin(a, b string) string {
	a = strings.TrimSuffix(a, "/")
	if a == "" {
		return b
	}
	if !strings.HasPrefix(b, "/") {
		b = "/" + b
	}
	return a + b
}

func peekModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return m.Model
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func formatScore(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func scoreStr(f *float64) string {
	if f == nil {
		return "-"
	}
	return strconv.FormatFloat(*f, 'f', 3, 64)
}
