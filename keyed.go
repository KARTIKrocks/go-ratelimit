package ratelimit

import (
	"sync"
	"time"
)

// keyedStore holds per-key state for the keyed limiters. Entries are kept in
// least-recently-used order, so evicting when full and removing idle entries
// only touch the oldest entries instead of scanning the whole map.
//
// Callers must hold mu around every method except close.
type keyedStore[S any] struct {
	mu       sync.Mutex
	entries  map[string]*keyedEntry[S]
	head     *keyedEntry[S] // most recently used
	tail     *keyedEntry[S] // least recently used
	maxKeys  int
	newState func(now time.Time) S
	done     chan struct{}
	stopOnce sync.Once
}

type keyedEntry[S any] struct {
	key        string
	state      S
	lastAccess time.Time
	prev, next *keyedEntry[S] // prev is towards head
}

// newKeyedStore creates a store. If cleanupInterval is positive, a goroutine
// removes entries idle for more than twice that interval until close.
func newKeyedStore[S any](cleanupInterval time.Duration, newState func(now time.Time) S) *keyedStore[S] {
	s := &keyedStore[S]{
		entries:  make(map[string]*keyedEntry[S]),
		newState: newState,
		done:     make(chan struct{}),
	}
	if cleanupInterval > 0 {
		go s.cleanup(cleanupInterval)
	}
	return s
}

func (s *keyedStore[S]) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			s.removeIdle(now.Add(-2 * interval))
			s.mu.Unlock()
		}
	}
}

// removeIdle removes entries last accessed before cutoff.
func (s *keyedStore[S]) removeIdle(cutoff time.Time) {
	for s.tail != nil && s.tail.lastAccess.Before(cutoff) {
		s.remove(s.tail)
	}
}

// get returns the state for key, creating it if needed, and marks it as
// most recently used. When the store is full, the least recently used entry
// is evicted to make room.
func (s *keyedStore[S]) get(key string, now time.Time) *S {
	if e, ok := s.entries[key]; ok {
		e.lastAccess = now
		if e != s.head {
			s.unlink(e)
			s.pushFront(e)
		}
		return &e.state
	}

	if s.maxKeys > 0 && len(s.entries) >= s.maxKeys {
		s.remove(s.tail)
	}
	e := &keyedEntry[S]{key: key, state: s.newState(now), lastAccess: now}
	s.entries[key] = e
	s.pushFront(e)
	return &e.state
}

// peek returns the state for key without creating an entry or changing its
// recency. For an unknown key it returns a fresh state that is not stored.
func (s *keyedStore[S]) peek(key string, now time.Time) *S {
	if e, ok := s.entries[key]; ok {
		return &e.state
	}
	state := s.newState(now)
	return &state
}

func (s *keyedStore[S]) pushFront(e *keyedEntry[S]) {
	e.prev, e.next = nil, s.head
	if s.head != nil {
		s.head.prev = e
	}
	s.head = e
	if s.tail == nil {
		s.tail = e
	}
}

func (s *keyedStore[S]) unlink(e *keyedEntry[S]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

func (s *keyedStore[S]) remove(e *keyedEntry[S]) {
	s.unlink(e)
	delete(s.entries, e.key)
}

func (s *keyedStore[S]) delete(key string) {
	if e, ok := s.entries[key]; ok {
		s.remove(e)
	}
}

func (s *keyedStore[S]) reset() {
	s.entries = make(map[string]*keyedEntry[S])
	s.head, s.tail = nil, nil
}

// setMaxKeys sets the key limit, evicting the least recently used entries
// if the store is already over it. Zero means unlimited.
func (s *keyedStore[S]) setMaxKeys(n int) {
	s.maxKeys = n
	for n > 0 && len(s.entries) > n {
		s.remove(s.tail)
	}
}

func (s *keyedStore[S]) len() int {
	return len(s.entries)
}

// close stops the cleanup goroutine. It is safe to call more than once.
func (s *keyedStore[S]) close() {
	s.stopOnce.Do(func() { close(s.done) })
}
