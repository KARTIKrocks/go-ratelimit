package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestSlidingWindow_Allow(t *testing.T) {
	limiter := NewSlidingWindow(5, time.Second)

	for i := 0; i < 5; i++ {
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
	for i := 0; i < 3; i++ {
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
	for i := 0; i < 4; i++ {
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
	for i := 0; i < 5; i++ {
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

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		}()
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

	for i := 0; i < 5; i++ {
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
	for i := 0; i < 4; i++ {
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
	for i := 0; i < 5; i++ {
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

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		}()
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
	for i := 0; i < 5; i++ {
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
