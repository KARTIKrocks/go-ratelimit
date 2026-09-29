package ratelimit

import (
	"context"
	"sync"
	"time"
)

// SlidingWindow implements the sliding window log algorithm.
// It tracks each request timestamp and counts requests within the window.
// More accurate than fixed window but uses more memory.
type SlidingWindow struct {
	limit      int
	window     time.Duration
	timestamps []time.Time
	mu         sync.Mutex
}

// NewSlidingWindow creates a new sliding window limiter.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &SlidingWindow{
		limit:      limit,
		window:     window,
		timestamps: make([]time.Time, 0, limit),
	}
}

// cleanup removes expired timestamps.
func (sw *SlidingWindow) cleanup(now time.Time) {
	cutoff := now.Add(-sw.window)
	i := 0
	for ; i < len(sw.timestamps); i++ {
		if sw.timestamps[i].After(cutoff) {
			break
		}
	}
	if i > 0 {
		remaining := len(sw.timestamps) - i
		// Copy to new slice to allow garbage collection of old backing array
		// when more than half the capacity is wasted.
		if remaining < cap(sw.timestamps)/2 {
			newTimestamps := make([]time.Time, remaining, remaining+sw.limit)
			copy(newTimestamps, sw.timestamps[i:])
			sw.timestamps = newTimestamps
		} else {
			sw.timestamps = sw.timestamps[i:]
		}
	}
}

