package ratelimit

import (
	"context"
	"sync"
	"time"
)

// LeakyBucket implements the leaky bucket algorithm.
// Requests are processed at a fixed rate, with overflow being rejected.
// Provides smooth rate limiting without bursts.
type LeakyBucket struct {
	rate     float64   // Requests per second (leak rate)
	capacity int       // Maximum queue size
	water    float64   // Current water level
	lastLeak time.Time // Last leak time
	mu       sync.Mutex
}

// NewLeakyBucket creates a new leaky bucket limiter.
// rate: requests per second (leak rate)
// capacity: maximum requests that can be queued
func NewLeakyBucket(rate float64, capacity int) *LeakyBucket {
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if capacity <= 0 {
		panic("ratelimit: capacity must be positive")
	}
	return &LeakyBucket{
		rate:     rate,
		capacity: capacity,
		water:    0,
		lastLeak: time.Now(),
	}
}

// NewLeakyBucketPerDuration creates a leaky bucket with rate per duration.
func NewLeakyBucketPerDuration(count int, per time.Duration, capacity int) *LeakyBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewLeakyBucket(rate, capacity)
}

// leak removes water based on elapsed time.
func (lb *LeakyBucket) leak() {
	now := time.Now()
	elapsed := now.Sub(lb.lastLeak).Seconds()
	lb.lastLeak = now

	// Leak water
	lb.water -= elapsed * lb.rate
	if lb.water < 0 {
		lb.water = 0
	}
}

