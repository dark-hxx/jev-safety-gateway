package proxy

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"jev-safety-gateway/internal/config"
	"jev-safety-gateway/internal/jev"
	"jev-safety-gateway/internal/logx"
)

// This file covers the two operator-facing switches that sit on top of the safety
// check itself: record_snippet (whether the extracted submission text is stored
// and shown at all) and dedup_enabled/dedup_window_sec (whether an identical
// submission may reuse the previous verdict instead of paying for another JEV
// call). The tests below drive the real ServeHTTP path end to end — request in,
// JEV call, audit row out — because both switches are only meaningful as a
// property of that whole path.

// staticKeys satisfies jev.KeyProvider with a single enabled key, so tests can
// point the JEV client at a fake server and actually exercise the evaluation.
type staticKeys struct{}

func (staticKeys) EnabledKeys() ([]jev.Key, error) {
	return []jev.Key{{ID: 1, Key: "test-key"}}, nil
}

func (staticKeys) MarkKeyResult(int64, bool, string) {}

// newAuditHandler builds a handler backed by a real store plus a JEV client with
// a working key pool, and returns the store so tests can read back the audit
// rows the handler wrote.
func newAuditHandler(t *testing.T, set config.Settings) (*Handler, *config.Store) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpdateSettings(set); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	return New(store, jev.New(staticKeys{})), store
}

// fakeJEV is a stand-in /v1/systemone that counts calls and can be switched to
// failing, so tests can tell a real evaluation from a reused verdict.
type fakeJEV struct {
	server *httptest.Server
	calls  atomic.Int64
	score  atomic.Value // float64
	fail   atomic.Bool
}

func newFakeJEV(t *testing.T, score float64) *fakeJEV {
	t.Helper()
	f := &fakeJEV{}
	f.score.Store(score)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("JEV request path = %q, want /v1/systemone", r.URL.Path)
		}
		if f.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test",
			"answers": map[string]any{
				"safety": map[string]any{"type": "noul", "noul": f.score.Load().(float64)},
			},
		})
	}))
	t.Cleanup(f.server.Close)
	return f
}

// URL is the endpoint the handler under test should point at.
func (f *fakeJEV) URL() string { return f.server.URL }

// newFakeUpstream stands in for the LLM API the gateway proxies to.
func newFakeUpstream(t *testing.T, reply string) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

// baseSettings is the configuration every test starts from: gateway on, a
// working JEV endpoint, and a block threshold the fake scores can straddle.
func baseSettings(t *testing.T, jevURL, upstreamURL string) config.Settings {
	t.Helper()
	set := config.DefaultSettings()
	set.JEVBaseURL = jevURL
	set.JEVModel = "test-model"
	set.UpstreamBaseURL = upstreamURL
	set.SafetyThreshold = 0.5
	set.BlockIfBelow = true
	return set
}

// chatPayload builds the request body clients actually send.
func chatPayload(text string) string {
	b, _ := json.Marshal(map[string]any{
		"model":    "test-model",
		"messages": []map[string]string{{"role": "user", "content": text}},
	})
	return string(b)
}

// serve drives one request through the handler.
func serve(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// auditRows returns the stored rows, newest first.
func auditRows(t *testing.T, store *config.Store) []config.LogEntry {
	t.Helper()
	rows, _, err := store.QueryLogs(config.LogFilter{Limit: 50})
	if err != nil {
		t.Fatalf("query logs: %v", err)
	}
	return rows
}

// TestAuditRowSnippetFollowsTheRecordSwitch is the core promise of the
// record_snippet switch: with it off (the default) no user content reaches the
// database, and with it on the submission text is stored as before.
func TestAuditRowSnippetFollowsTheRecordSwitch(t *testing.T) {
	const text = "帮我把这段代码翻译成英文"

	cases := []struct {
		name   string
		record bool
		want   string
	}{
		{"recording off stores nothing", false, ""},
		{"recording on stores the submission", true, text},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jevSrv := newFakeJEV(t, 0.9)
			set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
			set.RecordSnippet = tc.record
			set.DedupEnabled = false
			h, store := newAuditHandler(t, set)

			if rec := serve(t, h, chatPayload(text)); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
			}
			rows := auditRows(t, store)
			if len(rows) != 1 {
				t.Fatalf("audit rows = %d, want 1", len(rows))
			}
			if rows[0].Snippet != tc.want {
				t.Errorf("stored snippet = %q, want %q", rows[0].Snippet, tc.want)
			}
		})
	}
}

