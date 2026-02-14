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
)

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
// A mutex serializes AllowN/WaitN to prevent TOCTOU races when checking
// multiple limiters.
type Multi struct {
	limiters []Limiter
	mu       sync.Mutex
}

// NewMulti creates a new multi-limiter.
func NewMulti(limiters ...Limiter) *Multi {
	return &Multi{limiters: limiters}
}

// Allow checks if all limiters allow the request.
func (m *Multi) Allow() bool {
	return m.AllowN(1)
}

// AllowN checks if all limiters allow n requests.
// Uses sequential consumption with rollback-safe ordering. The mutex
// ensures no concurrent caller can observe an inconsistent state between
// the individual limiter checks.
func (m *Multi) AllowN(n int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

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
	for _, l := range m.limiters {
		if err := l.WaitN(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

// Reset resets all limiters.
func (m *Multi) Reset() {
	for _, l := range m.limiters {
		l.Reset()
	}
}
