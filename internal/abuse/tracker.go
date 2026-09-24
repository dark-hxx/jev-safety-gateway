// Package abuse provides in-memory per-IP abuse detection: it counts recent
// harmful ("block") verdicts per client IP within a sliding window and
// temporarily bans an IP that exceeds a threshold. State is process-local and
// resets on restart, which is appropriate for a single-instance gateway.
package abuse

import (
	"sync"
	"time"
)

// Tracker records harmful strikes per IP and issues temporary bans.
type Tracker struct {
	mu      sync.Mutex
	strikes map[string][]int64 // ip -> unix-ms timestamps of harmful hits
	banned  map[string]int64   // ip -> ban-until unix-ms
}

// New creates a Tracker and starts a background sweeper to bound memory.
func New() *Tracker {
	t := &Tracker{
		strikes: make(map[string][]int64),
		banned:  make(map[string]int64),
	}
	go t.sweepLoop()
	return t
}

// Banned reports whether ip is currently banned and, if so, until when.
func (t *Tracker) Banned(ip string, now time.Time) (bool, time.Time) {
	nowMS := now.UnixMilli()
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.banned[ip]
	if !ok {
		return false, time.Time{}
	}
	if nowMS >= until {
		delete(t.banned, ip)
		return false, time.Time{}
	}
	return true, time.UnixMilli(until)
}

// Snapshot returns the IPs currently banned as of now, each mapped to the time
// its ban expires. It is read-only: callers get a fresh copy and entries that
// have already expired are omitted, but nothing is deleted here — removal stays
// with Banned/sweepLoop so the ban lifecycle has a single owner. This exists so
// the admin console can report live bans without reaching into the map.
func (t *Tracker) Snapshot(now time.Time) map[string]time.Time {
	nowMS := now.UnixMilli()
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]time.Time, len(t.banned))
	for ip, until := range t.banned {
		if until > nowMS {
			out[ip] = time.UnixMilli(until)
		}
	}
	return out
}

// Strike records a harmful hit for ip. If the number of hits within the last
// windowSec seconds reaches maxHarmful, ip is banned for banSec seconds and the
// method returns (true, banUntil). Non-positive parameters disable the check.
func (t *Tracker) Strike(ip string, now time.Time, windowSec, maxHarmful, banSec int) (bool, time.Time) {
	if windowSec <= 0 || maxHarmful <= 0 || banSec <= 0 {
		return false, time.Time{}
	}
	nowMS := now.UnixMilli()
	windowStart := nowMS - int64(windowSec)*1000

	t.mu.Lock()
	defer t.mu.Unlock()

	// Prune timestamps outside the window, then append this hit.
	hits := t.strikes[ip]
	kept := hits[:0]
	for _, ts := range hits {
		if ts >= windowStart {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, nowMS)
	t.strikes[ip] = kept

	if len(kept) >= maxHarmful {
		until := nowMS + int64(banSec)*1000
		t.banned[ip] = until
		delete(t.strikes, ip) // reset counter once banned
		return true, time.UnixMilli(until)
	}
	return false, time.Time{}
}

// sweepLoop periodically drops expired bans and stale strike windows.
func (t *Tracker) sweepLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		nowMS := time.Now().UnixMilli()
		t.mu.Lock()
		for ip, until := range t.banned {
			if nowMS >= until {
				delete(t.banned, ip)
			}
		}
		// Drop strike lists whose newest entry is over an hour old.
		cutoff := nowMS - 3600*1000
		for ip, hits := range t.strikes {
			if len(hits) == 0 || hits[len(hits)-1] < cutoff {
				delete(t.strikes, ip)
			}
		}
		t.mu.Unlock()
	}
}