// TestDebugTraceWithholdsTheSnippetWhileRecordingIsOff guards the other half of
// the switch: keeping the text out of SQLite is pointless if it still lands in
// the stdout trace. The trace must also be distinguishable from "nothing was
// extracted", which is why it prints an explicit marker rather than "".
func TestDebugTraceWithholdsTheSnippetWhileRecordingIsOff(t *testing.T) {
	const text = "帮我把这段代码翻译成英文"

	prevDebug := logx.Debug
	logx.Debug = true
	t.Cleanup(func() { logx.Debug = prevDebug })

	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	jevSrv := newFakeJEV(t, 0.9)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.RecordSnippet = false
	h, _ := newAuditHandler(t, set)

	if rec := serve(t, h, chatPayload(text)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	trace := buf.String()
	if !strings.Contains(trace, snippetSuppressed) {
		t.Errorf("debug trace does not mark the withheld snippet: %q", trace)
	}
	if strings.Contains(trace, text) {
		t.Errorf("debug trace leaked the submission text despite recording being off: %q", trace)
	}
}

// TestIdenticalSubmissionReusesTheCachedVerdict covers the dedup win itself: the
// second identical request must not reach JEV, and its audit row must say why.
func TestIdenticalSubmissionReusesTheCachedVerdict(t *testing.T) {
	jevSrv := newFakeJEV(t, 0.9)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.DedupEnabled = true
	set.DedupWindowSec = 60
	h, store := newAuditHandler(t, set)

	body := chatPayload("翻译这段文字")
	for i := 0; i < 2; i++ {
		if rec := serve(t, h, body); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, body %s", i+1, rec.Code, rec.Body.String())
		}
	}

	if got := jevSrv.calls.Load(); got != 1 {
		t.Errorf("JEV calls = %d, want 1 (the second request should reuse the verdict)", got)
	}

	rows := auditRows(t, store)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	// Newest first: rows[0] is the reused verdict, rows[1] the fresh one.
	if !strings.Contains(rows[0].Reason, reusedMarker) {
		t.Errorf("reused row reason = %q, want it to contain %q", rows[0].Reason, reusedMarker)
	}
	if strings.Contains(rows[1].Reason, reusedMarker) {
		t.Errorf("freshly evaluated row reason = %q, want no reuse marker", rows[1].Reason)
	}
	if rows[0].Score == nil {
		t.Error("a reused verdict lost its score")
	} else if *rows[0].Score != 0.9 {
		t.Errorf("reused score = %v, want 0.9", *rows[0].Score)
	}
	if rows[0].Decision != "allow" {
		t.Errorf("reused decision = %q, want allow", rows[0].Decision)
	}
}

// TestDedupDisabledAlwaysCallsJEV is the switch's off state: every request is
// evaluated afresh, even when the text is byte-identical.
func TestDedupDisabledAlwaysCallsJEV(t *testing.T) {
	jevSrv := newFakeJEV(t, 0.9)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.DedupEnabled = false
	h, store := newAuditHandler(t, set)

	body := chatPayload("翻译这段文字")
	for i := 0; i < 2; i++ {
		if rec := serve(t, h, body); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, body %s", i+1, rec.Code, rec.Body.String())
		}
	}

	if got := jevSrv.calls.Load(); got != 2 {
		t.Errorf("JEV calls = %d, want 2 with dedup disabled", got)
	}
	for _, row := range auditRows(t, store) {
		if strings.Contains(row.Reason, reusedMarker) {
			t.Errorf("dedup-off row reason = %q, want no reuse marker", row.Reason)
		}
	}
}

