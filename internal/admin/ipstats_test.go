package admin

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jev-safety-gateway/internal/config"
)

// stubBans is a fixed BanSnapshot: it hands back whatever bans the test planted,
// standing in for *abuse.Tracker without pulling in the abuse package.
type stubBans struct{ m map[string]time.Time }

func (s stubBans) Snapshot(time.Time) map[string]time.Time { return s.m }

func newIPHandler(t *testing.T, bans BanSnapshot) (*Handler, *config.Store) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetAdminPasswordPlain("secret123"); err != nil {
		t.Fatalf("set admin password: %v", err)
	}
	return New(store, testFS(), nil, bans), store
}

// Per-IP analytics expose per-client traffic volume and block history, so the
// endpoint must stay behind auth — like /api/stats/latency, unlike /api/version.
func TestIPStatsRequiresToken(t *testing.T) {
	h, _ := newIPHandler(t, nil)
	if w := getWithToken(t, h, "/api/stats/ip", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/stats/ip without a token = %d, want 401", w.Code)
	}
}

// With a token the endpoint returns the window summary, the per-IP rows, and the
// live bans read from the tracker — sorted latest-expiry first, rendered as a
// non-null list.
func TestIPStatsReturnsSummaryItemsAndBans(t *testing.T) {
	now := time.Now()
	soon := now.Add(10 * time.Second)
	later := now.Add(300 * time.Second)
	h, store := newIPHandler(t, stubBans{m: map[string]time.Time{
		"1.1.1.1": soon,
		"2.2.2.2": later,
	}})
	token := loginToken(t, h)

	store.AddLog(config.LogEntry{TS: now.Add(-time.Minute), IP: "9.9.9.9", Decision: "block", Score: ptr(0.05)})
	store.AddLog(config.LogEntry{TS: now.Add(-time.Minute), IP: "9.9.9.9", Decision: "block", Score: ptr(0.05)})

	w := getWithToken(t, h, "/api/stats/ip?hours=6", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/ip = %d, body = %s", w.Code, w.Body.String())
	}

	var out struct {
		DistinctIPs int64           `json:"distinct_ips"`
		Hours       int             `json:"hours"`
		Items       []config.IPStat `json:"items"`
		Bans        []banEntry      `json:"bans"`
		BanCount    int             `json:"ban_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode ip stats: %v (%s)", err, w.Body.String())
	}

	if out.Hours != 6 {
		t.Errorf("hours = %d, want 6 (echoes the window selector)", out.Hours)
	}
	if out.DistinctIPs != 1 || len(out.Items) != 1 || out.Items[0].IP != "9.9.9.9" {
		t.Errorf("items/summary = %+v, want the single logged IP 9.9.9.9", out)
	}

	if out.BanCount != 2 || len(out.Bans) != 2 {
		t.Fatalf("ban_count/len = %d/%d, want 2/2 (bans: %+v)", out.BanCount, len(out.Bans), out.Bans)
	}
	// Latest expiry first.
	if out.Bans[0].IP != "2.2.2.2" || out.Bans[1].IP != "1.1.1.1" {
		t.Errorf("ban order = [%s, %s], want [2.2.2.2, 1.1.1.1] (latest expiry first)", out.Bans[0].IP, out.Bans[1].IP)
	}
	if out.Bans[0].Until != later.UnixMilli() {
		t.Errorf("bans[0].until = %d, want %d", out.Bans[0].Until, later.UnixMilli())
	}
	// RemainSec is floored at 0 and counts down from now; allow slack for elapsed time.
	if r := out.Bans[0].RemainSec; r <= 0 || r > 300 {
		t.Errorf("bans[0].remain_sec = %d, want (0, 300]", r)
	}
}

// A handler wired with no tracker (tests, or omitted wiring) reports an empty ban
// list as [] rather than null, and never panics.
func TestIPStatsNilTrackerYieldsEmptyList(t *testing.T) {
	h, _ := newIPHandler(t, nil)
	token := loginToken(t, h)

	w := getWithToken(t, h, "/api/stats/ip", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/ip = %d, body = %s", w.Code, w.Body.String())
	}
	// The null-vs-[] guarantee, checked both on the wire and after decode.
	if body := w.Body.String(); !strings.Contains(body, `"bans":[]`) {
		t.Errorf("response should carry an empty bans array, got %s", body)
	}
	var out struct {
		Bans     []banEntry `json:"bans"`
		BanCount int        `json:"ban_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Bans == nil {
		t.Error("bans decoded as nil; want a non-nil empty slice")
	}
	if out.BanCount != 0 {
		t.Errorf("ban_count = %d, want 0", out.BanCount)
	}
}
