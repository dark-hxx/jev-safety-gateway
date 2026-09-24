package config

import (
	"math"
	"testing"
	"time"
)

// approxEq compares two danger ratings; the arithmetic is deterministic but
// floating-point, so an exact == would be brittle.
func approxEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// addIPRows seeds n identical audit rows for one IP. Decision and score are
// independent here on purpose: IPStats derives danger from the columns, not from
// any allow/block-vs-score consistency, and the test needs to pin each factor.
func addIPRows(s *Store, ip string, n int, decision string, score *float64, ts time.Time, reason string) {
	for i := 0; i < n; i++ {
		s.AddLog(LogEntry{TS: ts, IP: ip, Decision: decision, Score: score, Reason: reason})
	}
}

// The danger dataset is hand-computed so every assertion has a closed form:
//
//	danger = max(0,1-avg) * blocked/scored * min(1, total/40)
//
// crit 203.0.113.10: 40 blocks @0.1  -> 0.9 * 1.0 * 1.0  = 0.90  (crit)
// high 203.0.113.20: 16 block+4 allow @0.0, total 20 -> 1 * 0.8 * 0.5  = 0.40 (high)
// med  203.0.113.30:  8 block+2 allow @0.0, total 10 -> 1 * 0.8 * 0.25 = 0.20 (med)
// low  203.0.113.40:  4 blocks @0.0,        total  4 -> 1 * 1.0 * 0.10 = 0.10 (low, blocked>0)
// zero 203.0.113.50:  5 skips, no score               -> scored 0      = 0.00 (low, no evidence)
//
// Rows with an empty IP and rows before the window must not appear at all.
func TestIPStatsDangerRankingLevelsAndSummary(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	since := now.Add(-time.Hour)
	inWin := now.Add(-10 * time.Second)

	// crit: 39 rows with a stale reason, then the newest carrying a sentinel so
	// LastReason must resolve to the most recent row, not an arbitrary one.
	addIPRows(s, "203.0.113.10", 39, "block", f(0.1), inWin, "crit-old")
	s.AddLog(LogEntry{TS: inWin, IP: "203.0.113.10", Decision: "block", Score: f(0.1), Reason: "crit-latest"})

	addIPRows(s, "203.0.113.20", 16, "block", f(0.0), inWin, "high")
	addIPRows(s, "203.0.113.20", 4, "allow", f(0.0), inWin, "high")

	addIPRows(s, "203.0.113.30", 8, "block", f(0.0), inWin, "med")
	addIPRows(s, "203.0.113.30", 2, "allow", f(0.0), inWin, "med")

	// low: three stale rows, then a row sharing the same ts but added last. The
	// newest-by-id tie-break (not ts) must pick the sentinel.
	addIPRows(s, "203.0.113.40", 3, "block", f(0.0), inWin, "low-old")
	s.AddLog(LogEntry{TS: inWin, IP: "203.0.113.40", Decision: "block", Score: f(0.0), Reason: "low-latest"})

	addIPRows(s, "203.0.113.50", 5, "skip", nil, inWin, "skip")

	// Noise that must be excluded: rows with no IP, and an out-of-window row for
	// an otherwise-dangerous IP.
	addIPRows(s, "", 3, "block", f(0.0), inWin, "no-ip")
	s.AddLog(LogEntry{TS: since.Add(-time.Hour), IP: "198.51.100.9", Decision: "block", Score: f(0.0), Reason: "too-old"})

	items, summary, _, _, err := s.IPStats(since, 0, nil)
	if err != nil {
		t.Fatalf("IPStats: %v", err)
	}

	// Excluded rows leave exactly the five in-window, non-empty IPs.
	if len(items) != 5 {
		t.Fatalf("got %d IPs, want 5 (empty IP and out-of-window rows must be dropped): %+v", len(items), items)
	}
	for _, it := range items {
		if it.IP == "" {
			t.Fatalf("empty-IP row leaked into results: %+v", it)
		}
		if it.IP == "198.51.100.9" {
			t.Fatalf("out-of-window IP leaked into results: %+v", it)
		}
	}

	// Ranking: most dangerous first.
	wantOrder := []struct {
		ip     string
		danger float64
		level  string
	}{
		{"203.0.113.10", 0.90, "crit"},
		{"203.0.113.20", 0.40, "high"},
		{"203.0.113.30", 0.20, "med"},
		{"203.0.113.40", 0.10, "low"},
		{"203.0.113.50", 0.00, "low"},
	}
	for i, w := range wantOrder {
		got := items[i]
		if got.IP != w.ip {
			t.Errorf("items[%d].IP = %q, want %q (danger order)", i, got.IP, w.ip)
		}
		if !approxEq(got.Danger, w.danger) {
			t.Errorf("%s danger = %v, want %v", w.ip, got.Danger, w.danger)
		}
		if got.Level != w.level {
			t.Errorf("%s level = %q, want %q", w.ip, got.Level, w.level)
		}
	}

	// Newest-row reason, including the same-ts / higher-id tie-break for the low IP.
	byIP := map[string]IPStat{}
	for _, it := range items {
		byIP[it.IP] = it
	}
	if got := byIP["203.0.113.10"].LastReason; got != "crit-latest" {
		t.Errorf("crit LastReason = %q, want %q", got, "crit-latest")
	}
	if got := byIP["203.0.113.40"].LastReason; got != "low-latest" {
		t.Errorf("low LastReason = %q, want %q (MAX(id) tie-break, not ts)", got, "low-latest")
	}

	// Scored aggregates: crit carries avg/min; the skip-only IP must report neither.
	crit := byIP["203.0.113.10"]
	if crit.Scored != 40 || crit.Blocked != 40 || crit.Total != 40 {
		t.Errorf("crit counts total/scored/blocked = %d/%d/%d, want 40/40/40", crit.Total, crit.Scored, crit.Blocked)
	}
	if crit.AvgScore == nil || !approxEq(*crit.AvgScore, 0.1) {
		t.Errorf("crit AvgScore = %v, want 0.1", crit.AvgScore)
	}
	if crit.MinScore == nil || !approxEq(*crit.MinScore, 0.1) {
		t.Errorf("crit MinScore = %v, want 0.1", crit.MinScore)
	}
	zero := byIP["203.0.113.50"]
	if zero.Scored != 0 {
		t.Errorf("skip-only Scored = %d, want 0", zero.Scored)
	}
	if zero.AvgScore != nil || zero.MinScore != nil {
		t.Errorf("skip-only avg/min = %v/%v, want nil/nil (no scored rows)", zero.AvgScore, zero.MinScore)
	}

	// Summary is computed over every in-window IP, not just the returned slice.
	if summary.DistinctIPs != 5 {
		t.Errorf("DistinctIPs = %d, want 5", summary.DistinctIPs)
	}
	if summary.Crit != 1 {
		t.Errorf("Crit = %d, want 1", summary.Crit)
	}
	if summary.High != 1 {
		t.Errorf("High = %d, want 1", summary.High)
	}
	if summary.BlockedIPs != 4 {
		t.Errorf("BlockedIPs = %d, want 4 (skip-only IP has no blocks)", summary.BlockedIPs)
	}
	if want := int64(40 + 16 + 8 + 4); summary.TotalBlocked != want {
		t.Errorf("TotalBlocked = %d, want %d", summary.TotalBlocked, want)
	}
	if want := int64(40 + 20 + 10 + 4 + 5); summary.TotalEvents != want {
		t.Errorf("TotalEvents = %d, want %d", summary.TotalEvents, want)
	}
}

