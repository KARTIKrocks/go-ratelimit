package ratelimit

import (
	"testing"
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
	for i := 0; i < 5; i++ {
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
