package config

import (
	"path/filepath"
	"testing"
	"time"
)

func openRuleStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCanonicalizeIPPattern(t *testing.T) {
	cases := []struct {
		in     string
		canon  string
		isCIDR bool
		ok     bool
	}{
		{"1.2.3.4", "1.2.3.4", false, true},
		{"  1.2.3.4  ", "1.2.3.4", false, true},
		{"10.0.0.0/8", "10.0.0.0/8", true, true},
		{"10.1.2.3/8", "10.0.0.0/8", true, true},     // host bits dropped by Masked()
		{"::ffff:1.2.3.4", "1.2.3.4", false, true},   // v4-in-v6 is unmapped
		{"2001:db8::/32", "2001:db8::/32", true, true},
		{"", "", false, false},
		{"not-an-ip", "", false, false},
		{"1.2.3.4/33", "", false, false},
	}
	for _, c := range cases {
		canon, isCIDR, err := CanonicalizeIPPattern(c.in)
		if c.ok != (err == nil) {
			t.Errorf("Canonicalize(%q) err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if !c.ok {
			continue
		}
		if canon != c.canon || isCIDR != c.isCIDR {
			t.Errorf("Canonicalize(%q) = (%q,%v), want (%q,%v)", c.in, canon, isCIDR, c.canon, c.isCIDR)
		}
	}
}

func TestIPRuleAddListDelete(t *testing.T) {
	s := openRuleStore(t)

	dur := time.Now().Add(time.Hour)
	r1, err := s.AddIPRule("1.2.3.4", IPRuleBlock, "manual", &dur)
	if err != nil {
		t.Fatalf("add block: %v", err)
	}
	if r1.ID == 0 || r1.Kind != IPRuleBlock || r1.IsCIDR || r1.ExpiresAt == nil {
		t.Fatalf("unexpected block rule: %+v", r1)
	}

	// An allow rule never expires: the expiry argument is ignored for it.
	r2, err := s.AddIPRule("10.0.0.0/8", IPRuleAllow, "trusted", &dur)
	if err != nil {
		t.Fatalf("add allow: %v", err)
	}
	if !r2.IsCIDR || r2.ExpiresAt != nil {
		t.Fatalf("allow rule should be a permanent CIDR: %+v", r2)
	}

	if rules, err := s.ListIPRules(); err != nil || len(rules) != 2 {
		t.Fatalf("list = %+v (err %v), want 2 rules", rules, err)
	}

	// Re-adding the same pattern+kind upserts rather than duplicating.
	if _, err := s.AddIPRule("1.2.3.4", IPRuleBlock, "again", nil); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if rules, _ := s.ListIPRules(); len(rules) != 2 {
		t.Fatalf("after upsert len = %d, want 2 (no duplicate)", len(rules))
	}

	if err := s.DeleteIPRule(r1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rules, _ := s.ListIPRules(); len(rules) != 1 || rules[0].ID != r2.ID {
		t.Fatalf("after delete = %+v, want only the allow rule", rules)
	}

	if _, err := s.AddIPRule("1.2.3.4", "nope", "", nil); err == nil {
		t.Error("expected error for invalid kind")
	}
	if _, err := s.AddIPRule("bad", IPRuleBlock, "", nil); err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestMatchIPRules(t *testing.T) {
	s := openRuleStore(t)
	now := time.Now()

	mustAdd := func(pattern, kind string, exp *time.Time) {
		t.Helper()
		if _, err := s.AddIPRule(pattern, kind, "", exp); err != nil {
			t.Fatalf("add %s %s: %v", kind, pattern, err)
		}
	}
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	mustAdd("1.2.3.4", IPRuleBlock, nil)              // permanent single-IP block
	mustAdd("10.0.0.0/8", IPRuleBlock, &past)         // expired temporary CIDR block
	mustAdd("192.168.0.0/16", IPRuleBlock, &future)   // active temporary CIDR block
	mustAdd("192.168.1.1", IPRuleAllow, nil)          // allowlist inside the active block

	t.Run("permanent single ip", func(t *testing.T) {
		allow, block, _, perm := s.MatchIPRules("1.2.3.4", now)
		if allow || !block || !perm {
			t.Errorf("got allow=%v block=%v perm=%v, want false,true,true", allow, block, perm)
		}
	})
	t.Run("expired temporary block is ignored", func(t *testing.T) {
		allow, block, _, _ := s.MatchIPRules("10.0.0.5", now)
		if allow || block {
			t.Errorf("got allow=%v block=%v, want no match (block expired)", allow, block)
		}
	})
	t.Run("active cidr block", func(t *testing.T) {
		allow, block, until, perm := s.MatchIPRules("192.168.5.5", now)
		if allow || !block || perm {
			t.Errorf("got allow=%v block=%v perm=%v, want false,true,false", allow, block, perm)
		}
		if !until.After(now) {
			t.Errorf("blockUntil = %v, want after now", until)
		}
	})
	t.Run("allow set even when an active block overlaps", func(t *testing.T) {
		allow, _, _, _ := s.MatchIPRules("192.168.1.1", now)
		if !allow {
			t.Error("got allow=false, want true (the proxy gives allow precedence)")
		}
	})
	t.Run("unmatched ip", func(t *testing.T) {
		allow, block, _, _ := s.MatchIPRules("8.8.8.8", now)
		if allow || block {
			t.Errorf("got allow=%v block=%v, want no match", allow, block)
		}
	})
	t.Run("garbage ip yields no match", func(t *testing.T) {
		allow, block, _, _ := s.MatchIPRules("not-an-ip", now)
		if allow || block {
			t.Errorf("garbage ip matched: allow=%v block=%v", allow, block)
		}
	})
}