// Allow checks if a request is allowed.
func (lb *LeakyBucket) Allow() bool {
	return lb.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (lb *LeakyBucket) AllowN(n int) bool {
	if validateN(n, lb.capacity) != nil {
		return false
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.leak()

	if lb.water+float64(n) <= float64(lb.capacity) {
		lb.water += float64(n)
		return true
	}
	return false
}

// Wait blocks until a request is allowed.
func (lb *LeakyBucket) Wait(ctx context.Context) error {
	return lb.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (lb *LeakyBucket) WaitN(ctx context.Context, n int) error {
	if err := validateN(n, lb.capacity); err != nil {
		return err
	}

	for {
		lb.mu.Lock()
		lb.leak()

		if lb.water+float64(n) <= float64(lb.capacity) {
			lb.water += float64(n)
			lb.mu.Unlock()
			return nil
		}

		overflow := lb.water + float64(n) - float64(lb.capacity)
		waitTime := time.Duration(overflow / lb.rate * float64(time.Second))
		lb.mu.Unlock()

		timer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Reset resets the bucket.
func (lb *LeakyBucket) Reset() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.water = 0
	lb.lastLeak = time.Now()
}

// Check returns the current state without consuming.
func (lb *LeakyBucket) Check() Result {
	return lb.CheckN(1)
}

// CheckN returns the state for n requests without consuming.
func (lb *LeakyBucket) CheckN(n int) Result {
	if validateN(n, lb.capacity) != nil {
		return Result{Limit: lb.capacity}
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.leak()

	result := Result{
		Limit:     lb.capacity,
		Remaining: int(float64(lb.capacity) - lb.water),
	}

	if lb.water+float64(n) <= float64(lb.capacity) {
		result.Allowed = true
	} else {
		result.Allowed = false
		overflow := lb.water + float64(n) - float64(lb.capacity)
		result.RetryAfter = time.Duration(overflow / lb.rate * float64(time.Second))
	}

	return result
}

// Take consumes a token and returns the result.
func (lb *LeakyBucket) Take() Result {
	return lb.TakeN(1)
}

// TakeN consumes n tokens and returns the result.
func (lb *LeakyBucket) TakeN(n int) Result {
	if validateN(n, lb.capacity) != nil {
		return Result{Limit: lb.capacity}
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.leak()

	result := Result{
		Limit:     lb.capacity,
		Remaining: int(float64(lb.capacity) - lb.water),
	}

	if lb.water+float64(n) <= float64(lb.capacity) {
		lb.water += float64(n)
		result.Allowed = true
		result.Remaining = int(float64(lb.capacity) - lb.water)
	} else {
		result.Allowed = false
		overflow := lb.water + float64(n) - float64(lb.capacity)
		result.RetryAfter = time.Duration(overflow / lb.rate * float64(time.Second))
	}

	return result
}

// Water returns the current water level.
func (lb *LeakyBucket) Water() float64 {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.leak()
	return lb.water
}

// Rate returns the leak rate per second.
func (lb *LeakyBucket) Rate() float64 {
	return lb.rate
}

// Capacity returns the bucket capacity.
func (lb *LeakyBucket) Capacity() int {
	return lb.capacity
}

// KeyedLeakyBucket provides per-key leaky bucket rate limiting.
type KeyedLeakyBucket struct {
	rate     float64
	capacity int
	store    *keyedStore[leakyBucketEntry]
}

type leakyBucketEntry struct {
	water    float64
	lastLeak time.Time
}

// NewKeyedLeakyBucket creates a new keyed leaky bucket limiter.
// If cleanupInterval is positive, keys idle for twice that long are removed
// in the background until Close. Use SetMaxKeys to bound memory.
func NewKeyedLeakyBucket(rate float64, capacity int, cleanupInterval time.Duration) *KeyedLeakyBucket {
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if capacity <= 0 {
		panic("ratelimit: capacity must be positive")
	}
	return &KeyedLeakyBucket{
		rate:     rate,
		capacity: capacity,
		store: newKeyedStore(cleanupInterval, func(now time.Time) leakyBucketEntry {
			return leakyBucketEntry{lastLeak: now}
		}),
	}
}

// NewKeyedLeakyBucketPerDuration creates a keyed leaky bucket with rate per duration.
func NewKeyedLeakyBucketPerDuration(count int, per time.Duration, capacity int, cleanupInterval time.Duration) *KeyedLeakyBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewKeyedLeakyBucket(rate, capacity, cleanupInterval)
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, the least recently used key is evicted to make room for a new
// one; an evicted key starts again with no usage. Zero means unlimited.
// Size n well above the number of keys active at once: a client that can
// create more than n new keys can evict another key and reset its usage.
// Returns the receiver for chaining.
func (klb *KeyedLeakyBucket) SetMaxKeys(n int) *KeyedLeakyBucket {
	klb.store.mu.Lock()
	defer klb.store.mu.Unlock()
	klb.store.setMaxKeys(n)
	return klb
}

// Allow checks if a request for the key is allowed.
func (klb *KeyedLeakyBucket) Allow(key string) bool {
	return klb.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (klb *KeyedLeakyBucket) AllowN(key string, n int) bool {
	return klb.TakeN(key, n).Allowed
}

// Wait blocks until a request for the key is allowed.
func (klb *KeyedLeakyBucket) Wait(ctx context.Context, key string) error {
	return klb.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (klb *KeyedLeakyBucket) WaitN(ctx context.Context, key string, n int) error {
	if err := validateN(n, klb.capacity); err != nil {
		return err
	}
	for {
		result := klb.TakeN(key, n)
		if result.Allowed {
			return nil
		}
		if err := sleepCtx(ctx, max(result.RetryAfter, time.Millisecond)); err != nil {
			return err
		}
	}
}

// Reset resets the limiter for the given key.
func (klb *KeyedLeakyBucket) Reset(key string) {
	klb.store.mu.Lock()
	defer klb.store.mu.Unlock()
	klb.store.delete(key)
}

// ResetAll resets all keys.
func (klb *KeyedLeakyBucket) ResetAll() {
	klb.store.mu.Lock()
	defer klb.store.mu.Unlock()
	klb.store.reset()
}

// Check returns the current state for a key without consuming.
func (klb *KeyedLeakyBucket) Check(key string) Result {
	return klb.CheckN(key, 1)
}

// CheckN returns the state for n requests without consuming. It does not
// create an entry for an unknown key.
func (klb *KeyedLeakyBucket) CheckN(key string, n int) Result {
	return klb.do(key, n, false)
}

// Take consumes one request for the key and returns the result.
func (klb *KeyedLeakyBucket) Take(key string) Result {
	return klb.TakeN(key, 1)
}

// TakeN consumes n requests for the key and returns the result.
func (klb *KeyedLeakyBucket) TakeN(key string, n int) Result {
	return klb.do(key, n, true)
}

// entry returns the state for key: stored and marked as used when
// consuming, or a read-only view when only checking.
// Must be called with klb.store.mu held.
func (klb *KeyedLeakyBucket) entry(key string, now time.Time, consume bool) *leakyBucketEntry {
	if consume {
		return klb.store.get(key, now)
	}
	return klb.store.peek(key, now)
}

func (klb *KeyedLeakyBucket) do(key string, n int, consume bool) Result {
	if validateN(n, klb.capacity) != nil {
		return Result{Limit: klb.capacity}
	}

	klb.store.mu.Lock()
	defer klb.store.mu.Unlock()

	now := time.Now()
	entry := klb.entry(key, now, consume)
	entry.water = max(entry.water-now.Sub(entry.lastLeak).Seconds()*klb.rate, 0)
	entry.lastLeak = now

	if entry.water+float64(n) <= float64(klb.capacity) {
		if consume {
			entry.water += float64(n)
		}
		return Result{Allowed: true, Limit: klb.capacity, Remaining: int(float64(klb.capacity) - entry.water)}
	}
	overflow := entry.water + float64(n) - float64(klb.capacity)
	return Result{
		Limit:      klb.capacity,
		Remaining:  int(float64(klb.capacity) - entry.water),
		RetryAfter: time.Duration(overflow / klb.rate * float64(time.Second)),
	}
}

// Close stops the cleanup goroutine.
func (klb *KeyedLeakyBucket) Close() {
	klb.store.close()
}

// Len returns the number of active keys.
func (klb *KeyedLeakyBucket) Len() int {
	klb.store.mu.Lock()
	defer klb.store.mu.Unlock()
	return klb.store.len()
}
