package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestSlidingWindow_Allow(t *testing.T) {
	limiter := NewSlidingWindow(5, time.Second)

	for i := range 5 {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("Request should be denied after limit reached")
	}
}

func TestSlidingWindow_AllowN(t *testing.T) {
	limiter := NewSlidingWindow(10, time.Second)

	if !limiter.AllowN(3) {
		t.Error("3 requests should be allowed")
	}

	if !limiter.AllowN(7) {
		t.Error("Another 7 requests should be allowed")
	}

	if limiter.AllowN(1) {
		t.Error("No more requests should be allowed")
	}
}

func TestSlidingWindow_SlidingBehavior(t *testing.T) {
	limiter := NewSlidingWindow(3, 100*time.Millisecond)

	// Exhaust limit
	for i := range 3 {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("Request should be denied after limit reached")
	}

	// Wait for the oldest timestamps to expire
	time.Sleep(150 * time.Millisecond)

	if !limiter.Allow() {
		t.Error("Request should be allowed after oldest entries expired")
	}
}

func TestSlidingWindow_Check(t *testing.T) {
	limiter := NewSlidingWindow(5, time.Second)

	// Check should not consume tokens
	result1 := limiter.Check()
	result2 := limiter.Check()

	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume tokens")
	}
	if result1.Remaining != result2.Remaining {
		t.Error("Remaining should be same after multiple checks")
	}

	// Verify remaining is at full capacity
	if result1.Remaining != 5 {
		t.Errorf("Expected 5 remaining, got %d", result1.Remaining)
	}
}

func TestSlidingWindow_Take(t *testing.T) {
	limiter := NewSlidingWindow(5, time.Second)

	result := limiter.Take()
	if !result.Allowed {
		t.Error("First request should be allowed")
	}
	if result.Remaining != 4 {
		t.Errorf("Expected 4 remaining, got %d", result.Remaining)
	}
	if result.Limit != 5 {
		t.Errorf("Expected limit 5, got %d", result.Limit)
	}

	// Exhaust remaining
	for range 4 {
		limiter.Take()
	}

	result = limiter.Take()
	if result.Allowed {
		t.Error("Request should be denied after limit reached")
	}
	if result.Remaining != 0 {
		t.Errorf("Expected 0 remaining, got %d", result.Remaining)
	}
	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}
}

func TestSlidingWindow_Reset(t *testing.T) {
	limiter := NewSlidingWindow(5, time.Second)

	// Exhaust tokens
	for range 5 {
		limiter.Allow()
	}

	if limiter.Allow() {
		t.Error("Should be denied before reset")
	}

	limiter.Reset()

	if !limiter.Allow() {
		t.Error("Should be allowed after reset")
	}
}

