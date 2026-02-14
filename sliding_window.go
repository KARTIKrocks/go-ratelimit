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
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.cleanup(now)

	if len(sw.timestamps)+n <= sw.limit {
		for i := 0; i < n; i++ {
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
	for {
		sw.mu.Lock()
		now := time.Now()
		sw.cleanup(now)

		if len(sw.timestamps)+n <= sw.limit {
			for i := 0; i < n; i++ {
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
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	sw.cleanup(now)

	result := Result{
		Limit:     sw.limit,
		Remaining: sw.limit - len(sw.timestamps),
	}

	if len(sw.timestamps)+n <= sw.limit {
		for i := 0; i < n; i++ {
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
	limit           int
	window          time.Duration
	maxKeys         int
	entries         map[string]*slidingWindowEntry
	mu              sync.RWMutex
	cleanupInterval time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
}

type slidingWindowEntry struct {
	prevCount   int
	currCount   int
	windowStart time.Time
	lastAccess  time.Time
}

// SetMaxKeys sets the maximum number of keys tracked. When the limit is
// reached, requests for new keys are denied. Zero means unlimited.
// Returns the receiver for chaining.
func (ksw *KeyedSlidingWindow) SetMaxKeys(n int) *KeyedSlidingWindow {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()
	ksw.maxKeys = n
	return ksw
}

// NewKeyedSlidingWindow creates a new keyed sliding window limiter.
func NewKeyedSlidingWindow(limit int, window time.Duration, cleanupInterval time.Duration) *KeyedSlidingWindow {
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	ksw := &KeyedSlidingWindow{
		limit:           limit,
		window:          window,
		entries:         make(map[string]*slidingWindowEntry),
		cleanupInterval: cleanupInterval,
		ctx:             ctx,
		cancel:          cancel,
	}

	if cleanupInterval > 0 {
		go ksw.cleanup()
	}

	return ksw
}

// cleanup removes inactive entries.
func (ksw *KeyedSlidingWindow) cleanup() {
	ticker := time.NewTicker(ksw.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ksw.ctx.Done():
			return
		case now := <-ticker.C:
			ksw.mu.Lock()
			cutoff := now.Add(-ksw.cleanupInterval * 2)
			for key, entry := range ksw.entries {
				if entry.lastAccess.Before(cutoff) {
					delete(ksw.entries, key)
				}
			}
			ksw.mu.Unlock()
		}
	}
}

// getOrCreate gets or creates an entry for a key.
// Must be called with ksw.mu held. Returns nil if maxKeys is reached.
func (ksw *KeyedSlidingWindow) getOrCreate(key string) *slidingWindowEntry {
	entry, ok := ksw.entries[key]
	if !ok {
		if ksw.maxKeys > 0 && len(ksw.entries) >= ksw.maxKeys {
			return nil
		}
		entry = &slidingWindowEntry{
			windowStart: time.Now().Truncate(ksw.window),
			lastAccess:  time.Now(),
		}
		ksw.entries[key] = entry
	}
	return entry
}

// update updates the entry's window if needed.
// Must be called with ksw.mu held.
func (ksw *KeyedSlidingWindow) update(entry *slidingWindowEntry, now time.Time) {
	entry.lastAccess = now
	windowStart := now.Truncate(ksw.window)

	if windowStart.After(entry.windowStart) {
		if windowStart.Sub(entry.windowStart) >= ksw.window*2 {
			entry.prevCount = 0
			entry.currCount = 0
		} else {
			entry.prevCount = entry.currCount
			entry.currCount = 0
		}
		entry.windowStart = windowStart
	}
}

// count returns the weighted count for an entry.
// Must be called with ksw.mu held.
func (ksw *KeyedSlidingWindow) count(entry *slidingWindowEntry, now time.Time) float64 {
	elapsed := now.Sub(entry.windowStart)
	weight := elapsed.Seconds() / ksw.window.Seconds()
	return float64(entry.prevCount)*(1-weight) + float64(entry.currCount)
}

// Allow checks if a request for the key is allowed.
func (ksw *KeyedSlidingWindow) Allow(key string) bool {
	return ksw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (ksw *KeyedSlidingWindow) AllowN(key string, n int) bool {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()

	entry := ksw.getOrCreate(key)
	if entry == nil {
		return false
	}
	now := time.Now()
	ksw.update(entry, now)

	if ksw.count(entry, now)+float64(n) <= float64(ksw.limit) {
		entry.currCount += n
		return true
	}
	return false
}

// Wait blocks until a request for the key is allowed.
func (ksw *KeyedSlidingWindow) Wait(ctx context.Context, key string) error {
	return ksw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (ksw *KeyedSlidingWindow) WaitN(ctx context.Context, key string, n int) error {
	for {
		ksw.mu.Lock()
		entry := ksw.getOrCreate(key)
		if entry == nil {
			ksw.mu.Unlock()
			return ErrRateLimitExceeded
		}
		now := time.Now()
		ksw.update(entry, now)

		if ksw.count(entry, now)+float64(n) <= float64(ksw.limit) {
			entry.currCount += n
			ksw.mu.Unlock()
			return nil
		}

		waitTime := entry.windowStart.Add(ksw.window).Sub(now)
		if waitTime < 0 {
			waitTime = time.Millisecond
		}
		ksw.mu.Unlock()

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
func (ksw *KeyedSlidingWindow) Reset(key string) {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()

	delete(ksw.entries, key)
}

// ResetAll resets all keys.
func (ksw *KeyedSlidingWindow) ResetAll() {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()

	ksw.entries = make(map[string]*slidingWindowEntry)
}

// Take consumes a token for the key and returns the result.
func (ksw *KeyedSlidingWindow) Take(key string) Result {
	return ksw.TakeN(key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (ksw *KeyedSlidingWindow) TakeN(key string, n int) Result {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()

	entry := ksw.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: ksw.limit, Remaining: 0}
	}
	now := time.Now()
	ksw.update(entry, now)

	currentCount := ksw.count(entry, now)
	result := Result{
		Limit:     ksw.limit,
		Remaining: int(float64(ksw.limit) - currentCount),
		ResetAt:   entry.windowStart.Add(ksw.window),
	}

	if currentCount+float64(n) <= float64(ksw.limit) {
		entry.currCount += n
		result.Allowed = true
		result.Remaining = int(float64(ksw.limit) - ksw.count(entry, now))
	} else {
		result.Allowed = false
		result.RetryAfter = entry.windowStart.Add(ksw.window).Sub(now)
	}

	return result
}

// Check returns the current state for a key without consuming.
func (ksw *KeyedSlidingWindow) Check(key string) Result {
	return ksw.CheckN(key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (ksw *KeyedSlidingWindow) CheckN(key string, n int) Result {
	ksw.mu.Lock()
	defer ksw.mu.Unlock()

	entry := ksw.getOrCreate(key)
	if entry == nil {
		return Result{Allowed: false, Limit: ksw.limit, Remaining: 0}
	}
	now := time.Now()
	ksw.update(entry, now)

	currentCount := ksw.count(entry, now)
	result := Result{
		Limit:     ksw.limit,
		Remaining: int(float64(ksw.limit) - currentCount),
		ResetAt:   entry.windowStart.Add(ksw.window),
	}

	if currentCount+float64(n) <= float64(ksw.limit) {
		result.Allowed = true
	} else {
		result.Allowed = false
		result.RetryAfter = entry.windowStart.Add(ksw.window).Sub(now)
	}

	return result
}

// Close stops the cleanup goroutine.
func (ksw *KeyedSlidingWindow) Close() {
	ksw.cancel()
}

// Len returns the number of active keys.
func (ksw *KeyedSlidingWindow) Len() int {
	ksw.mu.RLock()
	defer ksw.mu.RUnlock()
	return len(ksw.entries)
}
