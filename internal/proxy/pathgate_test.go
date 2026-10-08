package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jev-safety-gateway/internal/config"
)

// gateHarness is an upstream that records how many requests reached it, plus the
// handler wired to it. The path gate's whole point is that a rejected request
// never gets here, so "was the upstream called" is the primary assertion.
type gateHarness struct {
	handler *Handler
	hits    int
}

func newGateHarness(t *testing.T, tweak func(*config.Settings)) *gateHarness {
	t.Helper()
	h := &gateHarness{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.hits++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	set := config.DefaultSettings()
	set.UpstreamBaseURL = upstream.URL
	if tweak != nil {
		tweak(&set)
	}
	h.handler = newTestHandler(t, set)
	return h
}

// do issues one request from a fixed source address. GET is used by default
// because non-write methods are exactly what the gateway forwards without
// inspection today — so if the gate were sited after the method check, a
// scanner's GET would still sail through and these tests would catch it.
func (g *gateHarness) do(method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	g.handler.ServeHTTP(rec, req)
	return rec
}

// TestPathGateRejectsScannerPaths is the core case for the feature: a scanner's
// probe is answered with a bare 404 and never reaches the upstream.
func TestPathGateRejectsScannerPaths(t *testing.T) {
	for _, path := range []string{"/wp-login.php", "/.env", "/actuator/env", "/phpmyadmin/", "/.git/config"} {
		t.Run(path, func(t *testing.T) {
			g := newGateHarness(t, func(s *config.Settings) { s.PathAllowlistEnabled = true })

			rec := g.do(http.MethodGet, path)

			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
			if g.hits != 0 {
				t.Errorf("upstream called %d times for %q, want 0", g.hits, path)
			}
			// The response must not confirm that a filter is present.
			if v := rec.Header().Get("X-JEV-Gateway"); v != "" {
				t.Errorf("X-JEV-Gateway = %q, want it absent so the gateway is not advertised", v)
			}
			if !strings.Contains(rec.Body.String(), "not_found") {
				t.Errorf("body = %q, want an upstream-shaped not_found error", rec.Body.String())
			}
			// The trace id is what lets an operator find the row; keep it.
			if rec.Header().Get(TraceHeader) == "" {
				t.Error("missing trace header on a path-gate rejection")
			}
		})
	}
}

// TestPathGateRecordsAuditRow checks the rejection is diagnosable: kind "path",
// decision "block", and no inspect phase because the request never got that far.
func TestPathGateRecordsAuditRow(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) { s.PathAllowlistEnabled = true })
	g.do(http.MethodGet, "/wp-login.php")

	rows, _, err := g.handler.store.QueryLogs(config.LogFilter{})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.Kind != "path" {
		t.Errorf("kind = %q, want path", row.Kind)
	}
	if row.Decision != "block" {
		t.Errorf("decision = %q, want block", row.Decision)
	}
	if row.Path != "/wp-login.php" {
		t.Errorf("path = %q, want /wp-login.php", row.Path)
	}
	if row.InspectMS != nil {
		t.Errorf("inspect_ms = %d, want nil (the request was rejected before the inspect phase)", *row.InspectMS)
	}
	if row.Score != nil {
		t.Errorf("score = %v, want nil (no JEV call was made)", *row.Score)
	}
}

// TestPathGateOffIsTodayBehavior is the upgrade guard: with the gate off — the
// default — an unknown path is still forwarded, so deploying this change cannot
// start rejecting traffic on its own.
func TestPathGateOffIsTodayBehavior(t *testing.T) {
	g := newGateHarness(t, nil) // DefaultSettings: gate off

	rec := g.do(http.MethodGet, "/wp-login.php")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (forwarded)", rec.Code)
	}
	if g.hits != 1 {
		t.Errorf("upstream hits = %d, want 1", g.hits)
	}
}

// TestPathGateAllowsTheApiSurface is the false-positive guard: everything the
// gateway legitimately serves must still get through, from both halves of the
// allowlist — the extraction table and the configured prefixes.
func TestPathGateAllowsTheApiSurface(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) { s.PathAllowlistEnabled = true })

	for _, path := range []string{
		// Covered by the operator's default prefixes.
		"/v1/chat/completions",
		"/v1/models",
		"/v1/audio/transcriptions",
		"/v1beta/models/gemini-pro:generateContent",
		// Covered by the extraction table even though no default prefix matches:
		// this is the half that makes a forgotten prefix non-fatal.
		"/openai/deployments/x/chat/completions",
	} {
		t.Run(path, func(t *testing.T) {
			before := g.hits
			rec := g.do(http.MethodGet, path)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("%q was rejected by the path gate", path)
			}
			if g.hits != before+1 {
				t.Errorf("upstream hits = %d, want %d (%q must be forwarded)", g.hits, before+1, path)
			}
		})
	}
}

// TestPathGateAppliesToUnknownWritePath covers the other direction from the
// scanner case: a POST to an unknown path. Without the gate this is the request
// that fell through to genericText, got scored, and was then proxied upstream.
func TestPathGateAppliesToUnknownWritePath(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) { s.PathAllowlistEnabled = true })

	req := httptest.NewRequest(http.MethodPost, "/xmlrpc.php", strings.NewReader(`{"model":"m"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	g.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if g.hits != 0 {
		t.Errorf("upstream hits = %d, want 0", g.hits)
	}
}

// TestPathGateCustomPrefixes verifies the operator-owned half of the allowlist:
// a prefix added in the console opens that path with no code change.
func TestPathGateCustomPrefixes(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) {
		s.PathAllowlistEnabled = true
		s.PathAllowlistPrefixes = "/v1/,/internal/llm/"
	})

	if rec := g.do(http.MethodGet, "/internal/llm/whatever"); rec.Code == http.StatusNotFound {
		t.Error("a custom prefix was not honored")
	}
	if rec := g.do(http.MethodGet, "/wp-login.php"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d for a scanner path, want 404", rec.Code)
	}
}

// TestPathGateIgnoresIPAllowlist pins the deliberate interaction with the IP
// allowlist. Per the allowlist contract it only exempts IP-based bans; like
// content filtering, the path gate is a property of the request rather than of
// the sender, so a trusted source is still gated. An operator who needs an
// exotic path adds a prefix instead.
func TestPathGateIgnoresIPAllowlist(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) { s.PathAllowlistEnabled = true })
	if _, err := g.handler.store.AddIPRule("192.0.2.1", config.IPRuleAllow, "trusted", nil); err != nil {
		t.Fatalf("AddIPRule: %v", err)
	}

	rec := g.do(http.MethodGet, "/wp-login.php")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (the IP allowlist must not open the path gate)", rec.Code)
	}
	if g.hits != 0 {
		t.Errorf("upstream hits = %d, want 0", g.hits)
	}
}
