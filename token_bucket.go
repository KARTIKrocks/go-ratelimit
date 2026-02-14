package ratelimit

import (
	"context"
	"sync"
	"time"
)

// TokenBucket implements the token bucket algorithm.
// Tokens are added at a fixed rate up to a maximum bucket size (burst).
// Each request consumes one token.
type TokenBucket struct {
	rate       float64   // Tokens per second
	burst      int       // Maximum tokens
	tokens     float64   // Current tokens
	lastUpdate time.Time // Last token update time
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket limiter.
// rate: requests per second
// burst: maximum burst size (bucket capacity)
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if burst <= 0 {
		panic("ratelimit: burst must be positive")
	}
	return &TokenBucket{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst), // Start full
		lastUpdate: time.Now(),
	}
}

// NewTokenBucketPerDuration creates a token bucket with rate specified per duration.
func NewTokenBucketPerDuration(count int, per time.Duration, burst int) *TokenBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewTokenBucket(rate, burst)
}

// update adds tokens based on elapsed time.
func (tb *TokenBucket) update() {
	now := time.Now()
	elapsed := now.Sub(tb.lastUpdate).Seconds()
	tb.lastUpdate = now

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}
}

// Allow checks if a request is allowed.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.update()

	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}

// Wait blocks until a request is allowed.
func (tb *TokenBucket) Wait(ctx context.Context) error {
	return tb.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (tb *TokenBucket) WaitN(ctx context.Context, n int) error {
	for {
		tb.mu.Lock()
		tb.update()

		if tb.tokens >= float64(n) {
			tb.tokens -= float64(n)
			tb.mu.Unlock()
			return nil
		}

		needed := float64(n) - tb.tokens
		waitTime := time.Duration(needed / tb.rate * float64(time.Second))
		tb.mu.Unlock()

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

// Reset resets the bucket to full.
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.tokens = float64(tb.burst)
	tb.lastUpdate = time.Now()
}

// Check returns the current state without consuming tokens.
func (tb *TokenBucket) Check() Result {
	return tb.CheckN(1)
}

// CheckN returns the state for n tokens without consuming.
func (tb *TokenBucket) CheckN(n int) Result {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.update()

	result := Result{
		Limit:     tb.burst,
		Remaining: int(tb.tokens),
	}

	if tb.tokens >= float64(n) {
		result.Allowed = true
	} else {
		result.Allowed = false
		needed := float64(n) - tb.tokens
		result.RetryAfter = time.Duration(needed / tb.rate * float64(time.Second))
	}

	return result
}

// Take consumes a token and returns the result.
func (tb *TokenBucket) Take() Result {
	return tb.TakeN(1)
}

// TakeN consumes n tokens and returns the result.
func (tb *TokenBucket) TakeN(n int) Result {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.update()

	result := Result{
		Limit:     tb.burst,
		Remaining: int(tb.tokens),
	}

	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		result.Allowed = true
		result.Remaining = int(tb.tokens)
	} else {
		result.Allowed = false
		needed := float64(n) - tb.tokens
		result.RetryAfter = time.Duration(needed / tb.rate * float64(time.Second))
	}

	return result
}

// Tokens returns the current number of available tokens.
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.update()
	return tb.tokens
}

// Rate returns the token rate per second.
func (tb *TokenBucket) Rate() float64 {
	return tb.rate
}

// Burst returns the bucket capacity.
func (tb *TokenBucket) Burst() int {
	return tb.burst
}

// KeyedTokenBucket provides per-key token bucket rate limiting.
type KeyedTokenBucket struct {
	rate            float64
	burst           int
	maxKeys         int
	buckets         map[string]*tokenBucketEntry
	mu              sync.RWMutex
	cleanupInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
}

type tokenBucketEntry struct {
	tokens     float64
	lastUpdate time.Time
	lastAccess time.Time
}

// NewKeyedTokenBucket creates a new keyed token bucket limiter.
// Use SetMaxKeys to limit the number of tracked keys (0 = unlimited).
func NewKeyedTokenBucket(rate float64, burst int, cleanupInterval time.Duration) *KeyedTokenBucket {
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if burst <= 0 {
		panic("ratelimit: burst must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	kb := &KeyedTokenBucket{
		rate:            rate,
		burst:           burst,
		buckets:         make(map[string]*tokenBucketEntry),
		cleanupInterval: cleanupInterval,
		ctx:             ctx,
		cancel:          cancel,
	}

	if cleanupInterval > 0 {
		go kb.cleanup()
	}

	return kb
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, requests for new keys are denied. Zero means unlimited.
// Returns the receiver for chaining.
func (kb *KeyedTokenBucket) SetMaxKeys(n int) *KeyedTokenBucket {
	kb.mu.Lock()
	defer kb.mu.Unlock()
	kb.maxKeys = n
	return kb
}

// NewKeyedTokenBucketPerDuration creates a keyed token bucket with rate per duration.
func NewKeyedTokenBucketPerDuration(count int, per time.Duration, burst int, cleanupInterval time.Duration) *KeyedTokenBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewKeyedTokenBucket(rate, burst, cleanupInterval)
}

// cleanup removes inactive buckets.
func (kb *KeyedTokenBucket) cleanup() {
	ticker := time.NewTicker(kb.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-kb.ctx.Done():
			return
		case now := <-ticker.C:
			kb.mu.Lock()
			cutoff := now.Add(-kb.cleanupInterval * 2)
			for key, entry := range kb.buckets {
				if entry.lastAccess.Before(cutoff) {
					delete(kb.buckets, key)
				}
			}
			kb.mu.Unlock()
		}
	}
}

