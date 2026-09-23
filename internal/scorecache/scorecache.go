// Package scorecache reuses a JEV verdict for an identical submission within a
// short window. Clients that replay the whole conversation on every turn
// (stateless /v1/responses and /v1/messages callers) submit the very same user
// text dozens of times in a row: re-evaluating it costs a JEV call and returns
// the same answer.
//
// State is process-local and resets on restart, which is appropriate for a
// single-instance gateway — as with internal/abuse, none of it is worth
// persisting. The cache never stores the submitted text itself, only a digest
// of it, so it holds no user content in memory.
package scorecache

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

const (
	// maxEntryAge bounds memory regardless of the configured window: an entry
	// older than this is dropped by the sweeper even if the window was raised
	// after it was written.
	maxEntryAge = time.Hour

	// sweepInterval is how often the sweep loop reclaims stale entries.
	sweepInterval = time.Minute
)

// Cache maps a submission key to the score JEV returned for it.
type Cache struct {
	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	score float64
	at    time.Time
}

// New creates a Cache and starts a background sweeper to bound memory.
func New() *Cache {
	c := &Cache{entries: make(map[string]entry)}
	go c.sweepLoop()
	return c
}

// Key derives the cache key from the parts that determine a verdict: the text
// submitted plus every parameter that could change the answer. Parts are length
// prefixed, so ("ab","c") and ("a","bc") cannot collide.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return string(h.Sum(nil))
}

// Get returns the score recorded for key when it is still within ttl of now.
// The window is passed per lookup rather than held by the cache, so a window
// changed in the console applies immediately to entries already stored. An
// expired entry is dropped, so raising the window again cannot resurrect it.
func (c *Cache) Get(key string, ttl time.Duration, now time.Time) (float64, bool) {
	if ttl <= 0 {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return 0, false
	}
	if now.Sub(e.at) > ttl {
		delete(c.entries, key)
		return 0, false
	}
	return e.score, true
}

// Put records score for key, replacing any previous entry.
func (c *Cache) Put(key string, score float64, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{score: score, at: now}
}

// sweepLoop periodically drops entries older than maxEntryAge.
func (c *Cache) sweepLoop() {
	for {
		time.Sleep(sweepInterval)
		c.sweep(time.Now())
	}
}

// sweep removes every entry older than maxEntryAge relative to now.
func (c *Cache) sweep(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if now.Sub(e.at) > maxEntryAge {
			delete(c.entries, k)
		}
	}
}
