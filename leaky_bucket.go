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
	rate            float64
	capacity        int
	maxKeys         int
	buckets         map[string]*leakyBucketEntry
	mu              sync.RWMutex
	cleanupInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
}

type leakyBucketEntry struct {
	water      float64
	lastLeak   time.Time
	lastAccess time.Time
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, requests for new keys are denied. Zero means unlimited.
// Returns the receiver for chaining.
func (klb *KeyedLeakyBucket) SetMaxKeys(n int) *KeyedLeakyBucket {
	klb.mu.Lock()
	defer klb.mu.Unlock()
	klb.maxKeys = n
	return klb
}

// NewKeyedLeakyBucket creates a new keyed leaky bucket limiter.
func NewKeyedLeakyBucket(rate float64, capacity int, cleanupInterval time.Duration) *KeyedLeakyBucket {
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if capacity <= 0 {
		panic("ratelimit: capacity must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	klb := &KeyedLeakyBucket{
		rate:            rate,
		capacity:        capacity,
		buckets:         make(map[string]*leakyBucketEntry),
		cleanupInterval: cleanupInterval,
		ctx:             ctx,
		cancel:          cancel,
	}

	if cleanupInterval > 0 {
		go klb.cleanup()
	}

	return klb
}

// NewKeyedLeakyBucketPerDuration creates a keyed leaky bucket with rate per duration.
func NewKeyedLeakyBucketPerDuration(count int, per time.Duration, capacity int, cleanupInterval time.Duration) *KeyedLeakyBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewKeyedLeakyBucket(rate, capacity, cleanupInterval)
}

// cleanup removes inactive buckets.
func (klb *KeyedLeakyBucket) cleanup() {
	ticker := time.NewTicker(klb.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-klb.ctx.Done():
			return
		case now := <-ticker.C:
			klb.mu.Lock()
			cutoff := now.Add(-klb.cleanupInterval * 2)
			for key, entry := range klb.buckets {
				if entry.lastAccess.Before(cutoff) {
					delete(klb.buckets, key)
				}
			}
			klb.mu.Unlock()
		}
	}
}

// getOrCreate gets or creates a bucket entry for a key.
// Must be called with klb.mu held. Returns nil if maxKeys is reached.
func (klb *KeyedLeakyBucket) getOrCreate(key string) *leakyBucketEntry {
	entry, ok := klb.buckets[key]
	if !ok {
		if klb.maxKeys > 0 && len(klb.buckets) >= klb.maxKeys {
			return nil
		}
		entry = &leakyBucketEntry{
			water:      0,
			lastLeak:   time.Now(),
			lastAccess: time.Now(),
		}
		klb.buckets[key] = entry
	}
	return entry
}

// leak removes water based on elapsed time.
// Must be called with klb.mu held.
func (klb *KeyedLeakyBucket) leak(entry *leakyBucketEntry) {
	now := time.Now()
	elapsed := now.Sub(entry.lastLeak).Seconds()
	entry.lastLeak = now
	entry.lastAccess = now

	entry.water -= elapsed * klb.rate
	if entry.water < 0 {
		entry.water = 0
	}
}

// Allow checks if a request for the key is allowed.
func (klb *KeyedLeakyBucket) Allow(key string) bool {
	return klb.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (klb *KeyedLeakyBucket) AllowN(key string, n int) bool {
	klb.mu.Lock()
	defer klb.mu.Unlock()

	entry := klb.getOrCreate(key)
	if entry == nil {
		return false
	}
	klb.leak(entry)

	if entry.water+float64(n) <= float64(klb.capacity) {
		entry.water += float64(n)
		return true
	}
	return false
}

// Wait blocks until a request for the key is allowed.
func (klb *KeyedLeakyBucket) Wait(ctx context.Context, key string) error {
	return klb.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (klb *KeyedLeakyBucket) WaitN(ctx context.Context, key string, n int) error {
	for {
		klb.mu.Lock()
		entry := klb.getOrCreate(key)
		if entry == nil {
			klb.mu.Unlock()
			return ErrRateLimitExceeded
		}
		klb.leak(entry)

		if entry.water+float64(n) <= float64(klb.capacity) {
			entry.water += float64(n)
			klb.mu.Unlock()
			return nil
		}

		overflow := entry.water + float64(n) - float64(klb.capacity)
		waitTime := time.Duration(overflow / klb.rate * float64(time.Second))
		klb.mu.Unlock()

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

// Reset resets the limiter for the given key.
func (klb *KeyedLeakyBucket) Reset(key string) {
	klb.mu.Lock()
	defer klb.mu.Unlock()

	delete(klb.buckets, key)
}

// ResetAll resets all keys.
func (klb *KeyedLeakyBucket) ResetAll() {
	klb.mu.Lock()
	defer klb.mu.Unlock()

	klb.buckets = make(map[string]*leakyBucketEntry)
}

// Take consumes a token for the key and returns the result.
func (klb *KeyedLeakyBucket) Take(key string) Result {
	return klb.TakeN(key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (klb *KeyedLeakyBucket) TakeN(key string, n int) Result {
	klb.mu.Lock()
	defer klb.mu.Unlock()

	entry := klb.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: klb.capacity, Remaining: 0}
	}
	klb.leak(entry)

	result := Result{
		Limit:     klb.capacity,
		Remaining: int(float64(klb.capacity) - entry.water),
	}

	if entry.water+float64(n) <= float64(klb.capacity) {
		entry.water += float64(n)
		result.Allowed = true
		result.Remaining = int(float64(klb.capacity) - entry.water)
	} else {
		result.Allowed = false
		overflow := entry.water + float64(n) - float64(klb.capacity)
		result.RetryAfter = time.Duration(overflow / klb.rate * float64(time.Second))
	}

	return result
}

// Check returns the current state for a key without consuming.
func (klb *KeyedLeakyBucket) Check(key string) Result {
	return klb.CheckN(key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (klb *KeyedLeakyBucket) CheckN(key string, n int) Result {
	klb.mu.Lock()
	defer klb.mu.Unlock()

	entry := klb.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: klb.capacity, Remaining: 0}
	}
	klb.leak(entry)

	result := Result{
		Limit:     klb.capacity,
		Remaining: int(float64(klb.capacity) - entry.water),
	}

	if entry.water+float64(n) <= float64(klb.capacity) {
		result.Allowed = true
	} else {
		result.Allowed = false
		overflow := entry.water + float64(n) - float64(klb.capacity)
		result.RetryAfter = time.Duration(overflow / klb.rate * float64(time.Second))
	}

	return result
}

// Close stops the cleanup goroutine.
func (klb *KeyedLeakyBucket) Close() {
	klb.cancel()
}

// Len returns the number of active keys.
func (klb *KeyedLeakyBucket) Len() int {
	klb.mu.RLock()
	defer klb.mu.RUnlock()
	return len(klb.buckets)
}
