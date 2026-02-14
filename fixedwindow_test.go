package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestFixedWindow_Allow(t *testing.T) {
	limiter := NewFixedWindow(5, time.Minute)

	// Should allow up to limit
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Should deny after limit
	if limiter.Allow() {
		t.Error("Request should be denied after limit reached")
	}
}

func TestFixedWindow_AllowN(t *testing.T) {
	limiter := NewFixedWindow(10, time.Minute)

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

func TestFixedWindow_WindowReset(t *testing.T) {
	limiter := NewFixedWindow(5, 100*time.Millisecond)

	// Exhaust the limit
	for i := 0; i < 5; i++ {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	if limiter.Allow() {
		t.Error("Request should be denied after limit reached")
	}

	// Wait for window to reset
	time.Sleep(150 * time.Millisecond)

	if !limiter.Allow() {
		t.Error("Request should be allowed after window reset")
	}
}

func TestFixedWindow_Check(t *testing.T) {
	limiter := NewFixedWindow(5, time.Minute)

	// Check should not consume tokens
	result1 := limiter.Check()
	result2 := limiter.Check()

	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should report allowed without consuming")
	}
	if result1.Remaining != result2.Remaining {
		t.Error("Remaining should be same after multiple checks")
	}

	// Consume all tokens
	for i := 0; i < 5; i++ {
		limiter.Allow()
	}

	// Check should report not allowed
	result3 := limiter.Check()
	if result3.Allowed {
		t.Error("Check should report not allowed after limit reached")
	}
	if result3.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when not allowed")
	}
}

func TestFixedWindow_Take(t *testing.T) {
	limiter := NewFixedWindow(5, time.Minute)

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
	if result.ResetAt.IsZero() {
		t.Error("ResetAt should not be zero")
	}

	// Exhaust remaining
	for i := 0; i < 4; i++ {
		limiter.Take()
	}

	// Next take should be denied
	denied := limiter.Take()
	if denied.Allowed {
		t.Error("Request should be denied after limit reached")
	}
	if denied.Remaining != 0 {
		t.Errorf("Expected 0 remaining, got %d", denied.Remaining)
	}
	if denied.RetryAfter <= 0 {
		t.Error("RetryAfter should be positive when denied")
	}
}

func TestFixedWindow_Reset(t *testing.T) {
	limiter := NewFixedWindow(5, time.Minute)

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

func TestFixedWindow_Concurrent(t *testing.T) {
	limiter := NewFixedWindow(100, time.Minute)
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

func TestKeyedFixedWindow_MultipleKeys(t *testing.T) {
	limiter := NewKeyedFixedWindow(5, time.Minute, time.Minute)
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

func TestKeyedFixedWindow_Cleanup(t *testing.T) {
	limiter := NewKeyedFixedWindow(5, time.Minute, 50*time.Millisecond)
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

func TestFixedWindow_PanicOnInvalidLimit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewFixedWindow(0, time.Minute)
}

func TestFixedWindow_PanicOnInvalidWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewFixedWindow(10, 0)
}

func TestKeyedFixedWindow_PanicOnInvalidLimit(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedFixedWindow(0, time.Minute, time.Minute)
}

func TestKeyedFixedWindow_PanicOnInvalidWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedFixedWindow(10, 0, time.Minute)
}