// getOrCreate gets or creates a bucket entry for a key.
// Must be called with kb.mu held. Returns nil if maxKeys is reached
// and the key doesn't already exist.
func (kb *KeyedTokenBucket) getOrCreate(key string) *tokenBucketEntry {
	entry, ok := kb.buckets[key]
	if !ok {
		if kb.maxKeys > 0 && len(kb.buckets) >= kb.maxKeys {
			return nil
		}
		entry = &tokenBucketEntry{
			tokens:     float64(kb.burst),
			lastUpdate: time.Now(),
			lastAccess: time.Now(),
		}
		kb.buckets[key] = entry
	}
	return entry
}

// update adds tokens based on elapsed time.
// Must be called with kb.mu held.
func (kb *KeyedTokenBucket) update(entry *tokenBucketEntry) {
	now := time.Now()
	elapsed := now.Sub(entry.lastUpdate).Seconds()
	entry.lastUpdate = now
	entry.lastAccess = now

	entry.tokens += elapsed * kb.rate
	if entry.tokens > float64(kb.burst) {
		entry.tokens = float64(kb.burst)
	}
}

// Allow checks if a request for the key is allowed.
func (kb *KeyedTokenBucket) Allow(key string) bool {
	return kb.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (kb *KeyedTokenBucket) AllowN(key string, n int) bool {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	entry := kb.getOrCreate(key)
	if entry == nil {
		return false
	}
	kb.update(entry)

	if entry.tokens >= float64(n) {
		entry.tokens -= float64(n)
		return true
	}
	return false
}

// Wait blocks until a request for the key is allowed.
func (kb *KeyedTokenBucket) Wait(ctx context.Context, key string) error {
	return kb.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (kb *KeyedTokenBucket) WaitN(ctx context.Context, key string, n int) error {
	for {
		kb.mu.Lock()
		entry := kb.getOrCreate(key)
		if entry == nil {
			kb.mu.Unlock()
			return ErrRateLimitExceeded
		}
		kb.update(entry)

		if entry.tokens >= float64(n) {
			entry.tokens -= float64(n)
			kb.mu.Unlock()
			return nil
		}

		needed := float64(n) - entry.tokens
		waitTime := time.Duration(needed / kb.rate * float64(time.Second))
		kb.mu.Unlock()

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
func (kb *KeyedTokenBucket) Reset(key string) {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	delete(kb.buckets, key)
}

// ResetAll resets all keys.
func (kb *KeyedTokenBucket) ResetAll() {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	kb.buckets = make(map[string]*tokenBucketEntry)
}

// Check returns the current state for a key without consuming tokens.
func (kb *KeyedTokenBucket) Check(key string) Result {
	return kb.CheckN(key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (kb *KeyedTokenBucket) CheckN(key string, n int) Result {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	entry := kb.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: kb.burst, Remaining: 0}
	}
	kb.update(entry)

	result := Result{
		Limit:     kb.burst,
		Remaining: int(entry.tokens),
	}

	if entry.tokens >= float64(n) {
		result.Allowed = true
	} else {
		result.Allowed = false
		needed := float64(n) - entry.tokens
		result.RetryAfter = time.Duration(needed / kb.rate * float64(time.Second))
	}

	return result
}

// Take consumes a token for the key and returns the result.
func (kb *KeyedTokenBucket) Take(key string) Result {
	return kb.TakeN(key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (kb *KeyedTokenBucket) TakeN(key string, n int) Result {
	kb.mu.Lock()
	defer kb.mu.Unlock()

	entry := kb.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: kb.burst, Remaining: 0}
	}
	kb.update(entry)

	result := Result{
		Limit:     kb.burst,
		Remaining: int(entry.tokens),
	}

	if entry.tokens >= float64(n) {
		entry.tokens -= float64(n)
		result.Allowed = true
		result.Remaining = int(entry.tokens)
	} else {
		result.Allowed = false
		needed := float64(n) - entry.tokens
		result.RetryAfter = time.Duration(needed / kb.rate * float64(time.Second))
	}

	return result
}

// Close stops the cleanup goroutine.
func (kb *KeyedTokenBucket) Close() {
	kb.cancel()
}

// Len returns the number of active keys.
func (kb *KeyedTokenBucket) Len() int {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	return len(kb.buckets)
}
