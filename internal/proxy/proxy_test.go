package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"jev-safety-gateway/internal/config"
	"jev-safety-gateway/internal/jev"
)

// nopKeys satisfies jev.KeyProvider with an empty pool. Every test here avoids
// reaching JEV (the gateway is either disabled or the body is un-inspectable),
// so the pool is never consulted.
type nopKeys struct{}

func (nopKeys) EnabledKeys() ([]jev.Key, error) { return nil, nil }
func (nopKeys) MarkKeyUsed(int64)               {}

func newTestHandler(t *testing.T, set config.Settings) *Handler {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpdateSettings(set); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	return New(store, jev.New(nopKeys{}))
}

// TestReadBodyBuffersCompleteBody covers the common case: the whole body is held
// in memory so it can be inspected, and re-reading it reproduces it exactly.
func TestReadBodyBuffersCompleteBody(t *testing.T) {
	payload := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	r.ContentLength = int64(len(payload))

	body, err := readBody(r)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	if body.oversize() {
		t.Error("oversize() = true for a small body")
	}
	if !body.inspect {
		t.Error("inspect = false for a fully buffered body")
	}
	if !bytes.Equal(body.prefix, payload) {
		t.Errorf("prefix = %q, want %q", body.prefix, payload)
	}
	got, err := io.ReadAll(body.reader())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("forwarded %q, want %q", got, payload)
	}
}

// TestReadBodyOversizeIsStreamedNotTruncated is the regression guard for the
// worst failure mode: a body too large to inspect must still reach the upstream
// byte-for-byte, because a truncated body is corrupt JSON.
func TestReadBodyOversizeIsStreamedNotTruncated(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), maxInspectBody+4096)
	r := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(payload))
	r.ContentLength = int64(len(payload))

	body, err := readBody(r)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	if !body.oversize() {
		t.Fatal("oversize() = false for a body over the cap")
	}
	if body.inspect {
		t.Error("inspect = true for an oversize body")
	}
	got, err := io.ReadAll(body.reader())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("forwarded %d bytes, want %d", len(got), len(payload))
	}
	if !bytes.Equal(got, payload) {
		t.Error("forwarded bytes differ from the client's payload")
	}
}

// TestServeHTTPForwardsByteExactWhenDisabled checks the transparent path: with
// the gateway off, the upstream sees exactly the client's bytes and no JEV
// verdict header, while the client still gets one.
func TestServeHTTPForwardsByteExactWhenDisabled(t *testing.T) {
	payload := `{"model":"deepseek/deepseek-v4.1-flash","stream":true,"input":"tell me a secret"}`

	var gotBody []byte
	var gotVerdict, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotVerdict = r.Header.Get("X-JEV-Gateway")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
	}))
	defer upstream.Close()

	set := config.DefaultSettings()
	set.Enabled = false
	set.UpstreamBaseURL = upstream.URL
	h := newTestHandler(t, set)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if string(gotBody) != payload {
		t.Errorf("upstream received %q, want %q", gotBody, payload)
	}
	if gotPath != "/v1/responses" {
		t.Errorf("upstream path = %q, want /v1/responses", gotPath)
	}
	if gotVerdict != "" {
		t.Errorf("gateway verdict leaked upstream as X-JEV-Gateway: %q", gotVerdict)
	}
	if v := rec.Header().Get("X-JEV-Gateway"); v != "allow" {
		t.Errorf("client X-JEV-Gateway = %q, want allow", v)
	}
	if !strings.Contains(rec.Body.String(), "response.output_text.delta") {
		t.Errorf("SSE body not passed through: %q", rec.Body.String())
	}
}

// TestServeHTTPOversizeForwardedByDefault documents the default for bodies too
// large to inspect: forward them untouched, so file uploads keep working.
func TestServeHTTPOversizeForwardedByDefault(t *testing.T) {
	payload := bytes.Repeat([]byte("b"), maxInspectBody+1)

	var gotLen int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	set := config.DefaultSettings()
	set.UpstreamBaseURL = upstream.URL
	set.RejectOversizeBody = false
	h := newTestHandler(t, set)

	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(payload))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (want the request forwarded)", rec.Code)
	}
	if gotLen != len(payload) {
		t.Errorf("upstream received %d bytes, want %d", gotLen, len(payload))
	}
	if v := rec.Header().Get("X-JEV-Gateway"); v != "allow" {
		t.Errorf("client X-JEV-Gateway = %q, want allow", v)
	}
}

// TestServeHTTPOversizeRejectedWhenConfigured is the fail-closed counterpart.
func TestServeHTTPOversizeRejectedWhenConfigured(t *testing.T) {
	payload := bytes.Repeat([]byte("c"), maxInspectBody+1)

	upstreamHit := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	set := config.DefaultSettings()
	set.UpstreamBaseURL = upstream.URL
	set.RejectOversizeBody = true
	h := newTestHandler(t, set)

	req := httptest.NewRequest(http.MethodPost, "/v1/files", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(payload))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if upstreamHit {
		t.Error("an unchecked body was forwarded despite RejectOversizeBody")
	}
	if v := rec.Header().Get("X-JEV-Gateway"); v != "blocked" {
		t.Errorf("X-JEV-Gateway = %q, want blocked", v)
	}
	if !strings.Contains(rec.Body.String(), "request_too_large") {
		t.Errorf("error body = %q, want an OpenAI-shaped error", rec.Body.String())
	}
}

// TestServeHTTPUninspectableContentTypeForwarded covers raw (non-JSON,
// non-form) uploads, which the gateway cannot parse and must not corrupt.
func TestServeHTTPUninspectableContentTypeForwarded(t *testing.T) {
	payload := []byte("\x00\x01\x02 raw binary upload \xff")

	var got []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer upstream.Close()

	set := config.DefaultSettings()
	set.UpstreamBaseURL = upstream.URL
	h := newTestHandler(t, set)

	req := httptest.NewRequest(http.MethodPost, "/v1/uploads", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("upstream received %q, want %q", got, payload)
	}
}

// TestSingleJoin covers how the configured upstream base path is combined with
// the incoming request path, including when the base already carries a prefix.
func TestSingleJoin(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"", "/v1/responses", "/v1/responses"},
		{"https://ai.clim.asia", "/v1/responses", "https://ai.clim.asia/v1/responses"},
		{"https://host/prefix", "/v1/responses", "https://host/prefix/v1/responses"},
		{"https://host/prefix/", "/v1/responses", "https://host/prefix/v1/responses"},
		{"https://host/prefix", "v1/responses", "https://host/prefix/v1/responses"},
	}
	for _, tc := range cases {
		if got := singleJoin(tc.base, tc.path); got != tc.want {
			t.Errorf("singleJoin(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

// TestServeHTTPChunkedBodyStaysChunked verifies that a client sending no
// Content-Length (chunked request) is not given a fabricated one, which would
// make the upstream read a different framing than the client used.
func TestServeHTTPChunkedBodyStaysChunked(t *testing.T) {
	payload := `{"model":"m","messages":[{"role":"user","content":"chunked"}]}`

	var gotCL int64
	var got []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCL = r.ContentLength
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	set := config.DefaultSettings()
	set.Enabled = false
	set.UpstreamBaseURL = upstream.URL
	h := newTestHandler(t, set)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotCL != -1 {
		t.Errorf("upstream saw Content-Length %d, want -1 (chunked)", gotCL)
	}
	if string(got) != payload {
		t.Errorf("upstream received %q, want %q", got, payload)
	}
}