func TestSlidingWindow_Concurrent(t *testing.T) {
	limiter := NewSlidingWindow(100, time.Second)
	var wg sync.WaitGroup
	var allowed, denied int
	var mu sync.Mutex

	for range 200 {
		wg.Go(func() {
			if limiter.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if allowed != 100 {
		t.Errorf("Expected 100 allowed, got %d", allowed)
	}
	if denied != 100 {
		t.Errorf("Expected 100 denied, got %d", denied)
	}
}

func TestSlidingWindowCounter_Allow(t *testing.T) {
	limiter := NewSlidingWindowCounter(5, time.Second)

	for i := range 5 {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("Request should be denied after limit reached")
	}
}

func TestSlidingWindowCounter_AllowN(t *testing.T) {
	limiter := NewSlidingWindowCounter(10, time.Second)

	if !limiter.AllowN(4) {
		t.Error("4 requests should be allowed")
	}

	if !limiter.AllowN(6) {
		t.Error("Another 6 requests should be allowed")
	}

	if limiter.AllowN(1) {
		t.Error("No more requests should be allowed")
	}
}

func TestSlidingWindowCounter_Check(t *testing.T) {
	limiter := NewSlidingWindowCounter(5, time.Second)

	// Check should not consume tokens
	result1 := limiter.Check()
	result2 := limiter.Check()

	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume tokens")
	}
	if result1.Remaining != result2.Remaining {
		t.Error("Remaining should be same after multiple checks")
	}
}

func TestSlidingWindowCounter_Take(t *testing.T) {
	limiter := NewSlidingWindowCounter(5, time.Second)

	result := limiter.Take()
	if !result.Allowed {
		t.Error("First request should be allowed")
	}
	if result.Limit != 5 {
		t.Errorf("Expected limit 5, got %d", result.Limit)
	}

	// Exhaust remaining
	for range 4 {
		limiter.Take()
	}

	result = limiter.Take()
	if result.Allowed {
		t.Error("Request should be denied after limit reached")
	}
	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}
}

func TestSlidingWindowCounter_Reset(t *testing.T) {
	limiter := NewSlidingWindowCounter(5, time.Second)

	// Exhaust tokens
	for range 5 {
		limiter.Allow()
	}

	if limiter.Allow() {
		t.Error("Should be denied before reset")
	}

	limiter.Reset()

	if !limiter.Allow() {
		t.Error("Should be allowed after reset")
	}
}

func TestSlidingWindowCounter_Concurrent(t *testing.T) {
	limiter := NewSlidingWindowCounter(100, time.Second)
	var wg sync.WaitGroup
	var allowed, denied int
	var mu sync.Mutex

	for range 200 {
		wg.Go(func() {
			if limiter.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	if allowed != 100 {
		t.Errorf("Expected 100 allowed, got %d", allowed)
	}
	if denied != 100 {
		t.Errorf("Expected 100 denied, got %d", denied)
	}
}

func TestKeyedSlidingWindow_MultipleKeys(t *testing.T) {
	limiter := NewKeyedSlidingWindow(5, time.Second, time.Minute)
	defer limiter.Close()

	// Different keys should have independent limits
	for i := range 5 {
		if !limiter.Allow("key1") {
			t.Errorf("key1 request %d should be allowed", i)
		}
		if !limiter.Allow("key2") {
			t.Errorf("key2 request %d should be allowed", i)
		}
	}

	// Both keys should be exhausted
	if limiter.Allow("key1") {
		t.Error("key1 should be exhausted")
	}
	if limiter.Allow("key2") {
		t.Error("key2 should be exhausted")
	}
}

func TestKeyedSlidingWindow_Cleanup(t *testing.T) {
	limiter := NewKeyedSlidingWindow(5, time.Second, 50*time.Millisecond)
	defer limiter.Close()

	limiter.Allow("key1")
	limiter.Allow("key2")

	if limiter.Len() != 2 {
		t.Errorf("Expected 2 keys, got %d", limiter.Len())
	}

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	if limiter.Len() != 0 {
		t.Errorf("Expected 0 keys after cleanup, got %d", limiter.Len())
	}
}

func TestSlidingWindow_PanicOnInvalidLimit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewSlidingWindow(0, time.Second)
}

func TestSlidingWindow_PanicOnInvalidWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewSlidingWindow(5, 0)
}

func TestSlidingWindowCounter_PanicOnInvalidLimit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewSlidingWindowCounter(0, time.Second)
}

func TestSlidingWindowCounter_PanicOnInvalidWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewSlidingWindowCounter(5, 0)
}

func TestKeyedSlidingWindow_PanicOnInvalidLimit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedSlidingWindow(0, time.Second, time.Minute)
}

func TestKeyedSlidingWindow_PanicOnInvalidWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedSlidingWindow(5, 0, time.Minute)
}

func TestSlidingRetryAfter(t *testing.T) {
	const w = 10 * time.Second
	tests := []struct {
		name                 string
		prev, curr, n, limit int
		elapsed, wantRetryIn time.Duration
	}{
		// 10*(1-x) + 0 + 1 <= 10 once x >= 0.1: 1s into the window, not 10s.
		{"previous window fades within this window", 10, 0, 1, 10, 0, time.Second},
		{"already fits", 10, 0, 1, 10, 3 * time.Second, 0},
		{"no previous count", 0, 5, 1, 10, 0, 0},
		// Current window is full: wait 8s for the next window, then 10*(1-x)+1 <= 10 needs x >= 0.1.
		{"current window full", 0, 10, 1, 10, 2 * time.Second, 9 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slidingRetryAfter(tt.prev, tt.curr, tt.n, tt.limit, tt.elapsed, w)
			if diff := got - tt.wantRetryIn; diff < -time.Millisecond || diff > time.Millisecond {
				t.Errorf("slidingRetryAfter = %v, want %v", got, tt.wantRetryIn)
			}
		})
	}
}

func TestSlidingWindowCounter_RetryAfterIsEarliestFit(t *testing.T) {
	swc := NewSlidingWindowCounter(10, time.Hour)
	swc.prevCount = 10
	swc.windowStart = time.Now()

	r := swc.Take()
	if r.Allowed {
		t.Fatal("request allowed with a full previous window")
	}
	// The previous window's weight drops enough after 6 minutes, not 1 hour.
	if r.RetryAfter > 7*time.Minute {
		t.Errorf("RetryAfter = %v, want about 6m", r.RetryAfter)
	}
}
