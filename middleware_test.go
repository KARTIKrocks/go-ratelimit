package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMiddleware_Basic(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// First 5 requests should succeed
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = testPrivateAddr
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Next request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}
}

func TestMiddleware_DifferentIPs(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 2, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Each IP should have independent limit
	ips := []string{testPrivateAddr, "192.168.1.2:1234", "192.168.1.3:1234"}

	for _, ip := range ips {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("IP %s request %d: expected 200, got %d", ip, i, w.Code)
			}
		}

		// Third request should be limited
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("IP %s: expected 429, got %d", ip, w.Code)
		}
	}
}

func TestMiddleware_Headers(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Check rate limit headers
	if w.Header().Get("X-RateLimit-Limit") != "5" {
		t.Errorf("Expected X-RateLimit-Limit: 5, got %s", w.Header().Get("X-RateLimit-Limit"))
	}

	if w.Header().Get("X-RateLimit-Remaining") != "4" {
		t.Errorf("Expected X-RateLimit-Remaining: 4, got %s", w.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestMiddleware_SkipHealthChecks(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter,
		WithSkipFunc(SkipHealthChecks),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	healthPaths := []string{"/health", "/healthz", "/ready", "/readyz", "/live", "/livez", "/ping"}

	for _, path := range healthPaths {
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", path, nil)
			req.RemoteAddr = testPrivateAddr
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Path %s request %d: expected 200, got %d", path, i, w.Code)
			}
		}
	}
}

func TestMiddleware_CustomKeyFunc(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 2, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter,
		WithKeyFunc(HeaderKeyFunc("X-API-Key")),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Same API key should share limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "key123")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Third request should be limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}

	// Different API key should have independent limit
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", "key456")
	w = httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Different key: expected 200, got %d", w.Code)
	}
}

func TestMiddleware_JSONOnLimitReached(t *testing.T) {
	limiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter,
		WithOnLimitReached(JSONOnLimitReached),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust limit
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Next request should return JSON error
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testPrivateAddr
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected JSON content type, got %s", w.Header().Get("Content-Type"))
	}

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}
}

func TestPathLimiter(t *testing.T) {
	normalLimiter := NewKeyedTokenBucket(10.0, 5, time.Minute)
	defer normalLimiter.Close()

	strictLimiter := NewKeyedTokenBucket(10.0, 1, time.Minute)
	defer strictLimiter.Close()

	pathLimiter := NewPathLimiter(normalLimiter)
	pathLimiter.Add("/api/strict", strictLimiter)

	handler := pathLimiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Normal path should allow 5 requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/normal", nil)
		req.RemoteAddr = testPrivateAddr
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Normal path request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Strict path should only allow 1 request
	req := httptest.NewRequest("GET", "/api/strict", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Strict path first request: expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/strict", nil)
	req.RemoteAddr = testPrivateAddr
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Strict path second request: expected 429, got %d", w.Code)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "RemoteAddr with port",
			remoteAddr: testPrivateAddr,
			expected:   "192.168.1.1",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name: "ignores X-Forwarded-For by default",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 192.168.1.1",
			},
			remoteAddr: "10.0.0.1:1234",
			expected:   "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetClientIP(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

func TestGetClientIPFromHeaders(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "falls back to RemoteAddr",
			remoteAddr: testPrivateAddr,
			expected:   "192.168.1.1",
		},
		{
			name: "X-Forwarded-For",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 192.168.1.1",
			},
			remoteAddr: testPrivateAddr,
			expected:   "203.0.113.1",
		},
		{
			name: "X-Real-IP",
			headers: map[string]string{
				"X-Real-IP": "203.0.113.2",
			},
			remoteAddr: testPrivateAddr,
			expected:   "203.0.113.2",
		},
		{
			name: "CF-Connecting-IP",
			headers: map[string]string{
				"CF-Connecting-IP": "203.0.113.3",
			},
			remoteAddr: testPrivateAddr,
			expected:   "203.0.113.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			ip := GetClientIPFromHeaders(req)
			if ip != tt.expected {
				t.Errorf("Expected IP %s, got %s", tt.expected, ip)
			}
		})
	}
}

func BenchmarkMiddleware(b *testing.B) {
	limiter := NewKeyedTokenBucket(1000000.0, 1000000, time.Minute)
	defer limiter.Close()

	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = testPrivateAddr

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}
