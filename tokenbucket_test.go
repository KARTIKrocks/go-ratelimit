package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	limiter := NewTokenBucket(10.0, 10)

	// Should allow burst
	for i := 0; i < 10; i++ {
		if !limiter.Allow() {
			t.Errorf("Request %d should be allowed", i)
		}
	}

	// Should deny after burst
	if limiter.Allow() {
		t.Error("Request should be denied after burst")
	}

	// Wait for tokens to refill
	time.Sleep(200 * time.Millisecond)
	if !limiter.Allow() {
		t.Error("Request should be allowed after refill")
	}
}

func TestTokenBucket_AllowN(t *testing.T) {
	limiter := NewTokenBucket(10.0, 10)

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

func TestTokenBucket_Wait(t *testing.T) {
	limiter := NewTokenBucket(100.0, 10)

	// Exhaust tokens
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

func TestTokenBucket_WaitWithTimeout(t *testing.T) {
	limiter := NewTokenBucket(1.0, 1)
	limiter.Allow() // Exhaust token

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got: %v", err)
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	limiter := NewTokenBucket(100.0, 100)
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

func TestTokenBucket_Take(t *testing.T) {
	limiter := NewTokenBucket(10.0, 10)

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

func TestTokenBucket_Check(t *testing.T) {
	limiter := NewTokenBucket(10.0, 10)

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

func TestTokenBucket_Reset(t *testing.T) {
	limiter := NewTokenBucket(10.0, 10)

	// Exhaust tokens
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	limiter.Reset()

	// Should be able to take tokens again
	if !limiter.Allow() {
		t.Error("Should be allowed after reset")
	}
}

func TestKeyedTokenBucket_MultipleKeys(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 10, time.Minute)
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

	// Both keys should be exhausted
	if limiter.Allow("key1") {
		t.Error("key1 should be exhausted")
	}
	if limiter.Allow("key2") {
		t.Error("key2 should be exhausted")
	}
}

func TestKeyedTokenBucket_Cleanup(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 10, 50*time.Millisecond)
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

func TestKeyedTokenBucket_ResetAll(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 10, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	limiter.Allow("key2")

	limiter.ResetAll()

	if limiter.Len() != 0 {
		t.Errorf("Expected 0 keys after ResetAll, got %d", limiter.Len())
	}
}

func BenchmarkTokenBucket_Allow(b *testing.B) {
	limiter := NewTokenBucket(1000000.0, 1000000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkTokenBucket_AllowParallel(b *testing.B) {
	limiter := NewTokenBucket(1000000.0, 1000000)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkKeyedTokenBucket_Allow(b *testing.B) {
	limiter := NewKeyedTokenBucket(1000000.0, 1000000, time.Minute)
	defer limiter.Close()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Allow("key")
	}
}

func BenchmarkKeyedTokenBucket_AllowParallel(b *testing.B) {
	limiter := NewKeyedTokenBucket(1000000.0, 1000000, time.Minute)
	defer limiter.Close()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow("key")
		}
	})
}

func TestKeyedTokenBucket_MaxKeys(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer limiter.Close()
	limiter.SetMaxKeys(2)

	// First two keys should succeed
	if !limiter.Allow("key1") {
		t.Error("key1 should be allowed")
	}
	if !limiter.Allow("key2") {
		t.Error("key2 should be allowed")
	}

	// Third new key should be denied (maxKeys reached)
	if limiter.Allow("key3") {
		t.Error("key3 should be denied (maxKeys=2)")
	}

	// Existing key should still work
	if !limiter.Allow("key1") {
		t.Error("existing key1 should still be allowed")
	}

	// Check/Take should also respect maxKeys
	result := limiter.Take("key4")
	if result.Allowed {
		t.Error("key4 Take should be denied")
	}
	result = limiter.Check("key5")
	if result.Allowed {
		t.Error("key5 Check should be denied")
	}

	// After reset, new key should work
	limiter.Reset("key2")
	if !limiter.Allow("key3") {
		t.Error("key3 should be allowed after key2 reset")
	}
}

func BenchmarkKeyedTokenBucket_MultipleKeys(b *testing.B) {
	limiter := NewKeyedTokenBucket(1000000.0, 1000000, time.Minute)
	defer limiter.Close()
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		limiter.Allow(keys[i%len(keys)])
	}
}
