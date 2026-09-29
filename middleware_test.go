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
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = testPrivateAddr
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Next request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
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
		for i := range 2 {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("IP %s request %d: expected 200, got %d", ip, i, w.Code)
			}
		}

		// Third request should be limited
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
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
		for i := range 10 {
			req := httptest.NewRequest(http.MethodGet, path, nil)
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
	for i := range 2 {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-API-Key", "key123")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Third request should be limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-API-Key", "key123")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}

	// Different API key should have independent limit
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Next request should return JSON error
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
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
	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, "/api/normal", nil)
		req.RemoteAddr = testPrivateAddr
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Normal path request %d: expected 200, got %d", i, w.Code)
		}
	}

	// Strict path should only allow 1 request
	req := httptest.NewRequest(http.MethodGet, "/api/strict", nil)
	req.RemoteAddr = testPrivateAddr
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Strict path first request: expected 200, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/strict", nil)
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
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
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
			name: "X-Forwarded-For uses the entry appended by the proxy",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 198.51.100.7",
			},
			remoteAddr: testPrivateAddr,
			expected:   "198.51.100.7",
		},
		{
			name: "X-Forwarded-For ignores forged entries on the left",
			headers: map[string]string{
				"X-Forwarded-For": "not-an-ip, 6.6.6.6, 198.51.100.7",
			},
			remoteAddr: testPrivateAddr,
			expected:   "198.51.100.7",
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
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = testPrivateAddr

	for b.Loop() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

func TestTrustedProxiesKeyFunc(t *testing.T) {
	keyFunc := TrustedProxiesKeyFunc("10.0.0.0/8", "192.168.1.1", "::ffff:172.16.0.1")

	tests := []struct {
		name       string
		xff        []string
		remoteAddr string
		expected   string
	}{
		{
			name:       "ignores headers from untrusted peer",
			xff:        []string{"203.0.113.1"},
			remoteAddr: "198.51.100.7:1234",
			expected:   "198.51.100.7",
		},
		{
			name:       "IPv4-mapped trusted proxy address",
			xff:        []string{"203.0.113.1"},
			remoteAddr: "172.16.0.1:1234",
			expected:   "203.0.113.1",
		},
		{
			name:       "no header from trusted peer",
			remoteAddr: "10.1.2.3:1234",
			expected:   "10.1.2.3",
		},
		{
			name:       "skips trusted hops right to left",
			xff:        []string{"6.6.6.6, 203.0.113.1, 10.9.9.9"},
			remoteAddr: "192.168.1.1:1234",
			expected:   "203.0.113.1",
		},
		{
			name:       "joins repeated headers in order",
			xff:        []string{"6.6.6.6", "203.0.113.1", "10.9.9.9"},
			remoteAddr: "10.1.2.3:1234",
			expected:   "203.0.113.1",
		},
		{
			name:       "stops at malformed entry",
			xff:        []string{"garbage, 10.9.9.9"},
			remoteAddr: "10.1.2.3:1234",
			expected:   "10.9.9.9",
		},
		{
			name:       "all hops trusted",
			xff:        []string{"10.8.8.8, 10.9.9.9"},
			remoteAddr: "10.1.2.3:1234",
			expected:   "10.8.8.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := keyFunc(req); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestTrustedProxiesKeyFunc_PanicsOnInvalidCIDR(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	TrustedProxiesKeyFunc("not-a-cidr")
}

func TestIPKeyFunc_IPv6UsesSlash64(t *testing.T) {
	tests := []struct {
		remoteAddr string
		expected   string
	}{
		{"[2001:db8:1:2:aaaa::1]:1234", "2001:db8:1:2::/64"},
		{"[2001:db8:1:2:ffff:ffff:ffff:ffff]:1234", "2001:db8:1:2::/64"},
		{"[fe80::1%eth0]:1234", "fe80::/64"},
		{"[::ffff:192.0.2.1]:1234", "192.0.2.1"},
		{"192.0.2.1:1234", "192.0.2.1"},
		{"not-an-ip", "not-an-ip"},
	}
	for _, tt := range tests {
		t.Run(tt.remoteAddr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.RemoteAddr = tt.remoteAddr
			if got := IPKeyFunc(req); got != tt.expected {
				t.Errorf("IPKeyFunc = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIPKeyFuncs_ShareIPv6Masking(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.RemoteAddr = "[2001:db8::1]:1234"
	const want = "2001:db8::/64"

	if got := IPPathKeyFunc(req); got != want+":/api" {
		t.Errorf("IPPathKeyFunc = %q", got)
	}
	if got := UserIDKeyFunc("uid")(req); got != want {
		t.Errorf("UserIDKeyFunc fallback = %q", got)
	}
	if got := TrustedProxyKeyFunc(req); got != want {
		t.Errorf("TrustedProxyKeyFunc = %q", got)
	}

	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "2001:db8::2")
	if got := TrustedProxiesKeyFunc("10.0.0.0/8")(req); got != want {
		t.Errorf("TrustedProxiesKeyFunc = %q", got)
	}
}

func TestHeaderKeyFunc_FallsBackToIP(t *testing.T) {
	fn := HeaderKeyFunc("X-API-Key")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	if got := fn(req); got != "ip:192.0.2.1" {
		t.Errorf("without header = %q, want ip:192.0.2.1", got)
	}

	// A header value can't impersonate another client's fallback key.
	req.Header.Set("X-API-Key", "ip:192.0.2.1")
	if got := fn(req); got != "header:ip:192.0.2.1" {
		t.Errorf("with header = %q, want header:ip:192.0.2.1", got)
	}
}

func TestHeaderKeyFunc_MissingHeaderDoesNotShareBucket(t *testing.T) {
	limiter := NewKeyedFixedWindow(1, time.Hour, 0)
	defer limiter.Close()
	handler := Middleware(limiter, WithKeyFunc(HeaderKeyFunc("X-API-Key")))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, addr := range []string{"192.0.2.1:1", "192.0.2.2:1"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("client %s without header got %d; clients share a bucket", addr, rec.Code)
		}
	}
}
