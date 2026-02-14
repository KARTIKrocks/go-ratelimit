package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryStore_GetEmpty(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()
	count, windowStart, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
	if !windowStart.IsZero() {
		t.Errorf("expected zero time, got %v", windowStart)
	}
}

func TestMemoryStore_SetAndGet(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()
	err := store.Set(ctx, "key1", 42, time.Minute)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	count, windowStart, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if count != 42 {
		t.Errorf("expected count 42, got %d", count)
	}
	if windowStart.IsZero() {
		t.Errorf("expected non-zero window start")
	}
}

func TestMemoryStore_Increment(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()

	// First increment creates entry with count=1
	count, err := store.Increment(ctx, "key1", time.Minute)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Second increment increases count
	count, err = store.Increment(ctx, "key1", time.Minute)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// Third increment
	count, err = store.Increment(ctx, "key1", time.Minute)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestMemoryStore_IncrementNewWindow(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()

	// Create entry with short window
	count, err := store.Increment(ctx, "key1", 50*time.Millisecond)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	count, err = store.Increment(ctx, "key1", 50*time.Millisecond)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Increment after expiration should create new entry with count=1
	count, err = store.Increment(ctx, "key1", 50*time.Millisecond)
	if err != nil {
		t.Errorf("Increment failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 after expiration, got %d", count)
	}
}

func TestMemoryStore_Reset(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()

	err := store.Set(ctx, "key1", 10, time.Minute)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	err = store.Reset(ctx, "key1")
	if err != nil {
		t.Errorf("Reset failed: %v", err)
	}

	count, windowStart, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0 after reset, got %d", count)
	}
	if !windowStart.IsZero() {
		t.Errorf("expected zero time after reset, got %v", windowStart)
	}
}

func TestMemoryStore_Expiration(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()

	err := store.Set(ctx, "key1", 5, 50*time.Millisecond)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	// Should be accessible immediately
	count, _, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected count 5, got %d", count)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should return 0 after expiration
	count, windowStart, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0 after expiration, got %d", count)
	}
	if !windowStart.IsZero() {
		t.Errorf("expected zero time after expiration, got %v", windowStart)
	}
}

func TestMemoryStore_Cleanup(t *testing.T) {
	store := NewMemoryStore(50 * time.Millisecond)
	defer store.Close()

	ctx := context.Background()

	err := store.Set(ctx, "key1", 1, 50*time.Millisecond)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}
	err = store.Set(ctx, "key2", 2, 50*time.Millisecond)
	if err != nil {
		t.Errorf("Set failed: %v", err)
	}

	// Entries should exist initially
	count, _, _ := store.Get(ctx, "key1")
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// Wait for cleanup to run
	time.Sleep(200 * time.Millisecond)

	// Entries should be cleaned up
	store.mu.RLock()
	remaining := len(store.entries)
	store.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", remaining)
	}
}

func TestMemoryStore_Concurrent(t *testing.T) {
	store := NewMemoryStore(time.Minute)
	defer store.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Run concurrent increments on the same key
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Increment(ctx, "key1", time.Minute)
			if err != nil {
				t.Errorf("Increment failed: %v", err)
			}
		}()
	}

	wg.Wait()

	count, _, err := store.Get(ctx, "key1")
	if err != nil {
		t.Errorf("Get failed: %v", err)
	}
	if count != 100 {
		t.Errorf("expected count 100, got %d", count)
	}
}