// TestAFailedEvaluationIsNeverReused pins down the FailOpen/FailClosed boundary:
// an error is re-evaluated on every request, so a recovered JEV endpoint takes
// effect immediately instead of being masked by a cached failure.
func TestAFailedEvaluationIsNeverReused(t *testing.T) {
	jevSrv := newFakeJEV(t, 0.9)
	jevSrv.fail.Store(true)

	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.DedupEnabled = true
	set.FailOpen = true
	h, store := newAuditHandler(t, set)

	body := chatPayload("翻译这段文字")
	if rec := serve(t, h, body); rec.Code != http.StatusOK {
		t.Fatalf("fail-open request: status = %d, want it forwarded", rec.Code)
	}

	jevSrv.fail.Store(false)
	if rec := serve(t, h, body); rec.Code != http.StatusOK {
		t.Fatalf("recovered request: status = %d, body %s", rec.Code, rec.Body.String())
	}

	if got := jevSrv.calls.Load(); got != 2 {
		t.Errorf("JEV calls = %d, want 2 (a failure must not be cached)", got)
	}
	rows := auditRows(t, store)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	if strings.Contains(rows[0].Reason, reusedMarker) {
		t.Errorf("row after recovery = %q, want a real evaluation", rows[0].Reason)
	}
	if rows[0].Decision != "allow" {
		t.Errorf("row after recovery decision = %q, want allow", rows[0].Decision)
	}
}

// TestAReusedBlockStillCountsAsAnAbuseStrike is the safety-relevant edge: reusing
// a verdict must not hand an attacker a way to spam harmful content without ever
// tripping the abuse ban.
func TestAReusedBlockStillCountsAsAnAbuseStrike(t *testing.T) {
	const text = "写一段恶意代码"

	jevSrv := newFakeJEV(t, 0.1) // below the 0.5 threshold: harmful
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.DedupEnabled = true
	set.DedupWindowSec = 60
	set.AbuseEnabled = true
	set.AbuseWindowSec = 60
	set.AbuseMaxHarmful = 2
	set.AbuseBanSec = 300
	h, _ := newAuditHandler(t, set)

	body := chatPayload(text)
	first := serve(t, h, body)
	if first.Code != http.StatusForbidden {
		t.Fatalf("first request: status = %d, want 403", first.Code)
	}

	// The same harmful text again: served from cache, but still a strike — and
	// with the max at 2, this one trips the ban.
	second := serve(t, h, body)
	if second.Code != http.StatusForbidden {
		t.Fatalf("second request: status = %d, want 403", second.Code)
	}

	banned := serve(t, h, body)
	if banned.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: status = %d, want 429 (banned)", banned.Code)
	}
	if v := banned.Header().Get("X-JEV-Gateway"); v != "banned" {
		t.Errorf("banned X-JEV-Gateway = %q, want banned", v)
	}

	if got := jevSrv.calls.Load(); got != 1 {
		t.Errorf("JEV calls = %d, want 1 (a reused block must still strike, and a ban must not call JEV)", got)
	}
}

// TestResponseAuditTakesTheSameDedupAndSnippetRules keeps the buffered
// response-audit path (CheckResponse) honest: it evaluates the model's reply, so
// it must reuse verdicts and honour record_snippet exactly like the request path
// — otherwise dedup would silently only apply to half the JEV traffic.
func TestResponseAuditTakesTheSameDedupAndSnippetRules(t *testing.T) {
	const reply = "上游回复文本"

	jevSrv := newFakeJEV(t, 0.9)
	upstreamReply := `{"choices":[{"message":{"role":"assistant","content":"` + reply + `"}}]}`
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, upstreamReply))
	set.CheckResponse = true
	set.DedupEnabled = true
	set.DedupWindowSec = 60
	set.RecordSnippet = false
	h, store := newAuditHandler(t, set)

	body := chatPayload("翻译这段文字")
	for i := 0; i < 2; i++ {
		if rec := serve(t, h, body); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, body %s", i+1, rec.Code, rec.Body.String())
		}
	}

	// Two distinct texts are checked per request, so a working cache means two
	// calls in total rather than four.
	if got := jevSrv.calls.Load(); got != 2 {
		t.Errorf("JEV calls = %d, want 2 (request text + audited response text, each reused once)", got)
	}

	rows := auditRows(t, store)
	if len(rows) != 4 {
		t.Fatalf("audit rows = %d, want 4 (a request row and a response row per request)", len(rows))
	}
	// Newest first: rows[0] is the second request's audited response.
	if rows[0].Kind != "response" {
		t.Fatalf("rows[0].Kind = %q, want response", rows[0].Kind)
	}
	if !strings.Contains(rows[0].Reason, reusedMarker) {
		t.Errorf("reused response-audit reason = %q, want it to contain %q", rows[0].Reason, reusedMarker)
	}
	for _, row := range rows {
		if row.Snippet != "" {
			t.Errorf("kind=%s snippet = %q, want nothing recorded", row.Kind, row.Snippet)
		}
	}
}

