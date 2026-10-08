package proxy

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"jev-safety-gateway/internal/config"
)

// strikeSettings turns the gate on together with the opt-in that makes a
// path-guessing source accrue abuse strikes.
func strikeSettings(s *config.Settings) {
	s.PathAllowlistEnabled = true
	s.AbuseCountUnknownPath = true
	s.AbuseEnabled = true
	s.AbuseWindowSec = 60
	s.AbuseMaxHarmful = 3
	s.AbuseBanSec = 300
}

// TestPathGateStrikesUnknownPaths verifies the third layer: a source that keeps
// guessing paths is banned like a source that keeps sending harmful content.
func TestPathGateStrikesUnknownPaths(t *testing.T) {
	g := newGateHarness(t, strikeSettings)

	// The first maxHarmful-1 hits are plain rejections.
	for i := 1; i < 3; i++ {
		rec := g.do(http.MethodGet, "/wp-login.php")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("hit %d: status = %d, want 404", i, rec.Code)
		}
		if banned, _ := g.handler.abuse.Banned("192.0.2.1", time.Now()); banned {
			t.Fatalf("hit %d banned the IP early", i)
		}
	}

	// The hit that reaches the threshold still gets a 404 — it was rejected by
	// the gate, not by the ban — but the ban is recorded on the row.
	rec := g.do(http.MethodGet, "/wp-login.php")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("threshold hit: status = %d, want 404", rec.Code)
	}
	if banned, _ := g.handler.abuse.Banned("192.0.2.1", time.Now()); !banned {
		t.Fatal("the threshold hit did not ban the IP")
	}

	// And from here the abuse fast path answers first, so guessing is cheap to
	// reject and the source learns nothing new.
	rec = g.do(http.MethodGet, "/wp-login.php")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status after the ban = %d, want 429", rec.Code)
	}
	if v := rec.Header().Get("X-JEV-Gateway"); v != "banned" {
		t.Errorf("X-JEV-Gateway = %q, want banned once the IP is on the abuse list", v)
	}
	if g.hits != 0 {
		t.Errorf("upstream hits = %d, want 0", g.hits)
	}
}

// TestPathGateStrikeIsRecordedOnTheRow checks the operator can see why a source
// was banned, since the rejection status alone (404) does not say it.
func TestPathGateStrikeIsRecordedOnTheRow(t *testing.T) {
	g := newGateHarness(t, strikeSettings)
	for i := 0; i < 3; i++ {
		g.do(http.MethodGet, "/wp-login.php")
	}

	rows, _, err := g.handler.store.QueryLogs(config.LogFilter{})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	marked := false
	for _, row := range rows {
		if strings.Contains(row.Reason, "已触发滥用封禁") {
			marked = true
		}
	}
	if !marked {
		t.Errorf("no audit row carries the ban reason; rows = %+v", rows)
	}
}

// TestPathGateStrikeOffByDefault is the false-positive guard for the third
// layer: without the opt-in, even a run of rejections never bans anyone, so
// turning on the gate alone cannot hurt a client that is merely probing an
// endpoint the operator forgot to allow.
func TestPathGateStrikeOffByDefault(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) {
		s.PathAllowlistEnabled = true
		// AbuseCountUnknownPath stays at its default (false).
		s.AbuseEnabled = true
		s.AbuseMaxHarmful = 3
	})

	for i := 0; i < 10; i++ {
		rec := g.do(http.MethodGet, "/wp-login.php")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("hit %d: status = %d, want 404 (never 429)", i+1, rec.Code)
		}
	}
	if banned, _ := g.handler.abuse.Banned("192.0.2.1", time.Now()); banned {
		t.Error("the IP was banned with AbuseCountUnknownPath off")
	}
}

// TestUnknownPathStrikeIsInertWithoutTheGate documents that the opt-in means
// nothing on its own — with the gate off there is no rejection to count, so
// enabling it by accident cannot ban anyone.
func TestUnknownPathStrikeIsInertWithoutTheGate(t *testing.T) {
	g := newGateHarness(t, func(s *config.Settings) {
		s.PathAllowlistEnabled = false // the gate is off...
		s.AbuseCountUnknownPath = true // ...so this has nothing to count
		s.AbuseEnabled = true
		s.AbuseMaxHarmful = 3
	})

	for i := 0; i < 10; i++ {
		if rec := g.do(http.MethodGet, "/wp-login.php"); rec.Code != http.StatusOK {
			t.Fatalf("hit %d: status = %d, want 200 (forwarded)", i+1, rec.Code)
		}
	}
	if banned, _ := g.handler.abuse.Banned("192.0.2.1", time.Now()); banned {
		t.Error("the IP was banned by an inert setting")
	}
}

// TestPathGateStrikeSparesAllowlistedIPs follows the same rule as the
// harmful-content site: a trusted source is gated (see
// TestPathGateIgnoresIPAllowlist) but does not accumulate strikes from it.
func TestPathGateStrikeSparesAllowlistedIPs(t *testing.T) {
	g := newGateHarness(t, strikeSettings)
	if _, err := g.handler.store.AddIPRule("192.0.2.1", config.IPRuleAllow, "trusted", nil); err != nil {
		t.Fatalf("AddIPRule: %v", err)
	}

	for i := 0; i < 10; i++ {
		if rec := g.do(http.MethodGet, "/wp-login.php"); rec.Code != http.StatusNotFound {
			t.Fatalf("hit %d: status = %d, want 404", i+1, rec.Code)
		}
	}
	if banned, _ := g.handler.abuse.Banned("192.0.2.1", time.Now()); banned {
		t.Error("an allowlisted source accrued path strikes")
	}
}