// A hostile ?limit= truncates the returned list but must not shrink the
// window-wide summary: the console still needs accurate distinct/blocked totals.
func TestIPStatsLimitTruncatesListNotSummary(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	since := now.Add(-time.Hour)
	inWin := now.Add(-time.Second)

	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		s.AddLog(LogEntry{TS: inWin, IP: ip, Decision: "block", Score: f(0.0)})
	}

	items, summary, _, _, err := s.IPStats(since, 2, nil)
	if err != nil {
		t.Fatalf("IPStats: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("len(items) = %d, want 2 (limit honoured)", len(items))
	}
	if summary.DistinctIPs != 3 {
		t.Errorf("DistinctIPs = %d, want 3 (summary spans all IPs, not just the page)", summary.DistinctIPs)
	}

	// limit<=0 falls back to the default and returns everything present here.
	all, _, _, _, err := s.IPStats(since, 0, nil)
	if err != nil {
		t.Fatalf("IPStats(limit=0): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len(items) with limit=0 = %d, want 3 (default limit)", len(all))
	}
}

// ipDanger and dangerLevel are pure functions; pin their edges without a DB.
func TestIPDangerAndLevelUnit(t *testing.T) {
	danger := []struct {
		name string
		st   IPStat
		want float64
	}{
		{"no scored rows -> 0", IPStat{Total: 100, Blocked: 100, Scored: 0, AvgScore: f(0.0)}, 0},
		{"nil avg -> 0", IPStat{Total: 100, Blocked: 100, Scored: 100, AvgScore: nil}, 0},
		{"avg>1 clamps unsafety to 0", IPStat{Total: 40, Blocked: 40, Scored: 40, AvgScore: f(1.2)}, 0},
		{"full volume, all blocked, avg 0.1", IPStat{Total: 40, Blocked: 40, Scored: 40, AvgScore: f(0.1)}, 0.9},
		{"blockRate over scored not total", IPStat{Total: 80, Blocked: 40, Scored: 40, AvgScore: f(0.0)}, 1.0},
		{"low volume suppressed", IPStat{Total: 4, Blocked: 4, Scored: 4, AvgScore: f(0.0)}, 0.1},
	}
	for _, c := range danger {
		if got := ipDanger(c.st); !approxEq(got, c.want) {
			t.Errorf("ipDanger(%s) = %v, want %v", c.name, got, c.want)
		}
	}

	level := []struct {
		d    float64
		want string
	}{
		{0.55, "crit"}, {0.5499, "high"},
		{0.30, "high"}, {0.2999, "med"},
		{0.12, "med"}, {0.1199, "low"},
		{0, "low"},
	}
	for _, c := range level {
		if got := dangerLevel(c.d); got != c.want {
			t.Errorf("dangerLevel(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// fakeGeo is a deterministic GeoResolver for the aggregation tests: it maps a few
// public IPs to fixed country/ASN attributions and returns ok=false for anything
// else (private ranges, unknown IPs), exactly like the real resolver does for an
// IP neither database can attribute. enableCountry/enableASN model an operator who
// configured only one of the two databases.
type fakeGeo struct {
	enableCountry bool
	enableASN     bool
	table         map[string]GeoInfo
}

func (g fakeGeo) CountryEnabled() bool { return g.enableCountry }
func (g fakeGeo) ASNEnabled() bool     { return g.enableASN }
func (g fakeGeo) Lookup(ip string) (GeoInfo, bool) {
	info, ok := g.table[ip]
	return info, ok
}

// GatewayLocation is always unset here: the deployment coordinate is not part of
// the IP aggregation IPStats performs, so the aggregation tests have no reason to
// declare one. (The admin handler test covers the response field.)
func (g fakeGeo) GatewayLocation() (float64, float64, bool) { return 0, 0, false }

// With a resolver wired, IPStats must fill each IP's country/ASN and return the
// country and ASN buckets aggregated over the window, volume-sorted and top-N
// capped. Unattributed IPs fall into the ISO="" / ASN=0 bucket without polluting
// the named rankings, and a resolver with only one dimension enabled returns only
// that dimension's buckets.
func TestIPStatsGeoAggregation(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	since := now.Add(-time.Hour)
	inWin := now.Add(-10 * time.Second)

	// Two US IPs (same country, different ASN), one DE IP, one private IP the
	// resolver cannot attribute.
	addIPRows(s, "203.0.113.10", 6, "block", f(0.1), inWin, "us-a")
	addIPRows(s, "203.0.113.11", 4, "allow", f(0.9), inWin, "us-b")
	addIPRows(s, "198.51.100.20", 5, "block", f(0.2), inWin, "de")
	addIPRows(s, "10.0.0.5", 3, "allow", f(0.9), inWin, "private")

	geo := fakeGeo{
		enableCountry: true,
		enableASN:     true,
		table: map[string]GeoInfo{
			"203.0.113.10":  {CountryISO: "US", CountryName: "United States", ASN: 64500, ASNOrg: "Alpha"},
			"203.0.113.11":  {CountryISO: "US", CountryName: "United States", ASN: 64501, ASNOrg: "Beta"},
			"198.51.100.20": {CountryISO: "DE", CountryName: "Germany", ASN: 64500, ASNOrg: "Alpha"},
			// 10.0.0.5 intentionally absent -> ok=false.
		},
	}

	items, _, geoBuckets, asnBuckets, err := s.IPStats(since, 0, geo)
	if err != nil {
		t.Fatalf("IPStats: %v", err)
	}

	// Per-IP enrichment is filled from the resolver; the unattributed IP stays blank.
	byIP := map[string]IPStat{}
	for _, it := range items {
		byIP[it.IP] = it
	}
	if it := byIP["203.0.113.10"]; it.Country != "US" || it.ASN != 64500 || it.ASNOrg != "Alpha" {
		t.Errorf("203.0.113.10 enrichment = %q/%d/%q, want US/64500/Alpha", it.Country, it.ASN, it.ASNOrg)
	}
	if it := byIP["10.0.0.5"]; it.Country != "" || it.ASN != 0 {
		t.Errorf("private IP enrichment = %q/%d, want empty/0 (unattributed)", it.Country, it.ASN)
	}

	// Country buckets: US = 6+4 events / 6 blocks, DE = 5/5, unknown ("") = 3/0.
	// Volume-sorted: US (10) > DE (5) > unknown (3).
	if len(geoBuckets) != 3 {
		t.Fatalf("got %d country buckets, want 3 (US, DE, unknown): %+v", len(geoBuckets), geoBuckets)
	}
	if b := geoBuckets[0]; b.Country != "US" || b.Total != 10 || b.Blocked != 6 {
		t.Errorf("country[0] = %+v, want US total=10 blocked=6", b)
	}
	if b := geoBuckets[1]; b.Country != "DE" || b.Total != 5 || b.Blocked != 5 {
		t.Errorf("country[1] = %+v, want DE total=5 blocked=5", b)
	}
	if b := geoBuckets[2]; b.Country != "" || b.Total != 3 {
		t.Errorf("country[2] = %+v, want unknown total=3", b)
	}

	// ASN buckets: 64500 = 6(US-a)+5(DE) events / 11 total 11 blocks, 64501 = 4/0,
	// 0 (unattributed) = 3/0. Volume-sorted: 64500 (11) > 64501 (4) > 0 (3).
	if len(asnBuckets) != 3 {
		t.Fatalf("got %d ASN buckets, want 3: %+v", len(asnBuckets), asnBuckets)
	}
	if b := asnBuckets[0]; b.ASN != 64500 || b.Total != 11 || b.Blocked != 11 {
		t.Errorf("asn[0] = %+v, want 64500 total=11 blocked=11", b)
	}
	if b := asnBuckets[1]; b.ASN != 64501 || b.Total != 4 || b.Blocked != 0 {
		t.Errorf("asn[1] = %+v, want 64501 total=4 blocked=0", b)
	}
	if b := asnBuckets[2]; b.ASN != 0 || b.Total != 3 {
		t.Errorf("asn[2] = %+v, want unattributed ASN=0 total=3", b)
	}
}

// A resolver with only the country database enabled must return country buckets
// but a nil ASN slice (so the API omits that panel), and vice versa.
func TestIPStatsGeoSingleDimension(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	since := now.Add(-time.Hour)
	inWin := now.Add(-10 * time.Second)
	addIPRows(s, "203.0.113.10", 3, "block", f(0.1), inWin, "us")

	geo := fakeGeo{
		enableCountry: true,
		enableASN:     false,
		table:         map[string]GeoInfo{"203.0.113.10": {CountryISO: "US", CountryName: "United States", ASN: 64500}},
	}
	_, _, geoBuckets, asnBuckets, err := s.IPStats(since, 0, geo)
	if err != nil {
		t.Fatalf("IPStats: %v", err)
	}
	if len(geoBuckets) != 1 {
		t.Errorf("country enabled: got %d country buckets, want 1", len(geoBuckets))
	}
	if asnBuckets != nil {
		t.Errorf("ASN disabled: asnBuckets = %+v, want nil", asnBuckets)
	}
}

// With no resolver (nil), IPStats returns no buckets and leaves per-IP geo fields
// blank — the pre-Phase-3 behaviour, so an install without GeoIP is unaffected.
func TestIPStatsNilGeo(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	since := now.Add(-time.Hour)
	inWin := now.Add(-10 * time.Second)
	addIPRows(s, "203.0.113.10", 3, "block", f(0.1), inWin, "us")

	items, _, geoBuckets, asnBuckets, err := s.IPStats(since, 0, nil)
	if err != nil {
		t.Fatalf("IPStats: %v", err)
	}
	if geoBuckets != nil || asnBuckets != nil {
		t.Errorf("nil geo: buckets = %+v/%+v, want nil/nil", geoBuckets, asnBuckets)
	}
	if len(items) == 1 && (items[0].Country != "" || items[0].ASN != 0) {
		t.Errorf("nil geo: per-IP enrichment = %q/%d, want empty/0", items[0].Country, items[0].ASN)
	}
}