// Allow checks if a request is allowed.
func (sw *SlidingWindow) Allow() bool {
	return sw.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (sw *SlidingWindow) AllowN(n int) bool {
	if validateN(n, sw.limit) != nil {
		return false
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.cleanup(now)

	if len(sw.timestamps)+n <= sw.limit {
		for range n {
			sw.timestamps = append(sw.timestamps, now)
		}
		return true
	}
	return false
}

// Wait blocks until a request is allowed.
func (sw *SlidingWindow) Wait(ctx context.Context) error {
	return sw.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (sw *SlidingWindow) WaitN(ctx context.Context, n int) error {
	if err := validateN(n, sw.limit); err != nil {
		return err
	}

	for {
		sw.mu.Lock()
		now := time.Now()
		sw.cleanup(now)

		if len(sw.timestamps)+n <= sw.limit {
			for range n {
				sw.timestamps = append(sw.timestamps, now)
			}
			sw.mu.Unlock()
			return nil
		}

		var waitTime time.Duration
		if len(sw.timestamps) > 0 {
			waitTime = sw.timestamps[0].Add(sw.window).Sub(now)
			if waitTime < 0 {
				waitTime = time.Millisecond
			}
		} else {
			waitTime = time.Millisecond
		}
		sw.mu.Unlock()

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
func (sw *SlidingWindow) Reset() {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.timestamps = sw.timestamps[:0]
}

// Check returns the current state without consuming.
func (sw *SlidingWindow) Check() Result {
	return sw.CheckN(1)
}

// CheckN returns the state for n requests without consuming.
func (sw *SlidingWindow) CheckN(n int) Result {
	if validateN(n, sw.limit) != nil {
		return Result{Limit: sw.limit}
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.cleanup(now)

	result := Result{
		Limit:     sw.limit,
		Remaining: sw.limit - len(sw.timestamps),
	}

	if len(sw.timestamps)+n <= sw.limit {
		result.Allowed = true
	} else {
		result.Allowed = false
		if len(sw.timestamps) > 0 {
			result.RetryAfter = sw.timestamps[0].Add(sw.window).Sub(now)
			result.ResetAt = sw.timestamps[0].Add(sw.window)
		}
	}

	return result
}

// Take consumes a token and returns the result.
func (sw *SlidingWindow) Take() Result {
	return sw.TakeN(1)
}

// TakeN consumes n tokens and returns the result.
func (sw *SlidingWindow) TakeN(n int) Result {
	if validateN(n, sw.limit) != nil {
		return Result{Limit: sw.limit}
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.cleanup(now)

	result := Result{
		Limit:     sw.limit,
		Remaining: sw.limit - len(sw.timestamps),
	}

	if len(sw.timestamps)+n <= sw.limit {
		for range n {
			sw.timestamps = append(sw.timestamps, now)
		}
		result.Allowed = true
		result.Remaining = sw.limit - len(sw.timestamps)
	} else {
		result.Allowed = false
		if len(sw.timestamps) > 0 {
			result.RetryAfter = sw.timestamps[0].Add(sw.window).Sub(now)
			result.ResetAt = sw.timestamps[0].Add(sw.window)
		}
	}

	return result
}

// Count returns the current request count.
func (sw *SlidingWindow) Count() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.cleanup(time.Now())
	return len(sw.timestamps)
}

// SlidingWindowCounter implements the sliding window counter algorithm.
// A memory-efficient approximation using the previous and current window counts.
type SlidingWindowCounter struct {
	limit       int
	window      time.Duration
	prevCount   int
	currCount   int
	windowStart time.Time
	mu          sync.Mutex
}

// NewSlidingWindowCounter creates a new sliding window counter limiter.
func NewSlidingWindowCounter(limit int, window time.Duration) *SlidingWindowCounter {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &SlidingWindowCounter{
		limit:       limit,
		window:      window,
		windowStart: time.Now().Truncate(window),
	}
}

// update updates the window if needed.
func (swc *SlidingWindowCounter) update(now time.Time) {
	windowStart := now.Truncate(swc.window)

	if windowStart.After(swc.windowStart) {
		// Check if we're in the next window or further
		if windowStart.Sub(swc.windowStart) >= swc.window*2 {
			// More than one window has passed
			swc.prevCount = 0
			swc.currCount = 0
		} else {
			// Move to next window
			swc.prevCount = swc.currCount
			swc.currCount = 0
		}
		swc.windowStart = windowStart
	}
}

// count returns the weighted count.
func (swc *SlidingWindowCounter) count(now time.Time) float64 {
	// Calculate position within current window
	elapsed := now.Sub(swc.windowStart)
	weight := elapsed.Seconds() / swc.window.Seconds()

	// Weighted count: previous window * (1 - weight) + current window
	return float64(swc.prevCount)*(1-weight) + float64(swc.currCount)
}

// Allow checks if a request is allowed.
func (swc *SlidingWindowCounter) Allow() bool {
	return swc.AllowN(1)
}

// AllowN checks if n requests are allowed.
func (swc *SlidingWindowCounter) AllowN(n int) bool {
	if validateN(n, swc.limit) != nil {
		return false
	}

	swc.mu.Lock()
	defer swc.mu.Unlock()

	now := time.Now()
	swc.update(now)

	if swc.count(now)+float64(n) <= float64(swc.limit) {
		swc.currCount += n
		return true
	}
	return false
}

// Wait blocks until a request is allowed.
func (swc *SlidingWindowCounter) Wait(ctx context.Context) error {
	return swc.WaitN(ctx, 1)
}

// WaitN blocks until n requests are allowed.
func (swc *SlidingWindowCounter) WaitN(ctx context.Context, n int) error {
	if err := validateN(n, swc.limit); err != nil {
		return err
	}

	for {
		swc.mu.Lock()
		now := time.Now()
		swc.update(now)

		if swc.count(now)+float64(n) <= float64(swc.limit) {
			swc.currCount += n
			swc.mu.Unlock()
			return nil
		}

		waitTime := swc.windowStart.Add(swc.window).Sub(now)
		if waitTime < 0 {
			waitTime = time.Millisecond
		}
		swc.mu.Unlock()

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
func (swc *SlidingWindowCounter) Reset() {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	swc.prevCount = 0
	swc.currCount = 0
	swc.windowStart = time.Now().Truncate(swc.window)
}

// Check returns the current state without consuming.
func (swc *SlidingWindowCounter) Check() Result {
	return swc.CheckN(1)
}

// CheckN returns the state for n requests without consuming.
func (swc *SlidingWindowCounter) CheckN(n int) Result {
	if validateN(n, swc.limit) != nil {
		return Result{Limit: swc.limit}
	}

	swc.mu.Lock()
	defer swc.mu.Unlock()

	now := time.Now()
	swc.update(now)

	currentCount := swc.count(now)
	result := Result{
		Limit:     swc.limit,
		Remaining: int(float64(swc.limit) - currentCount),
		ResetAt:   swc.windowStart.Add(swc.window),
	}

	if currentCount+float64(n) <= float64(swc.limit) {
		result.Allowed = true
	} else {
		result.Allowed = false
		result.RetryAfter = swc.windowStart.Add(swc.window).Sub(now)
	}

	return result
}

// Take consumes a token and returns the result.
func (swc *SlidingWindowCounter) Take() Result {
	return swc.TakeN(1)
}

// TakeN consumes n tokens and returns the result.
func (swc *SlidingWindowCounter) TakeN(n int) Result {
	if validateN(n, swc.limit) != nil {
		return Result{Limit: swc.limit}
	}

	swc.mu.Lock()
	defer swc.mu.Unlock()

	now := time.Now()
	swc.update(now)

	currentCount := swc.count(now)
	result := Result{
		Limit:     swc.limit,
		Remaining: int(float64(swc.limit) - currentCount),
		ResetAt:   swc.windowStart.Add(swc.window),
	}

	if currentCount+float64(n) <= float64(swc.limit) {
		swc.currCount += n
		result.Allowed = true
		result.Remaining = int(float64(swc.limit) - swc.count(now))
	} else {
		result.Allowed = false
		result.RetryAfter = swc.windowStart.Add(swc.window).Sub(now)
	}

	return result
}

// KeyedSlidingWindow provides per-key sliding window rate limiting.
type KeyedSlidingWindow struct {
	limit  int
	window time.Duration
	store  *keyedStore[slidingWindowEntry]
}

type slidingWindowEntry struct {
	prevCount   int
	currCount   int
	windowStart time.Time
}

// NewKeyedSlidingWindow creates a new keyed sliding window limiter.
// If cleanupInterval is positive, keys idle for twice that long are removed
// in the background until Close. Use SetMaxKeys to bound memory.
func NewKeyedSlidingWindow(limit int, window time.Duration, cleanupInterval time.Duration) *KeyedSlidingWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &KeyedSlidingWindow{
		limit:  limit,
		window: window,
		store: newKeyedStore(cleanupInterval, func(now time.Time) slidingWindowEntry {
			return slidingWindowEntry{windowStart: now.Truncate(window)}
		}),
	}
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, the least recently used key is evicted to make room for a new
// one; an evicted key starts again with no usage. Zero means unlimited.
// Returns the receiver for chaining.
func (ksw *KeyedSlidingWindow) SetMaxKeys(n int) *KeyedSlidingWindow {
	ksw.store.mu.Lock()
	defer ksw.store.mu.Unlock()
	ksw.store.setMaxKeys(n)
	return ksw
}

// Allow checks if a request for the key is allowed.
func (ksw *KeyedSlidingWindow) Allow(key string) bool {
	return ksw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (ksw *KeyedSlidingWindow) AllowN(key string, n int) bool {
	return ksw.TakeN(key, n).Allowed
}

// Wait blocks until a request for the key is allowed.
func (ksw *KeyedSlidingWindow) Wait(ctx context.Context, key string) error {
	return ksw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (ksw *KeyedSlidingWindow) WaitN(ctx context.Context, key string, n int) error {
	if err := validateN(n, ksw.limit); err != nil {
		return err
	}
	for {
		result := ksw.TakeN(key, n)
		if result.Allowed {
			return nil
		}
		if err := sleepCtx(ctx, max(result.RetryAfter, time.Millisecond)); err != nil {
			return err
		}
	}
}

// Reset resets the limiter for the given key.
func (ksw *KeyedSlidingWindow) Reset(key string) {
	ksw.store.mu.Lock()
	defer ksw.store.mu.Unlock()
	ksw.store.delete(key)
}

// ResetAll resets all keys.
func (ksw *KeyedSlidingWindow) ResetAll() {
	ksw.store.mu.Lock()
	defer ksw.store.mu.Unlock()
	ksw.store.reset()
}

// Check returns the current state for a key without consuming.
func (ksw *KeyedSlidingWindow) Check(key string) Result {
	return ksw.CheckN(key, 1)
}

// CheckN returns the state for n requests without consuming. It does not
// create an entry for an unknown key.
func (ksw *KeyedSlidingWindow) CheckN(key string, n int) Result {
	return ksw.do(key, n, false)
}

// Take consumes one request for the key and returns the result.
func (ksw *KeyedSlidingWindow) Take(key string) Result {
	return ksw.TakeN(key, 1)
}

// TakeN consumes n requests for the key and returns the result.
func (ksw *KeyedSlidingWindow) TakeN(key string, n int) Result {
	return ksw.do(key, n, true)
}

// entry returns the state for key: stored and marked as used when
// consuming, or a read-only view when only checking.
// Must be called with ksw.store.mu held.
func (ksw *KeyedSlidingWindow) entry(key string, now time.Time, consume bool) *slidingWindowEntry {
	if consume {
		return ksw.store.get(key, now)
	}
	return ksw.store.peek(key, now)
}

func (ksw *KeyedSlidingWindow) do(key string, n int, consume bool) Result {
	if validateN(n, ksw.limit) != nil {
		return Result{Limit: ksw.limit}
	}

	ksw.store.mu.Lock()
	defer ksw.store.mu.Unlock()

	now := time.Now()
	entry := ksw.entry(key, now, consume)
	if windowStart := now.Truncate(ksw.window); windowStart.After(entry.windowStart) {
		if windowStart.Sub(entry.windowStart) >= ksw.window*2 {
			entry.prevCount = 0
		} else {
			entry.prevCount = entry.currCount
		}
		entry.currCount = 0
		entry.windowStart = windowStart
	}

	weight := now.Sub(entry.windowStart).Seconds() / ksw.window.Seconds()
	count := float64(entry.prevCount)*(1-weight) + float64(entry.currCount)
	resetAt := entry.windowStart.Add(ksw.window)

	if count+float64(n) <= float64(ksw.limit) {
		if consume {
			entry.currCount += n
			count += float64(n)
		}
		return Result{Allowed: true, Limit: ksw.limit, Remaining: int(float64(ksw.limit) - count), ResetAt: resetAt}
	}
	return Result{
		Limit:      ksw.limit,
		Remaining:  int(float64(ksw.limit) - count),
		ResetAt:    resetAt,
		RetryAfter: resetAt.Sub(now),
	}
}

// Close stops the cleanup goroutine.
func (ksw *KeyedSlidingWindow) Close() {
	ksw.store.close()
}

// Len returns the number of active keys.
func (ksw *KeyedSlidingWindow) Len() int {
	ksw.store.mu.Lock()
	defer ksw.store.mu.Unlock()
	return ksw.store.len()
}
