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
	limit           int
	window          time.Duration
	maxKeys         int
	entries         map[string]*fixedWindowEntry
	mu              sync.RWMutex
	cleanupInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
}

type fixedWindowEntry struct {
	count       int
	windowStart time.Time
	lastAccess  time.Time
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, requests for new keys are denied. Zero means unlimited.
// Returns the receiver for chaining.
func (kfw *KeyedFixedWindow) SetMaxKeys(n int) *KeyedFixedWindow {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()
	kfw.maxKeys = n
	return kfw
}

// NewKeyedFixedWindow creates a new keyed fixed window limiter.
func NewKeyedFixedWindow(limit int, window time.Duration, cleanupInterval time.Duration) *KeyedFixedWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	kfw := &KeyedFixedWindow{
		limit:           limit,
		window:          window,
		entries:         make(map[string]*fixedWindowEntry),
		cleanupInterval: cleanupInterval,
		ctx:             ctx,
		cancel:          cancel,
	}

	if cleanupInterval > 0 {
		go kfw.cleanup()
	}

	return kfw
}

// cleanup removes inactive entries.
func (kfw *KeyedFixedWindow) cleanup() {
	ticker := time.NewTicker(kfw.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-kfw.ctx.Done():
			return
		case now := <-ticker.C:
			kfw.mu.Lock()
			cutoff := now.Add(-kfw.cleanupInterval * 2)
			for key, entry := range kfw.entries {
				if entry.lastAccess.Before(cutoff) {
					delete(kfw.entries, key)
				}
			}
			kfw.mu.Unlock()
		}
	}
}

// getOrCreate gets or creates an entry for a key.
// Must be called with kfw.mu held. Returns nil if maxKeys is reached.
func (kfw *KeyedFixedWindow) getOrCreate(key string) *fixedWindowEntry {
	entry, ok := kfw.entries[key]
	if !ok {
		if kfw.maxKeys > 0 && len(kfw.entries) >= kfw.maxKeys {
			return nil
		}
		entry = &fixedWindowEntry{
			windowStart: time.Now().Truncate(kfw.window),
			lastAccess:  time.Now(),
		}
		kfw.entries[key] = entry
	}
	return entry
}

// update resets the entry's window if needed.
// Must be called with kfw.mu held.
func (kfw *KeyedFixedWindow) update(entry *fixedWindowEntry, now time.Time) {
	entry.lastAccess = now
	windowStart := now.Truncate(kfw.window)
	if windowStart.After(entry.windowStart) {
		entry.count = 0
		entry.windowStart = windowStart
	}
}

// Allow checks if a request for the key is allowed.
func (kfw *KeyedFixedWindow) Allow(key string) bool {
	return kfw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (kfw *KeyedFixedWindow) AllowN(key string, n int) bool {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()

	entry := kfw.getOrCreate(key)
	if entry == nil {
		return false
	}
	now := time.Now()
	kfw.update(entry, now)

	if entry.count+n <= kfw.limit {
		entry.count += n
		return true
	}
	return false
}

// Wait blocks until a request for the key is allowed.
func (kfw *KeyedFixedWindow) Wait(ctx context.Context, key string) error {
	return kfw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (kfw *KeyedFixedWindow) WaitN(ctx context.Context, key string, n int) error {
	for {
		kfw.mu.Lock()
		entry := kfw.getOrCreate(key)
		if entry == nil {
			kfw.mu.Unlock()
			return ErrRateLimitExceeded
		}
		now := time.Now()
		kfw.update(entry, now)

		if entry.count+n <= kfw.limit {
			entry.count += n
			kfw.mu.Unlock()
			return nil
		}

		waitTime := entry.windowStart.Add(kfw.window).Sub(now)
		if waitTime < 0 {
			waitTime = time.Millisecond
		}
		kfw.mu.Unlock()

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
func (kfw *KeyedFixedWindow) Reset(key string) {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()

	delete(kfw.entries, key)
}

// ResetAll resets all keys.
func (kfw *KeyedFixedWindow) ResetAll() {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()

	kfw.entries = make(map[string]*fixedWindowEntry)
}

// Take consumes a token for the key and returns the result.
func (kfw *KeyedFixedWindow) Take(key string) Result {
	return kfw.TakeN(key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (kfw *KeyedFixedWindow) TakeN(key string, n int) Result {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()

	entry := kfw.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: kfw.limit, Remaining: 0}
	}
	now := time.Now()
	kfw.update(entry, now)

	result := Result{
		Limit:     kfw.limit,
		Remaining: kfw.limit - entry.count,
		ResetAt:   entry.windowStart.Add(kfw.window),
	}

	if entry.count+n <= kfw.limit {
		entry.count += n
		result.Allowed = true
		result.Remaining = kfw.limit - entry.count
	} else {
		result.Allowed = false
		result.RetryAfter = entry.windowStart.Add(kfw.window).Sub(now)
	}

	return result
}

// Check returns the current state for a key without consuming.
func (kfw *KeyedFixedWindow) Check(key string) Result {
	return kfw.CheckN(key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (kfw *KeyedFixedWindow) CheckN(key string, n int) Result {
	kfw.mu.Lock()
	defer kfw.mu.Unlock()

	entry := kfw.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: kfw.limit, Remaining: 0}
	}
	now := time.Now()
	kfw.update(entry, now)

	result := Result{
		Limit:     kfw.limit,
		Remaining: kfw.limit - entry.count,
		ResetAt:   entry.windowStart.Add(kfw.window),
	}

	if entry.count+n <= kfw.limit {
		result.Allowed = true
	} else {
		result.Allowed = false
		result.RetryAfter = entry.windowStart.Add(kfw.window).Sub(now)
	}

	return result
}

// Close stops the cleanup goroutine.
func (kfw *KeyedFixedWindow) Close() {
	kfw.cancel()
}

// Len returns the number of active keys.
func (kfw *KeyedFixedWindow) Len() int {
	kfw.mu.RLock()
	defer kfw.mu.RUnlock()
	return len(kfw.entries)
}
