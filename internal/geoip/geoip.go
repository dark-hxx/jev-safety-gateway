// Package geoip resolves client IPs to country and ASN using optional local
// MaxMind (.mmdb) databases. It is deliberately off the proxy hot path: the
// admin analytics query calls it while aggregating the audit log, never
// proxy.ServeHTTP.
//
// Both databases are optional and supplied by the operator (GeoLite2 is not
// bundled — licensing plus the project's offline constraint). A missing or
// unreadable file disables that dimension instead of failing startup, so the
// analytics panels simply stay "not connected" until a database is configured.
package geoip

import (
	"net"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"

	"jev-safety-gateway/internal/config"
	"jev-safety-gateway/internal/logx"
)

// Resolver holds the open readers. Either may be nil (that dimension disabled).
// It implements config.GeoResolver. A nil *Resolver is safe to call.
type Resolver struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader

	// Where this gateway itself is deployed, for the origin map's central node.
	// Declared by the operator at startup (the gateway's own public IP is not
	// knowable offline), so it is guarded by a mutex rather than being a
	// constructor argument: Open stays a two-path call and "unset" is the zero
	// value. Read on the admin query path, written once before the servers start.
	mu            sync.RWMutex
	gatewayLat    float64
	gatewayLon    float64
	gatewayPlaced bool
}

// countryRecord is the subset of a GeoLite2-Country record we read. country is
// the physical location; registered_country is the fallback for IPs that carry
// only a registration (some anycast / mobile ranges).
type countryRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"registered_country"`
}

// asnRecord is the subset of a GeoLite2-ASN record we read.
type asnRecord struct {
	ASN uint   `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// Open opens whichever databases are configured. An empty path leaves that
// dimension disabled; a path that fails to open is logged and disabled rather
// than fatal. The returned *Resolver is always non-nil.
func Open(countryPath, asnPath string) (*Resolver, error) {
	r := &Resolver{}
	if countryPath != "" {
		if db, err := maxminddb.Open(countryPath); err != nil {
			logx.Infof("geoip: country database disabled (%s): %v", countryPath, err)
		} else {
			r.country = db
			logx.Infof("geoip: country database active (%s)", countryPath)
		}
	}
	if asnPath != "" {
		if db, err := maxminddb.Open(asnPath); err != nil {
			logx.Infof("geoip: ASN database disabled (%s): %v", asnPath, err)
		} else {
			r.asn = db
			logx.Infof("geoip: ASN database active (%s)", asnPath)
		}
	}
	return r, nil
}

// CountryEnabled reports whether a country database is open.
func (r *Resolver) CountryEnabled() bool { return r != nil && r.country != nil }

// ASNEnabled reports whether an ASN database is open.
func (r *Resolver) ASNEnabled() bool { return r != nil && r.asn != nil }

// SetGatewayLocation records where this gateway is deployed, in decimal degrees.
// The caller validates the ranges; this only stores. Calling it is what makes
// GatewayLocation report ok — so never calling it leaves the origin map without
// a hub, which is the honest default when the operator has not said where the
// service runs.
func (r *Resolver) SetGatewayLocation(lat, lon float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gatewayLat, r.gatewayLon, r.gatewayPlaced = lat, lon, true
}

// GatewayLocation reports the declared deployment coordinate of this gateway.
// ok is false when none was configured, and a nil *Resolver is safe to call.
//
// This is not IP attribution: it says where this process runs, not where a
// client came from, and it is only ever read by the admin analytics query.
func (r *Resolver) GatewayLocation() (lat, lon float64, ok bool) {
	if r == nil {
		return 0, 0, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gatewayLat, r.gatewayLon, r.gatewayPlaced
}

// Lookup resolves one IP. ok is false for an unparseable IP, or when neither
// database yielded any attribution (private ranges, unknown IPs).
func (r *Resolver) Lookup(ip string) (config.GeoInfo, bool) {
	if r == nil {
		return config.GeoInfo{}, false
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return config.GeoInfo{}, false
	}

	var info config.GeoInfo
	found := false

	if r.country != nil {
		var rec countryRecord
		if err := r.country.Lookup(addr, &rec); err == nil {
			iso, names := rec.Country.ISOCode, rec.Country.Names
			if iso == "" {
				iso, names = rec.RegisteredCountry.ISOCode, rec.RegisteredCountry.Names
			}
			if iso != "" {
				info.CountryISO = iso
				info.CountryName = names["en"]
				info.CountryNameZH = names["zh-CN"]
				found = true
			}
		}
	}

	if r.asn != nil {
		var rec asnRecord
		if err := r.asn.Lookup(addr, &rec); err == nil && rec.ASN != 0 {
			info.ASN = rec.ASN
			info.ASNOrg = rec.Org
			found = true
		}
	}

	return info, found
}

// Close closes any open readers. Safe on a nil *Resolver.
func (r *Resolver) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.country != nil {
		if e := r.country.Close(); e != nil {
			err = e
		}
	}
	if r.asn != nil {
		if e := r.asn.Close(); e != nil {
			err = e
		}
	}
	return err
}
