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
)

// Handler is the filtering reverse proxy HTTP handler.
type Handler struct {
	store *config.Store
	jev   *jev.Client
	abuse *abuse.Tracker
	http  *http.Client // used only when response auditing is enabled
}

// New builds the proxy handler.
func New(store *config.Store, client *jev.Client) *Handler {
	return &Handler{
		store: store,
		jev:   client,
		abuse: abuse.New(),
		http:  &http.Client{Timeout: 10 * time.Minute},
	}
}

const (
	maxRequestBody  = 8 << 20  // 8 MiB
	maxResponseBody = 32 << 20 // 32 MiB (only buffered when auditing responses)
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	set := h.store.Settings()

	if set.UpstreamBaseURL == "" {
		http.Error(w, `{"error":{"message":"gateway upstream not configured"}}`, http.StatusBadGateway)
		return
	}

	ip := clientIP(r)

	// Fast path: reject IPs already banned for abuse, without calling JEV.
	if set.Enabled && set.AbuseEnabled {
		if banned, until := h.abuse.Banned(ip, start); banned {
			h.store.AddLog(config.LogEntry{
				TS: start, Method: r.Method, Path: r.URL.Path, Kind: "abuse",
				Decision: "block", LatencyMS: time.Since(start).Milliseconds(),
				IP: ip, Reason: "IP 因滥用被临时封禁至 " + until.Format("15:04:05"),
			})
			h.writeBanned(w, set, until)
			return
		}
	}

	// Read and retain the body so we can both inspect and forward it.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, `{"error":{"message":"failed to read request body"}}`, http.StatusBadRequest)
		return
	}

	decision, kind, model, score, reason, snippet := h.decide(r, set, body)

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
		Snippet:   snippet,
	})

	if decision == "block" {
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
func (h *Handler) decide(r *http.Request, set config.Settings, body []byte) (decision, kind, model string, score *float64, reason, snippet string) {
	if !set.Enabled {
		return "skip", "disabled", extractModel(body), nil, "gateway disabled", ""
	}
	// Only inspect JSON bodies on write methods; forward the rest untouched.
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return "skip", "non-write", "", nil, "non-write method", ""
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return "skip", "non-json", "", nil, "non-json content-type", ""
	}

	res := extract.Extract(r.URL.Path, body)
	if !res.Checkable || strings.TrimSpace(res.Text) == "" {
		return "skip", res.Kind, res.Model, nil, "no extractable input", ""
	}

	stateText := extract.Clamp(res.Text, set.MaxStateChars)
	// The exact text sent to JEV — recorded so extraction can be debugged from
	// the console (e.g. spotting when a client's system prompt is being sent).
	snippet = preview(stateText, 200)
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

	score = &val
	harmful := false
	if set.BlockIfBelow {
		harmful = val < set.SafetyThreshold
	} else {
		harmful = val >= set.SafetyThreshold
	}
	if harmful {
		return "block", res.Kind, res.Model, score, "safety score below threshold", snippet
	}
	return "allow", res.Kind, res.Model, score, "safe", snippet
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

func formatRetryAfter(until time.Time) string {
	secs := int(time.Until(until).Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

// forward proxies the (approved) request to the upstream base URL, preserving
// path, query, headers and streaming the response back.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, set config.Settings, body []byte) {
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
			// Restore the buffered body.
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.ContentLength = int64(len(body))
			req.Header.Set("X-JEV-Gateway", "allow")
		},
		ErrorHandler: func(rw http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("upstream proxy error: %v", err)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadGateway)
			_, _ = rw.Write([]byte(`{"error":{"message":"upstream request failed"}}`))
		},
	}
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
func (h *Handler) forwardChecked(w http.ResponseWriter, r *http.Request, start time.Time, set config.Settings, body []byte) {
	target, err := url.Parse(set.UpstreamBaseURL)
	if err != nil {
		http.Error(w, `{"error":{"message":"invalid upstream url"}}`, http.StatusBadGateway)
		return
	}
	outURL := *target
	outURL.Path = singleJoin(target.Path, r.URL.Path)
	outURL.RawQuery = r.URL.RawQuery

	out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":{"message":"build upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	copyHeaders(out.Header, r.Header)
	out.Header.Set("X-JEV-Gateway", "allow")
	out.Host = target.Host
	out.ContentLength = int64(len(body))

	resp, err := h.http.Do(out)
	if err != nil {
		log.Printf("upstream request error: %v", err)
		http.Error(w, `{"error":{"message":"upstream request failed"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))

	// Only audit successful, textual responses; pass errors straight through.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		res := extract.Output(resp.Header.Get("Content-Type"), respBody)
		if res.Checkable {
			ctx, cancel := context.WithTimeout(r.Context(), time.Duration(set.JEVTimeoutMS+2000)*time.Millisecond)
			val, jerr := h.jev.Score(ctx, jev.Params{
				BaseURL:     set.JEVBaseURL,
				Model:       set.JEVModel,
				State:       extract.Clamp(res.Text, set.MaxStateChars),
				Instruction: set.SafetyInstruction,
				TimeoutMS:   set.JEVTimeoutMS,
			})
			cancel()

			blocked := false
			reason := "response safe"
			var score *float64
			if jerr != nil {
				log.Printf("jev response evaluation error: %v", jerr)
				if !set.FailOpen {
					blocked = true
					reason = "response JEV error, fail-closed"
				} else {
					reason = "response JEV error, fail-open"
				}
			} else {
				score = &val
				if (set.BlockIfBelow && val < set.SafetyThreshold) || (!set.BlockIfBelow && val >= set.SafetyThreshold) {
					blocked = true
					reason = "response score below threshold"
				}
			}

			h.store.AddLog(config.LogEntry{
				TS: start, Method: r.Method, Path: r.URL.Path, Kind: "response",
				Decision: decisionWord(blocked, jerr), Score: score,
				LatencyMS: time.Since(start).Milliseconds(), IP: clientIP(r), Reason: reason,
				Snippet: preview(res.Text, 200),
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

func extractModel(body []byte) string {
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
