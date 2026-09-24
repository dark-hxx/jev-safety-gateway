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

// stubGeo is a GeoResolver with both dimensions on and a fixed, optionally
// declared deployment coordinate. It stands in for *geoip.Resolver so the handler
// test can exercise the geo fields without MaxMind databases.
type stubGeo struct {
	lat, lon float64
	placed   bool
}

func (g stubGeo) Lookup(string) (config.GeoInfo, bool) { return config.GeoInfo{}, false }
func (g stubGeo) CountryEnabled() bool                 { return true }
func (g stubGeo) ASNEnabled() bool                     { return true }
func (g stubGeo) GatewayLocation() (float64, float64, bool) {
	return g.lat, g.lon, g.placed
}

func newIPHandlerWithGeo(t *testing.T, geo config.GeoResolver) *Handler {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "jev-safety-gateway.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetAdminPasswordPlain("secret123"); err != nil {
		t.Fatalf("set admin password: %v", err)
	}
	return New(store, testFS(), nil, nil, geo)
}

// decodeGateway pulls the gateway field out of an /api/stats/ip response, and
// reports whether the key was present at all — omitted and null are both "absent".
func decodeGateway(t *testing.T, h *Handler) (present bool, lat, lon float64) {
	t.Helper()
	w := getWithToken(t, h, "/api/stats/ip", loginToken(t, h))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/stats/ip = %d, body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Gateway *struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"gateway"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode ip stats: %v (%s)", err, w.Body.String())
	}
	if out.Gateway == nil {
		return false, 0, 0
	}
	return true, out.Gateway.Lat, out.Gateway.Lon
}

// A declared deployment coordinate is reported so the origin map can place its
// central node at the real location instead of a fabricated one.
func TestIPStatsReportsDeclaredGatewayLocation(t *testing.T) {
	h := newIPHandlerWithGeo(t, stubGeo{lat: 31.23, lon: 121.47, placed: true})
	present, lat, lon := decodeGateway(t, h)
	if !present {
		t.Fatal("gateway absent from the response, want it reported when declared")
	}
	if lat != 31.23 || lon != 121.47 {
		t.Errorf("gateway = %v,%v, want 31.23,121.47", lat, lon)
	}
}

// With no coordinate declared the field is omitted entirely (not 0,0), so the map
// draws origins only rather than pinning a hub on the Gulf of Guinea.
func TestIPStatsOmitsUndeclaredGatewayLocation(t *testing.T) {
	h := newIPHandlerWithGeo(t, stubGeo{placed: false})
	if present, lat, lon := decodeGateway(t, h); present {
		t.Errorf("gateway = %v,%v present, want the field omitted when undeclared", lat, lon)
	}
}

// The coordinate is independent of whether the client-IP dimensions are on: a
// resolver that placed the gateway but has no databases still reports it, and a
// nil resolver (no GeoIP at all) reports nothing for either.
func TestIPStatsGatewayLocationWithNilResolver(t *testing.T) {
	h, _ := newIPHandler(t, nil)
	if present, lat, lon := decodeGateway(t, h); present {
		t.Errorf("gateway = %v,%v present with a nil resolver, want omitted", lat, lon)
	}
}

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
	return New(store, testFS(), nil, bans, nil), store
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
		DistinctIPs int64              `json:"distinct_ips"`
		Hours       int                `json:"hours"`
		Items       []config.IPStat    `json:"items"`
		Bans        []banEntry         `json:"bans"`
		BanCount    int                `json:"ban_count"`
		GeoEnabled  bool               `json:"geo_enabled"`
		ASNEnabled  bool               `json:"asn_enabled"`
		Geo         []config.GeoBucket `json:"geo"`
		ASN         []config.ASNBucket `json:"asn"`
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

	// No GeoIP resolver wired (nil): both dimensions report not-connected and the
	// bucket arrays are omitted, so the console keeps its NotConnected panels.
	if out.GeoEnabled || out.ASNEnabled {
		t.Errorf("geo/asn enabled = %v/%v, want false/false (no resolver wired)", out.GeoEnabled, out.ASNEnabled)
	}
	if out.Geo != nil || out.ASN != nil {
		t.Errorf("geo/asn buckets = %+v/%+v, want nil/nil (no resolver wired)", out.Geo, out.ASN)
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
