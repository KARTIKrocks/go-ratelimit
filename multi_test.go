package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMulti_AllAllow(t *testing.T) {
	l1 := NewTokenBucket(10.0, 10)
	l2 := NewTokenBucket(10.0, 10)
	l3 := NewTokenBucket(10.0, 10)

	multi := NewMulti(l1, l2, l3)

	if !multi.Allow() {
		t.Errorf("expected Allow to return true when all limiters have capacity")
	}
}

func TestMulti_OneDeny(t *testing.T) {
	l1 := NewTokenBucket(10.0, 10)
	l2 := NewTokenBucket(10.0, 1) // burst of 1

	multi := NewMulti(l1, l2)

	// First request should pass (both have capacity)
	if !multi.Allow() {
		t.Errorf("expected first Allow to return true")
	}

	// Second request should fail (l2 is exhausted)
	if multi.Allow() {
		t.Errorf("expected Allow to return false when one limiter is exhausted")
	}
}

func TestMulti_AllowN(t *testing.T) {
	l1 := NewTokenBucket(10.0, 10)
	l2 := NewTokenBucket(10.0, 5) // smaller burst

	multi := NewMulti(l1, l2)

	// Should allow 5 (limited by l2)
	if !multi.AllowN(5) {
		t.Errorf("expected AllowN(5) to return true")
	}

	// Should deny 1 more (l2 is exhausted)
	if multi.AllowN(1) {
		t.Errorf("expected AllowN(1) to return false after l2 exhausted")
	}
}

func TestMulti_Reset(t *testing.T) {
	l1 := NewTokenBucket(10.0, 5)
	l2 := NewTokenBucket(10.0, 5)

	multi := NewMulti(l1, l2)

	// Exhaust both limiters
	for range 5 {
		multi.Allow()
	}

	if multi.Allow() {
		t.Errorf("expected Allow to return false after exhaustion")
	}

	// Reset all limiters
	multi.Reset()

	// Should allow again
	if !multi.Allow() {
		t.Errorf("expected Allow to return true after reset")
	}
}

func TestMulti_DeniedRequestDoesNotConsume(t *testing.T) {
	roomy := NewFixedWindow(10, time.Hour)
	tight := NewFixedWindow(1, time.Hour)
	multi := NewMulti(roomy, tight)

	for range 5 {
		multi.Allow()
	}

	if got := roomy.Count(); got != 1 {
		t.Errorf("roomy limiter consumed %d, want 1 (only the allowed request)", got)
	}
}

func TestMulti_WaitNDoesNotConsumeWhileBlocked(t *testing.T) {
	roomy := NewFixedWindow(10, time.Hour)
	tight := NewFixedWindow(1, time.Hour)
	tight.Allow()
	multi := NewMulti(roomy, tight)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := multi.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want DeadlineExceeded", err)
	}
	if got := roomy.Count(); got != 0 {
		t.Errorf("roomy limiter consumed %d while waiting, want 0", got)
	}
}

func TestMulti_WaitNSucceeds(t *testing.T) {
	multi := NewMulti(NewTokenBucket(100, 1), NewTokenBucket(100, 1))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 3 {
		if err := multi.Wait(ctx); err != nil {
			t.Fatalf("Wait = %v", err)
		}
	}
}

func TestMulti_InvalidN(t *testing.T) {
	multi := NewMulti(NewTokenBucket(1, 5), NewFixedWindow(3, time.Minute))

	if multi.AllowN(0) || multi.AllowN(-1) {
		t.Error("AllowN with n <= 0 allowed")
	}
	if err := multi.WaitN(context.Background(), 0); !errors.Is(err, ErrInvalidN) {
		t.Errorf("WaitN(0) = %v, want ErrInvalidN", err)
	}
	if err := multi.WaitN(context.Background(), 4); !errors.Is(err, ErrExceedsLimit) {
		t.Errorf("WaitN(4) = %v, want ErrExceedsLimit", err)
	}
}

// noLimitResult is a ResultLimiter that leaves Result.Limit unset.
type noLimitResult struct{ *TokenBucket }

func (l noLimitResult) CheckN(n int) Result {
	r := l.TokenBucket.CheckN(n)
	r.Limit = 0
	return r
}

func TestMulti_WaitNWithUnsetLimit(t *testing.T) {
	tb := NewTokenBucket(100, 1)
	tb.Allow()
	multi := NewMulti(noLimitResult{tb})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := multi.Wait(ctx); err != nil {
		t.Errorf("Wait = %v, want nil", err)
	}
}

// plainLimiter implements Limiter but not ResultLimiter.
type plainLimiter struct{ Limiter }

func TestMulti_FallbackWithoutResultLimiter(t *testing.T) {
	multi := NewMulti(plainLimiter{NewTokenBucket(0.001, 2)}, NewTokenBucket(0.001, 1))
	if !multi.Allow() {
		t.Fatal("first request should be allowed")
	}
	if multi.Allow() {
		t.Error("second request should be denied")
	}
}
