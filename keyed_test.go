package ratelimit

import (
	"fmt"
	"testing"
	"time"
)

type keyedTestLimiter interface {
	KeyedResultLimiter
	Allow(key string) bool
	Len() int
	Close()
}

// keyedLimiters returns one of each keyed limiter, each allowing 2 requests
// per key, with maxKeys applied.
func keyedLimiters(maxKeys int) map[string]keyedTestLimiter {
	return map[string]keyedTestLimiter{
		"KeyedTokenBucket":   NewKeyedTokenBucket(0.001, 2, 0).SetMaxKeys(maxKeys),
		"KeyedLeakyBucket":   NewKeyedLeakyBucket(0.001, 2, 0).SetMaxKeys(maxKeys),
		"KeyedFixedWindow":   NewKeyedFixedWindow(2, time.Hour, 0).SetMaxKeys(maxKeys),
		"KeyedSlidingWindow": NewKeyedSlidingWindow(2, time.Hour, 0).SetMaxKeys(maxKeys),
	}
}

func TestKeyed_EvictsLeastRecentlyUsed(t *testing.T) {
	for name, l := range keyedLimiters(2) {
		t.Run(name, func(t *testing.T) {
			defer l.Close()
			l.Allow("a")
			l.Allow("a") // "a" is now at its limit
			l.Allow("b")
			l.Allow("a") // "a" becomes most recently used; still denied

			// A new key is allowed rather than locked out, and evicts "b".
			if !l.Allow("c") {
				t.Fatal("new key denied when full")
			}
			if l.Len() != 2 {
				t.Errorf("Len = %d, want 2", l.Len())
			}
			if l.Allow("a") {
				t.Error(`"a" was evicted instead of the least recently used "b"`)
			}
		})
	}
}

func TestKeyed_CheckDoesNotCreateKeys(t *testing.T) {
	for name, l := range keyedLimiters(1) {
		t.Run(name, func(t *testing.T) {
			defer l.Close()
			l.Allow("a")
			l.Allow("a")

			for i := range 10 {
				if r := l.Check(fmt.Sprint("probe", i)); !r.Allowed || r.Remaining != 2 {
					t.Fatalf("Check on unknown key = %+v, want allowed with full remaining", r)
				}
			}
			if l.Len() != 1 {
				t.Errorf("Check created keys: Len = %d, want 1", l.Len())
			}
			if l.Check("a").Allowed {
				t.Error("Check evicted or reset an existing key")
			}
		})
	}
}

func TestKeyed_LoweringMaxKeysEvicts(t *testing.T) {
	l := NewKeyedFixedWindow(1, time.Hour, 0)
	defer l.Close()
	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	l.SetMaxKeys(1)
	if l.Len() != 1 {
		t.Fatalf("Len = %d, want 1", l.Len())
	}
	if l.Allow("c") {
		t.Error(`most recently used key "c" was evicted`)
	}
}

func TestKeyedStore_RemoveIdleStopsAtFirstActiveEntry(t *testing.T) {
	s := newKeyedStore(0, func(time.Time) int { return 0 })
	base := time.Now()
	s.get("old1", base)
	s.get("old2", base.Add(time.Second))
	s.get("new", base.Add(time.Hour))
	s.get("old1", base.Add(2*time.Hour)) // touched again, so it is kept

	s.removeIdle(base.Add(time.Minute))

	if s.len() != 2 {
		t.Fatalf("len = %d, want 2", s.len())
	}
	if _, ok := s.entries["old2"]; ok {
		t.Error("idle entry was not removed")
	}
}
