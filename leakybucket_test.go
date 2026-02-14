package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestLeakyBucket_Allow(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	// Should allow adding water up to capacity
	for i := 0; i < 10; i++ {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Should deny when bucket is full
	if limiter.Allow() {
		t.Error("Request should be denied when bucket is full")
	}

	// Wait for some water to leak out
	time.Sleep(150 * time.Millisecond)
	if !limiter.Allow() {
		t.Error("Request should be allowed after water leaks")
	}
}

func TestLeakyBucket_AllowN(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	if !limiter.AllowN(5) {
		t.Error("5 requests should be allowed")
	}

	if !limiter.AllowN(5) {
		t.Error("Another 5 requests should be allowed")
	}

	if limiter.AllowN(1) {
		t.Error("No more requests should be allowed")
	}
}

func TestLeakyBucket_Capacity(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 20)

	// Fill exactly to capacity
	if !limiter.AllowN(20) {
		t.Error("Should allow filling exactly to capacity")
	}

	// One more should be denied
	if limiter.Allow() {
		t.Error("Should deny when at capacity")
	}

	// Partial fill should also be denied
	if limiter.AllowN(2) {
		t.Error("Should deny when adding would exceed capacity")
	}
}

func TestLeakyBucket_Wait(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	// Exhaust capacity
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestLeakyBucket_WaitWithTimeout(t *testing.T) {
	limiter := NewLeakyBucket(1.0, 1)
	limiter.Allow() // Fill bucket

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got: %v", err)
	}
}

func TestLeakyBucket_Check(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	// Check should not add water
	result1 := limiter.Check()
	result2 := limiter.Check()

	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume capacity")
	}
	if result1.Remaining != result2.Remaining {
		t.Error("Remaining should be same after multiple checks")
	}
}

func TestLeakyBucket_Take(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	result := limiter.Take()
	if !result.Allowed {
		t.Error("First request should be allowed")
	}
	if result.Remaining != 9 {
		t.Errorf("Expected 9 remaining, got %d", result.Remaining)
	}
	if result.Limit != 10 {
		t.Errorf("Expected limit 10, got %d", result.Limit)
	}
}

func TestLeakyBucket_TakeN(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	result := limiter.TakeN(4)
	if !result.Allowed {
		t.Error("Taking 4 should be allowed")
	}
	if result.Remaining != 6 {
		t.Errorf("Expected 6 remaining, got %d", result.Remaining)
	}

	result = limiter.TakeN(6)
	if !result.Allowed {
		t.Error("Taking 6 more should be allowed")
	}
	if result.Remaining != 0 {
		t.Errorf("Expected 0 remaining, got %d", result.Remaining)
	}

	result = limiter.TakeN(1)
	if result.Allowed {
		t.Error("Taking 1 more should be denied")
	}
}

func TestLeakyBucket_Reset(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 10)

	// Fill the bucket
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	limiter.Reset()

	// Should be able to add water again
	if !limiter.Allow() {
		t.Error("Should be allowed after reset")
	}
}

func TestLeakyBucket_Concurrent(t *testing.T) {
	limiter := NewLeakyBucket(100.0, 100)
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

func TestKeyedLeakyBucket_MultipleKeys(t *testing.T) {
	limiter := NewKeyedLeakyBucket(100.0, 10, time.Minute)
	defer limiter.Close()

	// Different keys should have independent limits
	for i := 0; i < 10; i++ {
		if !limiter.Allow("key1") {
			t.Errorf("key1 request %d should be allowed", i)
		}
		if !limiter.Allow("key2") {
			t.Errorf("key2 request %d should be allowed", i)
		}
	}

	// Both keys should be full
	if limiter.Allow("key1") {
		t.Error("key1 should be full")
	}
	if limiter.Allow("key2") {
		t.Error("key2 should be full")
	}
}

func TestKeyedLeakyBucket_Cleanup(t *testing.T) {
	limiter := NewKeyedLeakyBucket(100.0, 10, 50*time.Millisecond)
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

func TestLeakyBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewLeakyBucket(0, 10)
}

func TestLeakyBucket_PanicOnInvalidCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewLeakyBucket(10.0, 0)
}

func TestLeakyBucketPerDuration_PanicOnInvalidPer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewLeakyBucketPerDuration(10, 0, 10)
}

func TestKeyedLeakyBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedLeakyBucket(-1.0, 10, time.Minute)
}

func TestKeyedLeakyBucket_PanicOnInvalidCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedLeakyBucket(10.0, -1, time.Minute)
}
