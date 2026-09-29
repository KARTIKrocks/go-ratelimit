package ratelimit

import (
	"context"
	"sync"
	"time"
)

// FixedWindow implements the fixed window algorithm.
// Simple and memory-efficient but can allow 2x burst at window boundaries.
type FixedWindow struct {
	limit       int
	window      time.Duration
	count       int
	windowStart time.Time
	mu          sync.Mutex
}

// NewFixedWindow creates a new fixed window limiter.
func NewFixedWindow(limit int, window time.Duration) *FixedWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &FixedWindow{
		limit:       limit,
		window:      window,
		windowStart: time.Now().Truncate(window),
	}
}

// update resets the window if needed.
func (fw *FixedWindow) update(now time.Time) {
	windowStart := now.Truncate(fw.window)
	if windowStart.After(fw.windowStart) {
		fw.count = 0
		fw.windowStart = windowStart
	}
}

// Allow checks if a request is allowed.
func (fw *FixedWindow) Allow() bool {
	return fw.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (fw *FixedWindow) AllowN(n int) bool {
	if validateN(n, fw.limit) != nil {
		return false
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now()
	fw.update(now)

	if fw.count+n <= fw.limit {
		fw.count += n
		return true
	}
	return false
}

// Wait blocks until a request is allowed.
func (fw *FixedWindow) Wait(ctx context.Context) error {
	return fw.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (fw *FixedWindow) WaitN(ctx context.Context, n int) error {
	if err := validateN(n, fw.limit); err != nil {
		return err
	}

	for {
		fw.mu.Lock()
		now := time.Now()
		fw.update(now)

		if fw.count+n <= fw.limit {
			fw.count += n
			fw.mu.Unlock()
			return nil
		}

		waitTime := fw.windowStart.Add(fw.window).Sub(now)
		if waitTime < 0 {
			waitTime = time.Millisecond
		}
		fw.mu.Unlock()

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

// Reset resets the limiter.
func (fw *FixedWindow) Reset() {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.count = 0
	fw.windowStart = time.Now().Truncate(fw.window)
}

// Check returns the current state without consuming.
func (fw *FixedWindow) Check() Result {
	return fw.CheckN(1)
}

// CheckN returns the state for n requests without consuming.
func (fw *FixedWindow) CheckN(n int) Result {
	if validateN(n, fw.limit) != nil {
		return Result{Limit: fw.limit}
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now()
	fw.update(now)

	result := Result{
		Limit:     fw.limit,
		Remaining: fw.limit - fw.count,
		ResetAt:   fw.windowStart.Add(fw.window),
	}

	if fw.count+n <= fw.limit {
		result.Allowed = true
	} else {
		result.Allowed = false
		result.RetryAfter = fw.windowStart.Add(fw.window).Sub(now)
	}

	return result
}

// Take consumes a token and returns the result.
func (fw *FixedWindow) Take() Result {
	return fw.TakeN(1)
}

// TakeN consumes n tokens and returns the result.
func (fw *FixedWindow) TakeN(n int) Result {
	if validateN(n, fw.limit) != nil {
		return Result{Limit: fw.limit}
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now()
	fw.update(now)

	result := Result{
		Limit:     fw.limit,
		Remaining: fw.limit - fw.count,
		ResetAt:   fw.windowStart.Add(fw.window),
	}

	if fw.count+n <= fw.limit {
		fw.count += n
		result.Allowed = true
		result.Remaining = fw.limit - fw.count
	} else {
		result.Allowed = false
		result.RetryAfter = fw.windowStart.Add(fw.window).Sub(now)
	}

	return result
}

// Count returns the current request count in this window.
func (fw *FixedWindow) Count() int {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	fw.update(time.Now())
	return fw.count
}

// KeyedFixedWindow provides per-key fixed window rate limiting.
type KeyedFixedWindow struct {
	limit  int
	window time.Duration
	store  *keyedStore[fixedWindowEntry]
}

type fixedWindowEntry struct {
	count       int
	windowStart time.Time
}

// NewKeyedFixedWindow creates a new keyed fixed window limiter.
// If cleanupInterval is positive, keys idle for twice that long are removed
// in the background until Close. Use SetMaxKeys to bound memory.
func NewKeyedFixedWindow(limit int, window time.Duration, cleanupInterval time.Duration) *KeyedFixedWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &KeyedFixedWindow{
		limit:  limit,
		window: window,
		store: newKeyedStore(cleanupInterval, func(now time.Time) fixedWindowEntry {
			return fixedWindowEntry{windowStart: now.Truncate(window)}
		}),
	}
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, the least recently used key is evicted to make room for a new
// one; an evicted key starts again with no usage. Zero means unlimited.
// Returns the receiver for chaining.
func (kfw *KeyedFixedWindow) SetMaxKeys(n int) *KeyedFixedWindow {
	kfw.store.mu.Lock()
	defer kfw.store.mu.Unlock()
	kfw.store.setMaxKeys(n)
	return kfw
}

// Allow checks if a request for the key is allowed.
func (kfw *KeyedFixedWindow) Allow(key string) bool {
	return kfw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (kfw *KeyedFixedWindow) AllowN(key string, n int) bool {
	return kfw.TakeN(key, n).Allowed
}

// Wait blocks until a request for the key is allowed.
func (kfw *KeyedFixedWindow) Wait(ctx context.Context, key string) error {
	return kfw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (kfw *KeyedFixedWindow) WaitN(ctx context.Context, key string, n int) error {
	if err := validateN(n, kfw.limit); err != nil {
		return err
	}
	for {
		result := kfw.TakeN(key, n)
		if result.Allowed {
			return nil
		}
		if err := sleepCtx(ctx, max(result.RetryAfter, time.Millisecond)); err != nil {
			return err
		}
	}
}

// Reset resets the limiter for the given key.
func (kfw *KeyedFixedWindow) Reset(key string) {
	kfw.store.mu.Lock()
	defer kfw.store.mu.Unlock()
	kfw.store.delete(key)
}

// ResetAll resets all keys.
func (kfw *KeyedFixedWindow) ResetAll() {
	kfw.store.mu.Lock()
	defer kfw.store.mu.Unlock()
	kfw.store.reset()
}

// Check returns the current state for a key without consuming.
func (kfw *KeyedFixedWindow) Check(key string) Result {
	return kfw.CheckN(key, 1)
}

// CheckN returns the state for n requests without consuming. It does not
// create an entry for an unknown key.
func (kfw *KeyedFixedWindow) CheckN(key string, n int) Result {
	return kfw.do(key, n, false)
}

// Take consumes one request for the key and returns the result.
func (kfw *KeyedFixedWindow) Take(key string) Result {
	return kfw.TakeN(key, 1)
}

// TakeN consumes n requests for the key and returns the result.
func (kfw *KeyedFixedWindow) TakeN(key string, n int) Result {
	return kfw.do(key, n, true)
}

// entry returns the state for key: stored and marked as used when
// consuming, or a read-only view when only checking.
// Must be called with kfw.store.mu held.
func (kfw *KeyedFixedWindow) entry(key string, now time.Time, consume bool) *fixedWindowEntry {
	if consume {
		return kfw.store.get(key, now)
	}
	return kfw.store.peek(key, now)
}

func (kfw *KeyedFixedWindow) do(key string, n int, consume bool) Result {
	if validateN(n, kfw.limit) != nil {
		return Result{Limit: kfw.limit}
	}

	kfw.store.mu.Lock()
	defer kfw.store.mu.Unlock()

	now := time.Now()
	entry := kfw.entry(key, now, consume)
	if windowStart := now.Truncate(kfw.window); windowStart.After(entry.windowStart) {
		entry.count = 0
		entry.windowStart = windowStart
	}

	resetAt := entry.windowStart.Add(kfw.window)
	if entry.count+n <= kfw.limit {
		if consume {
			entry.count += n
		}
		return Result{Allowed: true, Limit: kfw.limit, Remaining: kfw.limit - entry.count, ResetAt: resetAt}
	}
	return Result{
		Limit:      kfw.limit,
		Remaining:  kfw.limit - entry.count,
		ResetAt:    resetAt,
		RetryAfter: resetAt.Sub(now),
	}
}

// Close stops the cleanup goroutine.
func (kfw *KeyedFixedWindow) Close() {
	kfw.store.close()
}

// Len returns the number of active keys.
func (kfw *KeyedFixedWindow) Len() int {
	kfw.store.mu.Lock()
	defer kfw.store.mu.Unlock()
	return kfw.store.len()
}
