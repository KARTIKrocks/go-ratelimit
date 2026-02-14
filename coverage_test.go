package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testRemoteAddr  = "1.2.3.4:1234"
	testPrivateAddr = "192.168.1.1:1234"
)

// --- TokenBucket accessor tests ---

func TestTokenBucket_Accessors(t *testing.T) {
	tb := NewTokenBucket(10.0, 20)
	if tb.Rate() != 10.0 {
		t.Errorf("expected rate 10.0, got %f", tb.Rate())
	}
	if tb.Burst() != 20 {
		t.Errorf("expected burst 20, got %d", tb.Burst())
	}
	tokens := tb.Tokens()
	if tokens != 20.0 {
		t.Errorf("expected 20 tokens, got %f", tokens)
	}
}

func TestTokenBucket_CheckN_Denied(t *testing.T) {
	tb := NewTokenBucket(1.0, 1)
	tb.Allow() // exhaust

	result := tb.CheckN(1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestTokenBucket_TakeN_Denied(t *testing.T) {
	tb := NewTokenBucket(1.0, 1)
	tb.Allow() // exhaust

	result := tb.TakeN(1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestTokenBucketPerDuration(t *testing.T) {
	tb := NewTokenBucketPerDuration(100, time.Second, 10)
	if !tb.Allow() {
		t.Error("expected allowed")
	}
}

// --- KeyedTokenBucket extended tests ---

func TestKeyedTokenBucket_Wait(t *testing.T) {
	limiter := NewKeyedTokenBucket(100.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1") // exhaust

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx, "key1"); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestKeyedTokenBucket_WaitTimeout(t *testing.T) {
	limiter := NewKeyedTokenBucket(1.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx, "key1"); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestKeyedTokenBucket_Reset(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	for i := 0; i < 5; i++ {
		limiter.Allow("key1")
	}
	if limiter.Allow("key1") {
		t.Error("should be denied")
	}

	limiter.Reset("key1")
	if !limiter.Allow("key1") {
		t.Error("should be allowed after reset")
	}
}

func TestKeyedTokenBucket_Check(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 10, time.Minute)
	defer limiter.Close()

	result1 := limiter.Check("key1")
	result2 := limiter.Check("key1")
	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume tokens")
	}
	if result1.Remaining != result2.Remaining {
		t.Error("Remaining should be the same")
	}
}

func TestKeyedTokenBucket_CheckN_Denied(t *testing.T) {
	limiter := NewKeyedTokenBucket(1.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.CheckN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestKeyedTokenBucketPerDuration(t *testing.T) {
	limiter := NewKeyedTokenBucketPerDuration(100, time.Second, 10, time.Minute)
	defer limiter.Close()

	if !limiter.Allow("key") {
		t.Error("expected allowed")
	}
}

// --- LeakyBucket accessor tests ---

func TestLeakyBucket_Accessors(t *testing.T) {
	lb := NewLeakyBucket(10.0, 20)
	if lb.Rate() != 10.0 {
		t.Errorf("expected rate 10.0, got %f", lb.Rate())
	}
	if lb.Capacity() != 20 {
		t.Errorf("expected capacity 20, got %d", lb.Capacity())
	}
	if lb.Water() != 0 {
		t.Errorf("expected 0 water, got %f", lb.Water())
	}
}

func TestLeakyBucket_CheckN_Denied(t *testing.T) {
	lb := NewLeakyBucket(1.0, 1)
	lb.Allow() // fill

	result := lb.CheckN(1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestLeakyBucketPerDuration(t *testing.T) {
	lb := NewLeakyBucketPerDuration(100, time.Second, 10)
	if !lb.Allow() {
		t.Error("expected allowed")
	}
}

// --- KeyedLeakyBucket extended tests ---

func TestKeyedLeakyBucket_Wait(t *testing.T) {
	limiter := NewKeyedLeakyBucket(100.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1") // fill

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx, "key1"); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestKeyedLeakyBucket_WaitTimeout(t *testing.T) {
	limiter := NewKeyedLeakyBucket(1.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx, "key1"); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestKeyedLeakyBucket_Reset(t *testing.T) {
	limiter := NewKeyedLeakyBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	for i := 0; i < 5; i++ {
		limiter.Allow("key1")
	}
	if limiter.Allow("key1") {
		t.Error("should be denied")
	}

	limiter.Reset("key1")
	if !limiter.Allow("key1") {
		t.Error("should be allowed after reset")
	}
}

func TestKeyedLeakyBucket_ResetAll(t *testing.T) {
	limiter := NewKeyedLeakyBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	limiter.Allow("key2")
	limiter.ResetAll()

	if limiter.Len() != 0 {
		t.Errorf("expected 0 keys, got %d", limiter.Len())
	}
}

func TestKeyedLeakyBucket_Take(t *testing.T) {
	limiter := NewKeyedLeakyBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	result := limiter.Take("key1")
	if !result.Allowed {
		t.Error("expected allowed")
	}
	if result.Remaining != 4 {
		t.Errorf("expected 4 remaining, got %d", result.Remaining)
	}
}

func TestKeyedLeakyBucket_TakeN_Denied(t *testing.T) {
	limiter := NewKeyedLeakyBucket(1.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.TakeN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestKeyedLeakyBucket_Check(t *testing.T) {
	limiter := NewKeyedLeakyBucket(10.0, 10, time.Minute)
	defer limiter.Close()

	result1 := limiter.Check("key1")
	result2 := limiter.Check("key1")
	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume capacity")
	}
}

func TestKeyedLeakyBucket_CheckN_Denied(t *testing.T) {
	limiter := NewKeyedLeakyBucket(1.0, 1, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.CheckN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestKeyedLeakyBucketPerDuration(t *testing.T) {
	limiter := NewKeyedLeakyBucketPerDuration(100, time.Second, 10, time.Minute)
	defer limiter.Close()

	if !limiter.Allow("key") {
		t.Error("expected allowed")
	}
}

// --- FixedWindow extended tests ---

func TestFixedWindow_Wait(t *testing.T) {
	limiter := NewFixedWindow(1, 100*time.Millisecond)
	limiter.Allow()

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestFixedWindow_WaitTimeout(t *testing.T) {
	limiter := NewFixedWindow(1, time.Hour)
	limiter.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestFixedWindow_Count(t *testing.T) {
	limiter := NewFixedWindow(10, time.Minute)
	limiter.Allow()
	limiter.Allow()
	limiter.Allow()

	if count := limiter.Count(); count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

// --- KeyedFixedWindow extended tests ---

func TestKeyedFixedWindow_Wait(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, 100*time.Millisecond, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx, "key1"); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestKeyedFixedWindow_WaitTimeout(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, time.Hour, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx, "key1"); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestKeyedFixedWindow_Reset(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	if limiter.Allow("key1") {
		t.Error("should be denied")
	}

	limiter.Reset("key1")
	if !limiter.Allow("key1") {
		t.Error("should be allowed after reset")
	}
}

func TestKeyedFixedWindow_ResetAll(t *testing.T) {
	limiter := NewKeyedFixedWindow(10, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	limiter.Allow("key2")
	limiter.ResetAll()

	if limiter.Len() != 0 {
		t.Errorf("expected 0 keys, got %d", limiter.Len())
	}
}

func TestKeyedFixedWindow_Take(t *testing.T) {
	limiter := NewKeyedFixedWindow(5, time.Minute, time.Minute)
	defer limiter.Close()

	result := limiter.Take("key1")
	if !result.Allowed {
		t.Error("expected allowed")
	}
	if result.Remaining != 4 {
		t.Errorf("expected 4 remaining, got %d", result.Remaining)
	}
	if result.ResetAt.IsZero() {
		t.Error("expected non-zero ResetAt")
	}
}

func TestKeyedFixedWindow_TakeN_Denied(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.TakeN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestKeyedFixedWindow_Check(t *testing.T) {
	limiter := NewKeyedFixedWindow(10, time.Minute, time.Minute)
	defer limiter.Close()

	result1 := limiter.Check("key1")
	result2 := limiter.Check("key1")
	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume capacity")
	}
}

func TestKeyedFixedWindow_CheckN_Denied(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.CheckN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
}

// --- SlidingWindow extended tests ---

func TestSlidingWindow_Wait(t *testing.T) {
	limiter := NewSlidingWindow(1, 100*time.Millisecond)
	limiter.Allow()

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestSlidingWindow_WaitTimeout(t *testing.T) {
	limiter := NewSlidingWindow(1, time.Hour)
	limiter.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestSlidingWindow_Count(t *testing.T) {
	limiter := NewSlidingWindow(10, time.Minute)
	limiter.Allow()
	limiter.Allow()

	if count := limiter.Count(); count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

// --- SlidingWindowCounter extended tests ---

func TestSlidingWindowCounter_Wait(t *testing.T) {
	limiter := NewSlidingWindowCounter(1, 100*time.Millisecond)
	limiter.Allow()

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestSlidingWindowCounter_WaitTimeout(t *testing.T) {
	limiter := NewSlidingWindowCounter(1, time.Hour)
	limiter.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

// --- KeyedSlidingWindow extended tests ---

func TestKeyedSlidingWindow_Wait(t *testing.T) {
	limiter := NewKeyedSlidingWindow(1, 100*time.Millisecond, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx := context.Background()
	start := time.Now()
	if err := limiter.Wait(ctx, "key1"); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestKeyedSlidingWindow_WaitTimeout(t *testing.T) {
	limiter := NewKeyedSlidingWindow(1, time.Hour, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := limiter.Wait(ctx, "key1"); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestKeyedSlidingWindow_Reset(t *testing.T) {
	limiter := NewKeyedSlidingWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	if limiter.Allow("key1") {
		t.Error("should be denied")
	}

	limiter.Reset("key1")
	if !limiter.Allow("key1") {
		t.Error("should be allowed after reset")
	}
}

func TestKeyedSlidingWindow_ResetAll(t *testing.T) {
	limiter := NewKeyedSlidingWindow(10, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	limiter.Allow("key2")
	limiter.ResetAll()

	if limiter.Len() != 0 {
		t.Errorf("expected 0 keys, got %d", limiter.Len())
	}
}

func TestKeyedSlidingWindow_Take(t *testing.T) {
	limiter := NewKeyedSlidingWindow(5, time.Minute, time.Minute)
	defer limiter.Close()

	result := limiter.Take("key1")
	if !result.Allowed {
		t.Error("expected allowed")
	}
	if result.Remaining != 4 {
		t.Errorf("expected 4 remaining, got %d", result.Remaining)
	}
}

func TestKeyedSlidingWindow_TakeN_Denied(t *testing.T) {
	limiter := NewKeyedSlidingWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.TakeN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
	if result.RetryAfter <= 0 {
		t.Error("expected positive RetryAfter")
	}
}

func TestKeyedSlidingWindow_Check(t *testing.T) {
	limiter := NewKeyedSlidingWindow(10, time.Minute, time.Minute)
	defer limiter.Close()

	result1 := limiter.Check("key1")
	result2 := limiter.Check("key1")
	if !result1.Allowed || !result2.Allowed {
		t.Error("Check should not consume capacity")
	}
}

func TestKeyedSlidingWindow_CheckN_Denied(t *testing.T) {
	limiter := NewKeyedSlidingWindow(1, time.Minute, time.Minute)
	defer limiter.Close()

	limiter.Allow("key1")
	result := limiter.CheckN("key1", 1)
	if result.Allowed {
		t.Error("expected denied")
	}
}

// --- Multi extended tests ---

func TestMulti_Wait(t *testing.T) {
	l1 := NewTokenBucket(100.0, 1)
	l2 := NewTokenBucket(100.0, 1)

	multi := NewMulti(l1, l2)
	multi.Allow() // exhaust both

	ctx := context.Background()
	start := time.Now()
	if err := multi.Wait(ctx); err != nil {
		t.Errorf("Wait failed: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Wait should have blocked")
	}
}

func TestMulti_WaitTimeout(t *testing.T) {
	l1 := NewTokenBucket(1.0, 1)

	multi := NewMulti(l1)
	multi.Allow()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := multi.Wait(ctx); err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestMulti_CheckBeforeTake(t *testing.T) {
	// Test that Multi uses Check+Take when all limiters implement ResultLimiter
	l1 := NewTokenBucket(10.0, 5)
	l2 := NewTokenBucket(10.0, 1)

	multi := NewMulti(l1, l2)

	// First call should pass (both have tokens)
	if !multi.Allow() {
		t.Error("first call should be allowed")
	}

	// l2 is now exhausted. l1 should NOT have lost a token unnecessarily
	// because Multi checks all first.
	// l1 started with 5, took 1, so should have ~4 remaining
	result := l1.Check()
	if result.Remaining < 3 {
		t.Errorf("l1 should have ~4 remaining, got %d", result.Remaining)
	}
}

// --- Middleware extended tests ---

func TestMiddlewareFunc_Basic(t *testing.T) {
	limiter := NewTokenBucket(10.0, 2)

	handler := MiddlewareFunc(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 2 should pass
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Third should fail
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

func TestMiddlewareFunc_SkipFunc(t *testing.T) {
	limiter := NewTokenBucket(10.0, 1)

	handler := MiddlewareFunc(limiter,
		WithSkipFunc(SkipHealthChecks),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Health check should be skipped even after exhaustion
	limiter.Allow() // exhaust
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health check should bypass limiter, got %d", w.Code)
	}
}

func TestMiddleware_WithStatusCode(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter,
		WithStatusCode(http.StatusServiceUnavailable),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Next should get custom status code
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestMiddleware_WithHeaders_Disabled(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter,
		WithHeaders(false),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-RateLimit-Limit") != "" {
		t.Error("headers should be disabled")
	}
}

func TestDefaultOnLimitReached(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	DefaultOnLimitReached(w, req, Result{Allowed: false})

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

// --- Key function tests ---

func TestPathKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	key := PathKeyFunc(req)
	if key != "/api/v1/users" {
		t.Errorf("expected /api/v1/users, got %s", key)
	}
}

func TestMethodPathKeyFunc(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/users", nil)
	key := MethodPathKeyFunc(req)
	if key != "POST:/api/v1/users" {
		t.Errorf("expected POST:/api/v1/users, got %s", key)
	}
}

func TestIPPathKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	req.RemoteAddr = testPrivateAddr
	key := IPPathKeyFunc(req)
	if key != "192.168.1.1:/api/v1/users" {
		t.Errorf("expected 192.168.1.1:/api/v1/users, got %s", key)
	}
}

func TestCompositeKeyFunc(t *testing.T) {
	fn := CompositeKeyFunc(IPKeyFunc, PathKeyFunc)
	req := httptest.NewRequest("GET", "/api", nil)
	req.RemoteAddr = "10.0.0.1:5000"
	key := fn(req)
	if key != "10.0.0.1:/api" {
		t.Errorf("expected 10.0.0.1:/api, got %s", key)
	}
}

func TestUserIDKeyFunc(t *testing.T) {
	type ctxKey string
	fn := UserIDKeyFunc(ctxKey("uid"))

	// With user ID in context
	req := httptest.NewRequest("GET", "/api", nil)
	ctx := context.WithValue(req.Context(), ctxKey("uid"), "user42")
	req = req.WithContext(ctx)
	key := fn(req)
	if key != "user42" {
		t.Errorf("expected user42, got %s", key)
	}

	// Without user ID, falls back to IP
	req2 := httptest.NewRequest("GET", "/api", nil)
	req2.RemoteAddr = "10.0.0.1:5000"
	key2 := fn(req2)
	if key2 != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", key2)
	}
}

// --- Skip function tests ---

func TestSkipPrivateIPs(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1:1234", true},
		{"10.0.0.1:1234", true},
		{testPrivateAddr, true},
		{"8.8.8.8:1234", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = tt.ip
		if got := SkipPrivateIPs(req); got != tt.expected {
			t.Errorf("SkipPrivateIPs(%s) = %v, want %v", tt.ip, got, tt.expected)
		}
	}
}

func TestSkipMethods(t *testing.T) {
	skip := SkipMethods("OPTIONS", "HEAD")

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	if !skip(req) {
		t.Error("should skip OPTIONS")
	}

	req = httptest.NewRequest("GET", "/test", nil)
	if skip(req) {
		t.Error("should not skip GET")
	}
}

func TestSkipPaths(t *testing.T) {
	skip := SkipPaths("/metrics", "/debug")

	req := httptest.NewRequest("GET", "/metrics", nil)
	if !skip(req) {
		t.Error("should skip /metrics")
	}

	req = httptest.NewRequest("GET", "/api", nil)
	if skip(req) {
		t.Error("should not skip /api")
	}
}

func TestSkipIf(t *testing.T) {
	skip := SkipIf(SkipHealthChecks, SkipPrivateIPs)

	// Health check from public IP
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	if !skip(req) {
		t.Error("should skip health check")
	}

	// Non-health from private IP
	req = httptest.NewRequest("GET", "/api", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	if !skip(req) {
		t.Error("should skip private IP")
	}

	// Non-health from public IP
	req = httptest.NewRequest("GET", "/api", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	if skip(req) {
		t.Error("should not skip")
	}
}

// --- Handler adapter tests ---

func TestHandler(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer limiter.Close()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := Handler(inner, limiter)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandlerFunc(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer limiter.Close()

	fn := HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}, limiter)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	fn(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- WaitMiddleware test ---

func TestWaitMiddleware(t *testing.T) {
	limiter := NewKeyedTokenBucket(100.0, 1, time.Minute)
	defer limiter.Close()

	handler := WaitMiddleware(limiter, 200*time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request passes
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Second request waits and succeeds (high rate)
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 after wait, got %d", w.Code)
	}
}

func TestWaitMiddleware_Timeout(t *testing.T) {
	limiter := NewKeyedTokenBucket(0.1, 1, time.Minute) // very slow rate
	defer limiter.Close()

	handler := WaitMiddleware(limiter, 10*time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Second should timeout
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testRemoteAddr
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on timeout, got %d", w.Code)
	}
}

// --- Benchmarks for all algorithms ---

func BenchmarkLeakyBucket_Allow(b *testing.B) {
	limiter := NewLeakyBucket(1000000.0, 1000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkLeakyBucket_AllowParallel(b *testing.B) {
	limiter := NewLeakyBucket(1000000.0, 1000000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkFixedWindow_Allow(b *testing.B) {
	limiter := NewFixedWindow(1000000, time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkFixedWindow_AllowParallel(b *testing.B) {
	limiter := NewFixedWindow(1000000, time.Hour)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkSlidingWindow_Allow(b *testing.B) {
	limiter := NewSlidingWindow(1000000, time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkSlidingWindowCounter_Allow(b *testing.B) {
	limiter := NewSlidingWindowCounter(1000000, time.Hour)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

func BenchmarkSlidingWindowCounter_AllowParallel(b *testing.B) {
	limiter := NewSlidingWindowCounter(1000000, time.Hour)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Allow()
		}
	})
}

func BenchmarkKeyedFixedWindow_Allow(b *testing.B) {
	limiter := NewKeyedFixedWindow(1000000, time.Hour, time.Minute)
	defer limiter.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("key")
	}
}

func BenchmarkKeyedSlidingWindow_Allow(b *testing.B) {
	limiter := NewKeyedSlidingWindow(1000000, time.Hour, time.Minute)
	defer limiter.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("key")
	}
}

func BenchmarkKeyedLeakyBucket_Allow(b *testing.B) {
	limiter := NewKeyedLeakyBucket(1000000.0, 1000000, time.Minute)
	defer limiter.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("key")
	}
}