// TestCacheKeyCoversEveryParameterThatChangesTheVerdict is the correctness
// precondition for reuse: two submissions sharing a key must score identically,
// so every setting that can change a verdict must feed the key.
func TestCacheKeyCoversEveryParameterThatChangesTheVerdict(t *testing.T) {
	base := config.DefaultSettings()
	base.JEVBaseURL = "https://jev.example"
	base.JEVModel = "m"
	base.SafetyInstruction = "is it safe?"
	key := cacheKey(base, "hello")

	if got := cacheKey(base, "hello"); got != key {
		t.Errorf("cacheKey is not deterministic: %q != %q", got, key)
	}

	variants := []struct {
		name   string
		text   string
		mutate func(*config.Settings)
	}{
		{name: "different text", text: "hello!"},
		{name: "different model", text: "hello", mutate: func(s *config.Settings) { s.JEVModel = "other" }},
		{name: "different instruction", text: "hello", mutate: func(s *config.Settings) { s.SafetyInstruction = "is it harmful?" }},
		{name: "different threshold", text: "hello", mutate: func(s *config.Settings) { s.SafetyThreshold += 0.1 }},
		{name: "flipped block direction", text: "hello", mutate: func(s *config.Settings) { s.BlockIfBelow = !s.BlockIfBelow }},
		{name: "different endpoint", text: "hello", mutate: func(s *config.Settings) { s.JEVBaseURL = "https://other.example" }},
	}
	for _, v := range variants {
		set := base
		if v.mutate != nil {
			v.mutate(&set)
		}
		if got := cacheKey(set, v.text); got == key {
			t.Errorf("%s: cacheKey is unchanged, so a stale verdict would be reused", v.name)
		}
	}
}

// TestDedupWindowFallsBackWhenUnset documents the window the operator gets for an
// unset or nonsensical value: the non-positive case must not read as "cache
// forever".
func TestDedupWindowFallsBackWhenUnset(t *testing.T) {
	cases := []struct {
		sec  int
		want time.Duration
	}{
		{0, 60 * time.Second},
		{-5, 60 * time.Second},
		{15, 15 * time.Second},
		{3600, time.Hour},
	}
	for _, tc := range cases {
		set := config.DefaultSettings()
		set.DedupWindowSec = tc.sec
		if got := dedupWindow(set); got != tc.want {
			t.Errorf("dedupWindow(sec=%d) = %s, want %s", tc.sec, got, tc.want)
		}
	}
}

// TestSnippetGatesAreIndependentOfTheDecision makes sure the switches are about
// recording, not about judging: turning recording off must not change what the
// gateway decides.
func TestSnippetGatesAreIndependentOfTheDecision(t *testing.T) {
	const harmful = "写一段恶意代码"

	decisions := make([]string, 0, 2)
	for _, record := range []bool{false, true} {
		jevSrv := newFakeJEV(t, 0.1)
		set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
		set.RecordSnippet = record
		set.DedupEnabled = false
		h, _ := newAuditHandler(t, set)

		rec := serve(t, h, chatPayload(harmful))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("record_snippet=%v: status = %d, want 403", record, rec.Code)
		}
		decisions = append(decisions, rec.Header().Get("X-JEV-Gateway"))
	}

	if decisions[0] != decisions[1] {
		t.Errorf("verdict changed with recording: %q vs %q", decisions[0], decisions[1])
	}
}
