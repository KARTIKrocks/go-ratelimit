package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucket(0, 10)
}

func TestTokenBucket_PanicOnInvalidBurst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucket(10.0, 0)
}

func TestTokenBucketPerDuration_PanicOnInvalidPer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucketPerDuration(10, 0, 10)
}

func TestKeyedTokenBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucket(0, 10, time.Minute)
}

func TestKeyedTokenBucket_PanicOnInvalidBurst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucket(10.0, 0, time.Minute)
}

func TestKeyedTokenBucketPerDuration_PanicOnInvalidPer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucketPerDuration(10, 0, 10, time.Minute)
}

func TestInvalidN(t *testing.T) {
	limiters := map[string]interface {
		Limiter
		ResultLimiter
	}{
		"TokenBucket":          NewTokenBucket(1, 5),
		"LeakyBucket":          NewLeakyBucket(1, 5),
		"FixedWindow":          NewFixedWindow(5, time.Minute),
		"SlidingWindow":        NewSlidingWindow(5, time.Minute),
		"SlidingWindowCounter": NewSlidingWindowCounter(5, time.Minute),
	}

	for name, l := range limiters {
		t.Run(name, func(t *testing.T) {
			for _, n := range []int{0, -100} {
				if l.AllowN(n) {
					t.Errorf("AllowN(%d) allowed", n)
				}
				if r := l.TakeN(n); r.Allowed {
					t.Errorf("TakeN(%d) allowed", n)
				}
				if r := l.CheckN(n); r.Allowed {
					t.Errorf("CheckN(%d) allowed", n)
				}
				if err := l.WaitN(context.Background(), n); !errors.Is(err, ErrInvalidN) {
					t.Errorf("WaitN(%d) = %v, want ErrInvalidN", n, err)
				}
			}

			// Negative n must not free up capacity.
			allowed := 0
			for l.Allow() {
				allowed++
			}
			if allowed != 5 {
				t.Errorf("allowed %d requests after invalid calls, want 5", allowed)
			}

			if err := l.WaitN(context.Background(), 6); !errors.Is(err, ErrExceedsLimit) {
				t.Errorf("WaitN(6) = %v, want ErrExceedsLimit", err)
			}
		})
	}
}

func TestKeyedInvalidN(t *testing.T) {
	limiters := map[string]interface {
		KeyedLimiter
		KeyedResultLimiter
		Len() int
	}{
		"KeyedTokenBucket":   NewKeyedTokenBucket(1, 5, 0),
		"KeyedLeakyBucket":   NewKeyedLeakyBucket(1, 5, 0),
		"KeyedFixedWindow":   NewKeyedFixedWindow(5, time.Minute, 0),
		"KeyedSlidingWindow": NewKeyedSlidingWindow(5, time.Minute, 0),
	}

	for name, l := range limiters {
		t.Run(name, func(t *testing.T) {
			for _, n := range []int{0, -100} {
				if l.AllowN("k", n) {
					t.Errorf("AllowN(%d) allowed", n)
				}
				if r := l.TakeN("k", n); r.Allowed {
					t.Errorf("TakeN(%d) allowed", n)
				}
				if r := l.CheckN("k", n); r.Allowed {
					t.Errorf("CheckN(%d) allowed", n)
				}
				if err := l.WaitN(context.Background(), "k", n); !errors.Is(err, ErrInvalidN) {
					t.Errorf("WaitN(%d) = %v, want ErrInvalidN", n, err)
				}
			}
			if l.Len() != 0 {
				t.Errorf("invalid calls created %d keys", l.Len())
			}
			if err := l.WaitN(context.Background(), "k", 6); !errors.Is(err, ErrExceedsLimit) {
				t.Errorf("WaitN(6) = %v, want ErrExceedsLimit", err)
			}
		})
	}
}
