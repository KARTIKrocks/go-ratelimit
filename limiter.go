// Package ratelimit provides rate limiting algorithms and HTTP middleware.
package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Common errors.
var (
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// ErrInvalidN is returned when n is zero or negative.
	ErrInvalidN = errors.New("ratelimit: n must be positive")

	// ErrExceedsLimit is returned when n is larger than the limiter's
	// capacity, so the request can never be allowed.
	ErrExceedsLimit = errors.New("ratelimit: n exceeds limiter capacity")
)

// validateN reports whether n can ever be satisfied by a limiter whose
// capacity (burst, limit or bucket size) is limit.
func validateN(n, limit int) error {
	if n <= 0 {
		return ErrInvalidN
	}
	if n > limit {
		return ErrExceedsLimit
	}
	return nil
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Compile-time interface compliance checks.
var (
	_ Limiter       = (*TokenBucket)(nil)
	_ Limiter       = (*LeakyBucket)(nil)
	_ Limiter       = (*FixedWindow)(nil)
	_ Limiter       = (*SlidingWindow)(nil)
	_ Limiter       = (*SlidingWindowCounter)(nil)
	_ Limiter       = (*Multi)(nil)
	_ ResultLimiter = (*TokenBucket)(nil)
	_ ResultLimiter = (*LeakyBucket)(nil)
	_ ResultLimiter = (*FixedWindow)(nil)
	_ ResultLimiter = (*SlidingWindow)(nil)
	_ ResultLimiter = (*SlidingWindowCounter)(nil)

	_ KeyedLimiter       = (*KeyedTokenBucket)(nil)
	_ KeyedLimiter       = (*KeyedLeakyBucket)(nil)
	_ KeyedLimiter       = (*KeyedFixedWindow)(nil)
	_ KeyedLimiter       = (*KeyedSlidingWindow)(nil)
	_ KeyedResultLimiter = (*KeyedTokenBucket)(nil)
	_ KeyedResultLimiter = (*KeyedLeakyBucket)(nil)
	_ KeyedResultLimiter = (*KeyedFixedWindow)(nil)
	_ KeyedResultLimiter = (*KeyedSlidingWindow)(nil)

	_ Store = (*MemoryStore)(nil)
)

// Limiter is the interface for rate limiters.
type Limiter interface {
	// Allow checks if a request is allowed and consumes a token if so.
	Allow() bool

	// AllowN checks if n requests are allowed and consumes n tokens if so.
	AllowN(n int) bool

	// Wait blocks until a request is allowed or context is cancelled.
	Wait(ctx context.Context) error

	// WaitN blocks until n requests are allowed or context is cancelled.
	WaitN(ctx context.Context, n int) error

	// Reset resets the limiter state.
	Reset()
}

// KeyedLimiter is a rate limiter that supports per-key limiting.
type KeyedLimiter interface {
	// Allow checks if a request for the given key is allowed.
	Allow(key string) bool

	// AllowN checks if n requests for the given key are allowed.
	AllowN(key string, n int) bool

	// Wait blocks until a request for the given key is allowed.
	Wait(ctx context.Context, key string) error

	// WaitN blocks until n requests for the given key are allowed.
	WaitN(ctx context.Context, key string, n int) error

	// Reset resets the limiter for the given key.
	Reset(key string)

	// ResetAll resets all keys.
	ResetAll()
}

// Result contains the result of a rate limit check.
type Result struct {
	Allowed    bool          // Whether the request is allowed
	Limit      int           // Maximum requests allowed
	Remaining  int           // Remaining requests in current window
	RetryAfter time.Duration // Time until next request is allowed (if not allowed)
	ResetAt    time.Time     // When the rate limit resets
}

// ResultLimiter is a limiter that returns detailed results.
type ResultLimiter interface {
	// Check checks if a request is allowed and returns detailed result.
	Check() Result

	// CheckN checks if n requests are allowed and returns detailed result.
	CheckN(n int) Result

	// Take consumes a token and returns the result.
	Take() Result

	// TakeN consumes n tokens and returns the result.
	TakeN(n int) Result
}

// KeyedResultLimiter is a keyed limiter that returns detailed results.
type KeyedResultLimiter interface {
	// Check checks if a request for the key is allowed.
	Check(key string) Result

	// CheckN checks if n requests for the key are allowed.
	CheckN(key string, n int) Result

	// Take consumes a token for the key and returns the result.
	Take(key string) Result

	// TakeN consumes n tokens for the key and returns the result.
	TakeN(key string, n int) Result
}

// Closer is implemented by limiters that hold resources (goroutines, connections)
// that must be released when the limiter is no longer needed.
type Closer interface {
	Close()
}

// Compile-time Closer checks for keyed limiters.
var (
	_ Closer = (*KeyedTokenBucket)(nil)
	_ Closer = (*KeyedLeakyBucket)(nil)
	_ Closer = (*KeyedFixedWindow)(nil)
	_ Closer = (*KeyedSlidingWindow)(nil)
	_ Closer = (*MemoryStore)(nil)
)

// Store is the interface for persistent rate limit storage.
type Store interface {
	// Get retrieves the current count and window start for a key.
	Get(ctx context.Context, key string) (count int64, windowStart time.Time, err error)

	// Increment increments the count for a key.
	Increment(ctx context.Context, key string, window time.Duration) (count int64, err error)

	// Set sets the count for a key.
	Set(ctx context.Context, key string, count int64, expiration time.Duration) error

	// Reset resets the count for a key.
	Reset(ctx context.Context, key string) error
}

// KeyFunc extracts the rate limit key from an HTTP request.
type KeyFunc func(r *http.Request) string

// OnLimitReached is called when a rate limit is exceeded.
type OnLimitReached func(w http.ResponseWriter, r *http.Request, result Result)

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	entries map[string]*storeEntry
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

type storeEntry struct {
	count       int64
	windowStart time.Time
	expiresAt   time.Time
}

// NewMemoryStore creates a new in-memory store.
func NewMemoryStore(cleanupInterval time.Duration) *MemoryStore {
	ctx, cancel := context.WithCancel(context.Background())
	s := &MemoryStore{
		entries: make(map[string]*storeEntry),
		ctx:     ctx,
		cancel:  cancel,
	}

	if cleanupInterval > 0 {
		go s.cleanup(cleanupInterval)
	}

	return s
}

// cleanup periodically removes expired entries.
func (s *MemoryStore) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for key, entry := range s.entries {
				if now.After(entry.expiresAt) {
					delete(s.entries, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Get retrieves the current count and window start for a key.
func (s *MemoryStore) Get(ctx context.Context, key string) (int64, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return 0, time.Time{}, nil
	}

	if time.Now().After(entry.expiresAt) {
		return 0, time.Time{}, nil
	}

	return entry.count, entry.windowStart, nil
}

// Increment increments the count for a key.
func (s *MemoryStore) Increment(ctx context.Context, key string, window time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	entry, ok := s.entries[key]

	if !ok || now.After(entry.expiresAt) {
		s.entries[key] = &storeEntry{
			count:       1,
			windowStart: now,
			expiresAt:   now.Add(window),
		}
		return 1, nil
	}

	entry.count++
	return entry.count, nil
}

// Set sets the count for a key.
func (s *MemoryStore) Set(ctx context.Context, key string, count int64, expiration time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.entries[key] = &storeEntry{
		count:       count,
		windowStart: now,
		expiresAt:   now.Add(expiration),
	}
	return nil
}

// Reset resets the count for a key.
func (s *MemoryStore) Reset(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.entries, key)
	return nil
}

// Close closes the memory store.
func (s *MemoryStore) Close() {
	s.cancel()
}

// Multi combines multiple limiters with AND logic.
// All limiters must allow the request for it to be allowed.
//
// When every limiter implements ResultLimiter (all built-in limiters do),
// Multi checks all of them before consuming from any, so a request denied by
// one limiter does not use up capacity in the others. With a limiter that
// does not implement ResultLimiter, Multi falls back to consuming from each
// limiter in turn and cannot undo earlier consumption.
//
// A mutex serializes AllowN/WaitN across callers of the same Multi. The
// guarantee only holds if the wrapped limiters are not also used directly.
type Multi struct {
	limiters []Limiter
	checkers []ResultLimiter // nil unless every limiter is a ResultLimiter
	mu       sync.Mutex
}

// NewMulti creates a new multi-limiter.
func NewMulti(limiters ...Limiter) *Multi {
	m := &Multi{limiters: limiters}
	checkers := make([]ResultLimiter, 0, len(limiters))
	for _, l := range limiters {
		rl, ok := l.(ResultLimiter)
		if !ok {
			return m
		}
		checkers = append(checkers, rl)
	}
	m.checkers = checkers
	return m
}

// Allow checks if all limiters allow the request.
func (m *Multi) Allow() bool {
	return m.AllowN(1)
}

// AllowN checks if all limiters allow n requests.
func (m *Multi) AllowN(n int) bool {
	if n <= 0 {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.checkers != nil {
		if allowed, _, _ := m.checkAll(n); !allowed {
			return false
		}
	}
	return m.takeAll(n)
}

// checkAll reports whether every limiter would allow n requests without
// consuming anything. When denied, wait is the longest RetryAfter reported.
// Must be called with m.mu held and m.checkers non-nil.
func (m *Multi) checkAll(n int) (allowed bool, wait time.Duration, err error) {
	allowed = true
	for _, c := range m.checkers {
		r := c.CheckN(n)
		if r.Allowed {
			continue
		}
		// Limit is only trusted when set; a custom ResultLimiter may leave it 0.
		if r.Limit > 0 && n > r.Limit {
			return false, 0, ErrExceedsLimit
		}
		allowed = false
		wait = max(wait, r.RetryAfter)
	}
	return allowed, wait, nil
}

// takeAll consumes n from each limiter, stopping at the first denial.
// Must be called with m.mu held.
func (m *Multi) takeAll(n int) bool {
	for _, l := range m.limiters {
		if !l.AllowN(n) {
			return false
		}
	}
	return true
}

// Wait waits for all limiters to allow.
func (m *Multi) Wait(ctx context.Context) error {
	return m.WaitN(ctx, 1)
}

// WaitN waits for all limiters to allow n requests.
func (m *Multi) WaitN(ctx context.Context, n int) error {
	if n <= 0 {
		return ErrInvalidN
	}

	if m.checkers == nil {
		for _, l := range m.limiters {
			if err := l.WaitN(ctx, n); err != nil {
				return err
			}
		}
		return nil
	}

	for {
		m.mu.Lock()
		allowed, wait, err := m.checkAll(n)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		if allowed && m.takeAll(n) {
			m.mu.Unlock()
			return nil
		}
		m.mu.Unlock()

		if wait <= 0 {
			wait = time.Millisecond
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
	}
}

// Reset resets all limiters.
func (m *Multi) Reset() {
	for _, l := range m.limiters {
		l.Reset()
	}
}
