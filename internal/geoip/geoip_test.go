package geoip

import (
	"path/filepath"
	"testing"
)

// With no paths configured, both dimensions are disabled and every lookup misses.
// This is the default install: no GeoIP databases, panels stay "not connected".
func TestOpenEmptyPathsDisablesBoth(t *testing.T) {
	r, err := Open("", "")
	if err != nil {
		t.Fatalf("Open(\"\", \"\"): unexpected error %v", err)
	}
	if r == nil {
		t.Fatal("Open returned a nil *Resolver; it must always be non-nil")
	}
	t.Cleanup(func() { _ = r.Close() })

	if r.CountryEnabled() {
		t.Error("CountryEnabled() = true with no country path, want false")
	}
	if r.ASNEnabled() {
		t.Error("ASNEnabled() = true with no ASN path, want false")
	}
	if info, ok := r.Lookup("8.8.8.8"); ok {
		t.Errorf("Lookup with no databases returned ok=true (%+v), want false", info)
	}
}

// A path that does not exist disables that dimension rather than failing startup:
// the operator may point at a file they have not installed yet, and the gateway
// must still come up (the panel simply stays not-connected).
func TestOpenMissingFileDisablesGracefully(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.mmdb")
	r, err := Open(missing, missing)
	if err != nil {
		t.Fatalf("Open with missing files should not error, got %v", err)
	}
	if r == nil {
		t.Fatal("Open returned a nil *Resolver")
	}
	t.Cleanup(func() { _ = r.Close() })

	if r.CountryEnabled() || r.ASNEnabled() {
		t.Errorf("enabled = %v/%v with missing files, want false/false", r.CountryEnabled(), r.ASNEnabled())
	}
	// An unparseable IP must also miss without panicking.
	if _, ok := r.Lookup("not-an-ip"); ok {
		t.Error("Lookup of a malformed IP returned ok=true, want false")
	}
}

// The deployment coordinate is unset until the operator declares one, and 0,0 is
// a real place (the Gulf of Guinea) — so "unset" must be reported by ok, not by
// the numbers reading zero. Otherwise the origin map could not tell a deliberate
// 0,0 from no configuration at all.
func TestGatewayLocationUnsetThenDeclared(t *testing.T) {
	r, _ := Open("", "")
	t.Cleanup(func() { _ = r.Close() })

	if lat, lon, ok := r.GatewayLocation(); ok {
		t.Errorf("GatewayLocation() = %v,%v,%v before Set, want ok=false", lat, lon, ok)
	}

	r.SetGatewayLocation(31.23, 121.47) // Shanghai
	lat, lon, ok := r.GatewayLocation()
	if !ok {
		t.Fatal("GatewayLocation() ok=false after Set, want true")
	}
	if lat != 31.23 || lon != 121.47 {
		t.Errorf("GatewayLocation() = %v,%v, want 31.23,121.47", lat, lon)
	}

	// A deliberate 0,0 is a declared location, distinct from the unset state.
	r.SetGatewayLocation(0, 0)
	if lat, lon, ok := r.GatewayLocation(); !ok || lat != 0 || lon != 0 {
		t.Errorf("GatewayLocation() after Set(0,0) = %v,%v,%v, want 0,0,true", lat, lon, ok)
	}
}

// A nil *Resolver answers the deployment coordinate safely, like its other
// methods: the resolver is only nil-safe by contract, and main always defers Close.
func TestGatewayLocationSafeOnNil(t *testing.T) {
	var nilR *Resolver
	if _, _, ok := nilR.GatewayLocation(); ok {
		t.Error("nil resolver GatewayLocation() ok=true, want false")
	}
	nilR.SetGatewayLocation(1, 2) // must not panic
}

// Close is safe on a resolver with no open readers and on a nil *Resolver: the
// deferred Close in main must never panic regardless of which databases opened.
func TestCloseSafeOnNilAndEmpty(t *testing.T) {
	var nilR *Resolver
	if err := nilR.Close(); err != nil {
		t.Errorf("(*Resolver)(nil).Close() = %v, want nil", err)
	}
	// A nil resolver also answers the capability/lookup methods safely.
	if nilR.CountryEnabled() || nilR.ASNEnabled() {
		t.Error("nil resolver reports a dimension enabled")
	}
	if _, ok := nilR.Lookup("8.8.8.8"); ok {
		t.Error("nil resolver Lookup returned ok=true")
	}

	r, _ := Open("", "")
	if err := r.Close(); err != nil {
		t.Errorf("empty resolver Close() = %v, want nil", err)
	}
}
