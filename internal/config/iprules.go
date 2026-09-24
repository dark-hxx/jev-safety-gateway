package config

// Persisted IP access rules: manual bans (temporary or permanent), CIDR blocks,
// and allowlist entries. This is the durable counterpart to the in-memory abuse
// tracker (internal/abuse). The proxy consults both before calling JEV; an allow
// rule wins over every ban (persisted or automatic). Matching runs on the hot
// request path, so parsed rules are cached in memory (guarded by Store.mu, like
// the settings cache) and refreshed on every mutation.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const (
	// IPRuleBlock rejects matching requests with 429 before any JEV call.
	IPRuleBlock = "block"
	// IPRuleAllow exempts matching IPs from all IP-based bans (persisted and
	// abuse) and from abuse strike accrual — but not from content filtering.
	IPRuleAllow = "allow"
)

// parsedRule is the in-memory, match-ready form of an IPRule.
type parsedRule struct {
	prefix    netip.Prefix
	kind      string
	expiresMS int64 // 0 = permanent
}

// CanonicalizeIPPattern validates a single IP or CIDR pattern and returns its
// canonical string form plus whether it is a CIDR. An IPv4-in-IPv6 address is
// unmapped so it matches a plain IPv4 rule.
func CanonicalizeIPPattern(pattern string) (canonical string, isCIDR bool, err error) {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return "", false, errors.New("ip pattern is required")
	}
	if strings.Contains(p, "/") {
		pre, err := netip.ParsePrefix(p)
		if err != nil {
			return "", false, fmt.Errorf("invalid CIDR %q", pattern)
		}
		return pre.Masked().String(), true, nil
	}
	addr, err := netip.ParseAddr(p)
	if err != nil {
		return "", false, fmt.Errorf("invalid IP address %q", pattern)
	}
	return addr.Unmap().String(), false, nil
}

// patternPrefix turns a stored pattern into a netip.Prefix for containment tests.
// A single IP becomes a host prefix (/32 or /128).
func patternPrefix(pattern string, isCIDR bool) (netip.Prefix, error) {
	if isCIDR {
		return netip.ParsePrefix(pattern)
	}
	addr, err := netip.ParseAddr(pattern)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}

// reloadIPRules rebuilds the in-memory parsed-rule cache from the table. A row
// whose pattern no longer parses is skipped rather than breaking all matching.
func (s *Store) reloadIPRules() error {
	rows, err := s.db.Query(`SELECT pattern,is_cidr,kind,expires_at FROM ip_rules`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var parsed []parsedRule
	for rows.Next() {
		var pattern, kind string
		var isCIDR int
		var expires sql.NullInt64
		if err := rows.Scan(&pattern, &isCIDR, &kind, &expires); err != nil {
			return err
		}
		pre, err := patternPrefix(pattern, isCIDR == 1)
		if err != nil {
			continue
		}
		pr := parsedRule{prefix: pre, kind: kind}
		if expires.Valid {
			pr.expiresMS = expires.Int64
		}
		parsed = append(parsed, pr)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.ipRules = parsed
	s.mu.Unlock()
	return nil
}

// MatchIPRules reports how ip is matched by the persisted rules as of now. allow
// is true if any allow rule contains ip; block is true if any non-expired block
// rule contains ip. When several block rules match, blockUntil is the latest
// expiry and permanent is set if any matching block rule is permanent. The
// caller decides precedence (allow wins). An unparseable ip yields no match.
func (s *Store) MatchIPRules(ip string, now time.Time) (allow, block bool, blockUntil time.Time, permanent bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false, false, time.Time{}, false
	}
	addr = addr.Unmap()
	nowMS := now.UnixMilli()

	s.mu.RLock()
	rules := s.ipRules
	s.mu.RUnlock()

	for _, r := range rules {
		if !r.prefix.Contains(addr) {
			continue
		}
		if r.kind == IPRuleAllow {
			allow = true
			continue
		}
		// block rule: skip if it is a temporary ban that already expired.
		if r.expiresMS != 0 && nowMS >= r.expiresMS {
			continue
		}
		block = true
		if r.expiresMS == 0 {
			permanent = true
			continue
		}
		if t := time.UnixMilli(r.expiresMS); t.After(blockUntil) {
			blockUntil = t
		}
	}
	return allow, block, blockUntil, permanent
}

// ListIPRules returns all persisted rules in id order.
func (s *Store) ListIPRules() ([]IPRule, error) {
	rows, err := s.db.Query(
		`SELECT id,pattern,is_cidr,kind,expires_at,reason,created_at FROM ip_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPRule
	for rows.Next() {
		r, err := scanIPRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddIPRule inserts (or, for the same pattern+kind, refreshes) a rule. The
// pattern is validated and canonicalized; kind must be block or allow. An allow
// rule never expires, so expiresAt is ignored for it. The parsed-rule cache is
// refreshed before returning so the proxy sees the change immediately.
func (s *Store) AddIPRule(pattern, kind, reason string, expiresAt *time.Time) (IPRule, error) {
	canon, isCIDR, err := CanonicalizeIPPattern(pattern)
	if err != nil {
		return IPRule{}, err
	}
	if kind != IPRuleBlock && kind != IPRuleAllow {
		return IPRule{}, errors.New("kind must be block or allow")
	}
	if kind == IPRuleAllow {
		expiresAt = nil
	}
	var exp interface{}
	if expiresAt != nil {
		exp = expiresAt.UnixMilli()
	}
	cidr := 0
	if isCIDR {
		cidr = 1
	}
	if _, err := s.db.Exec(
		`INSERT INTO ip_rules(pattern,is_cidr,kind,expires_at,reason,created_at)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(pattern,kind) DO UPDATE SET
		   expires_at=excluded.expires_at, reason=excluded.reason, created_at=excluded.created_at`,
		canon, cidr, kind, exp, reason, time.Now().UnixMilli()); err != nil {
		return IPRule{}, err
	}
	if err := s.reloadIPRules(); err != nil {
		return IPRule{}, err
	}
	row := s.db.QueryRow(
		`SELECT id,pattern,is_cidr,kind,expires_at,reason,created_at FROM ip_rules WHERE pattern=? AND kind=?`,
		canon, kind)
	return scanIPRule(row)
}

// DeleteIPRule removes a rule by id and refreshes the cache.
func (s *Store) DeleteIPRule(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM ip_rules WHERE id=?`, id); err != nil {
		return err
	}
	return s.reloadIPRules()
}

// scanIPRule decodes one row from either *sql.Row or *sql.Rows.
func scanIPRule(sc interface{ Scan(...any) error }) (IPRule, error) {
	var r IPRule
	var isCIDR int
	var expires sql.NullInt64
	var created int64
	if err := sc.Scan(&r.ID, &r.Pattern, &isCIDR, &r.Kind, &expires, &r.Reason, &created); err != nil {
		return IPRule{}, err
	}
	r.IsCIDR = isCIDR == 1
	if expires.Valid {
		t := time.UnixMilli(expires.Int64)
		r.ExpiresAt = &t
	}
	r.CreatedAt = time.UnixMilli(created)
	return r, nil
}
