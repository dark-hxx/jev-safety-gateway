package scorecache

import (
	"testing"
	"time"
)

func TestKeyIsSensitiveToEveryPartAndToPartBoundaries(t *testing.T) {
	base := []string{"https://jev", "jev-latest", "is it safe?", "0.5", "true", "hello"}
	k := Key(base...)
	for i, p := range base {
		changed := append([]string{}, base...)
		changed[i] = p + "x"
		if Key(changed...) == k {
			t.Fatalf("part %d did not affect the key", i)
		}
	}
	// Length prefixing must keep different splits of the same bytes apart.
	if Key("ab", "c") == Key("a", "bc") {
		t.Fatal("part boundaries are ambiguous: (ab,c) collided with (a,bc)")
	}
	if Key(base...) != Key(base...) {
		t.Fatal("key is not stable across calls")
	}
}

func TestGetReturnsScoreWithinWindow(t *testing.T) {
	c := New()
	now := time.Now()
	key := Key("text")
	c.Put(key, 0.75, now)

	if got, ok := c.Get(key, time.Minute, now.Add(30*time.Second)); !ok || got != 0.75 {
		t.Fatalf("within window: got (%v, %v), want (0.75, true)", got, ok)
	}
	if _, ok := c.Get(key, time.Minute, now.Add(time.Minute)); !ok {
		t.Fatal("the window boundary itself should still hit")
	}
}

func TestGetMissesAfterWindowAndDropsTheEntry(t *testing.T) {
	c := New()
	now := time.Now()
	key := Key("text")
	c.Put(key, 0.2, now)

	if _, ok := c.Get(key, time.Minute, now.Add(time.Minute+time.Millisecond)); ok {
		t.Fatal("entry past its window must not hit")
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("expired entry was not dropped: %d entries left", n)
	}
	// Raising the window afterwards must not resurrect the dropped verdict.
	if _, ok := c.Get(key, 10*time.Minute, now.Add(time.Minute+time.Millisecond)); ok {
		t.Fatal("a dropped entry must not reappear when the window is raised")
	}
}

func TestGetUsesTheWindowPassedIn(t *testing.T) {
	now := time.Now()
	at := now.Add(90 * time.Second) // the entry is 90s old at lookup time

	narrow := New()
	narrow.Put(Key("text"), 0.9, now)
	if _, ok := narrow.Get(Key("text"), 60*time.Second, at); ok {
		t.Fatal("hit with a window shorter than the entry's age")
	}

	// Same entry, wider window: raising dedup_window_sec in the console applies
	// to lookups immediately, with no cache rebuild or restart.
	wide := New()
	wide.Put(Key("text"), 0.9, now)
	if _, ok := wide.Get(Key("text"), 5*time.Minute, at); !ok {
		t.Fatal("the same entry must hit once the window covers its age")
	}
}

func TestGetIgnoresNonPositiveWindow(t *testing.T) {
	c := New()
	now := time.Now()
	key := Key("text")
	c.Put(key, 0.5, now)

	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, ok := c.Get(key, ttl, now); ok {
			t.Fatalf("ttl %v must never hit", ttl)
		}
	}
}

func TestSweepDropsOnlyStaleEntries(t *testing.T) {
	c := New()
	now := time.Now()
	stale, fresh := Key("stale"), Key("fresh")
	c.Put(stale, 0.1, now.Add(-maxEntryAge-time.Minute))
	c.Put(fresh, 0.1, now)

	c.sweep(now)

	if _, ok := c.Get(stale, maxEntryAge, now); ok {
		t.Fatal("stale entry survived the sweep")
	}
	if _, ok := c.Get(fresh, maxEntryAge, now); !ok {
		t.Fatal("fresh entry was dropped by the sweep")
	}
}

func TestPutReplacesPreviousScore(t *testing.T) {
	c := New()
	now := time.Now()
	key := Key("text")
	c.Put(key, 0.1, now)
	c.Put(key, 0.8, now)
	if got, _ := c.Get(key, time.Minute, now); got != 0.8 {
		t.Fatalf("got %v, want the latest score 0.8", got)
	}
}
