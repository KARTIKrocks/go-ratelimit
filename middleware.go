package ratelimit

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Middleware creates HTTP middleware from a keyed limiter.
func Middleware(limiter KeyedResultLimiter, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		keyFunc:        IPKeyFunc,
		onLimitReached: nil,
		statusCode:     http.StatusTooManyRequests,
		addHeaders:     true,
		skipFunc:       nil,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.onLimitReached == nil {
		cfg.onLimitReached = cfg.defaultOnLimitReached
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skipFunc != nil && cfg.skipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			key := cfg.keyFunc(r)
			result := limiter.Take(key)

			if cfg.addHeaders {
				AddRateLimitHeaders(w, result)
			}

			if !result.Allowed {
				cfg.onLimitReached(w, r, result)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MiddlewareFunc creates middleware from a simple limiter (non-keyed).
func MiddlewareFunc(limiter ResultLimiter, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		onLimitReached: nil,
		statusCode:     http.StatusTooManyRequests,
		addHeaders:     true,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.onLimitReached == nil {
		cfg.onLimitReached = cfg.defaultOnLimitReached
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skipFunc != nil && cfg.skipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			result := limiter.Take()

			if cfg.addHeaders {
				AddRateLimitHeaders(w, result)
			}

			if !result.Allowed {
				cfg.onLimitReached(w, r, result)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

type middlewareConfig struct {
	keyFunc        KeyFunc
	onLimitReached OnLimitReached
	statusCode     int
	addHeaders     bool
	skipFunc       func(*http.Request) bool
}

// defaultOnLimitReached returns a plain-text error using the configured status code.
func (cfg *middlewareConfig) defaultOnLimitReached(w http.ResponseWriter, r *http.Request, result Result) {
	http.Error(w, "Rate limit exceeded", cfg.statusCode)
}

// MiddlewareOption is an option for the middleware.
type MiddlewareOption func(*middlewareConfig)

// WithKeyFunc sets the key extraction function.
func WithKeyFunc(fn KeyFunc) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.keyFunc = fn
	}
}

// WithOnLimitReached sets the handler for when limit is reached.
func WithOnLimitReached(fn OnLimitReached) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.onLimitReached = fn
	}
}

// WithStatusCode sets the status code returned when limit is reached.
func WithStatusCode(code int) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.statusCode = code
	}
}

// WithHeaders enables or disables rate limit headers.
func WithHeaders(enabled bool) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.addHeaders = enabled
	}
}

// WithSkipFunc sets a function to determine if rate limiting should be skipped.
func WithSkipFunc(fn func(*http.Request) bool) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.skipFunc = fn
	}
}

// AddRateLimitHeaders adds standard rate limit headers to the response.
func AddRateLimitHeaders(w http.ResponseWriter, result Result) {
	h := w.Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))

	if !result.ResetAt.IsZero() {
		h.Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))
	}

	if !result.Allowed && result.RetryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(int(math.Ceil(result.RetryAfter.Seconds()))))
	}
}

// DefaultOnLimitReached is the default handler for rate limit exceeded.
// When used with WithOnLimitReached, it always uses 429. To customize the
// status code, use WithStatusCode without WithOnLimitReached (the default
// handler respects WithStatusCode).
func DefaultOnLimitReached(w http.ResponseWriter, r *http.Request, result Result) {
	http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
}

// JSONOnLimitReached returns a JSON response when rate limit is exceeded
// with a 429 status code. Use JSONOnLimitReachedWithCode to customize.
func JSONOnLimitReached(w http.ResponseWriter, r *http.Request, result Result) {
	jsonOnLimitReached(w, result, http.StatusTooManyRequests)
}

// JSONOnLimitReachedWithCode creates a JSON response handler with a custom status code.
func JSONOnLimitReachedWithCode(statusCode int) OnLimitReached {
	return func(w http.ResponseWriter, r *http.Request, result Result) {
		jsonOnLimitReached(w, result, statusCode)
	}
}

func jsonOnLimitReached(w http.ResponseWriter, result Result, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	retryAfter := int(math.Ceil(result.RetryAfter.Seconds()))
	_, _ = fmt.Fprintf(w, `{"error":"rate_limit_exceeded","message":"Too many requests","retry_after":%d}`, retryAfter)
}

// Key Functions

// IPKeyFunc extracts the client IP address as the rate limit key.
func IPKeyFunc(r *http.Request) string {
	return GetClientIP(r)
}

// HeaderKeyFunc creates a key function that uses a header value.
func HeaderKeyFunc(header string) KeyFunc {
	return func(r *http.Request) string {
		return r.Header.Get(header)
	}
}

// UserIDKeyFunc creates a key function that uses a user ID from context.
func UserIDKeyFunc(ctxKey any) KeyFunc {
	return func(r *http.Request) string {
		if userID := r.Context().Value(ctxKey); userID != nil {
			return fmt.Sprintf("%v", userID)
		}
		return GetClientIP(r)
	}
}

// PathKeyFunc uses the request path as the key.
func PathKeyFunc(r *http.Request) string {
	return r.URL.Path
}

// MethodPathKeyFunc uses method + path as the key.
func MethodPathKeyFunc(r *http.Request) string {
	return r.Method + ":" + r.URL.Path
}

// IPPathKeyFunc uses IP + path as the key.
func IPPathKeyFunc(r *http.Request) string {
	return GetClientIP(r) + ":" + r.URL.Path
}

// CompositeKeyFunc combines multiple key functions.
func CompositeKeyFunc(funcs ...KeyFunc) KeyFunc {
	return func(r *http.Request) string {
		parts := make([]string, len(funcs))
		for i, fn := range funcs {
			parts[i] = fn(r)
		}
		return strings.Join(parts, ":")
	}
}

// GetClientIP extracts the client IP from the request using only RemoteAddr.
// This is the safe default — it does not trust proxy headers which can be spoofed.
// Use GetClientIPFromHeaders for deployments behind trusted proxies.
func GetClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// GetClientIPFromHeaders extracts the client IP from proxy headers, falling
// back to RemoteAddr. It checks X-Forwarded-For, X-Real-IP, and
// CF-Connecting-IP in order. Only use this when the server is behind a
// trusted reverse proxy that sets these headers.
func GetClientIPFromHeaders(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if i := strings.Index(xff, ","); i > 0 {
			xff = xff[:i]
		}
		xff = strings.TrimSpace(xff)
		if ip := net.ParseIP(xff); ip != nil {
			return ip.String()
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}

	// Check CF-Connecting-IP (Cloudflare)
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		if ip := net.ParseIP(cfip); ip != nil {
			return ip.String()
		}
	}

	return GetClientIP(r)
}

// TrustedProxyKeyFunc creates a key function that extracts client IP from
// proxy headers. Only use when the server is behind a trusted reverse proxy.
func TrustedProxyKeyFunc(r *http.Request) string {
	return GetClientIPFromHeaders(r)
}

// Skip Functions

// SkipHealthChecks skips rate limiting for common health check paths.
func SkipHealthChecks(r *http.Request) bool {
	switch r.URL.Path {
	case "/health", "/healthz", "/ready", "/readyz", "/live", "/livez", "/ping":
		return true
	}
	return false
}

// SkipPrivateIPs skips rate limiting for private IP addresses.
func SkipPrivateIPs(r *http.Request) bool {
	ip := net.ParseIP(GetClientIP(r))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// SkipMethods creates a skip function that skips specific HTTP methods.
func SkipMethods(methods ...string) func(*http.Request) bool {
	methodSet := make(map[string]bool)
	for _, m := range methods {
		methodSet[strings.ToUpper(m)] = true
	}
	return func(r *http.Request) bool {
		return methodSet[r.Method]
	}
}

// SkipPaths creates a skip function that skips specific paths.
func SkipPaths(paths ...string) func(*http.Request) bool {
	pathSet := make(map[string]bool)
	for _, p := range paths {
		pathSet[p] = true
	}
	return func(r *http.Request) bool {
		return pathSet[r.URL.Path]
	}
}

// SkipIf combines multiple skip functions with OR logic.
func SkipIf(funcs ...func(*http.Request) bool) func(*http.Request) bool {
	return func(r *http.Request) bool {
		for _, fn := range funcs {
			if fn(r) {
				return true
			}
		}
		return false
	}
}

// Handler Adapters

// Handler creates an http.Handler that applies rate limiting to another handler.
func Handler(handler http.Handler, limiter KeyedResultLimiter, opts ...MiddlewareOption) http.Handler {
	return Middleware(limiter, opts...)(handler)
}

// HandlerFunc creates an http.HandlerFunc with rate limiting.
func HandlerFunc(fn http.HandlerFunc, limiter KeyedResultLimiter, opts ...MiddlewareOption) http.HandlerFunc {
	return Middleware(limiter, opts...)(fn).ServeHTTP
}

// LimitByPath creates middleware that applies different limits to different paths.
type PathLimiter struct {
	limiters map[string]KeyedResultLimiter
	fallback KeyedResultLimiter
}

// NewPathLimiter creates a new path-based limiter.
func NewPathLimiter(fallback KeyedResultLimiter) *PathLimiter {
	return &PathLimiter{
		limiters: make(map[string]KeyedResultLimiter),
		fallback: fallback,
	}
}

// Add adds a limiter for a specific path.
func (pl *PathLimiter) Add(path string, limiter KeyedResultLimiter) *PathLimiter {
	pl.limiters[path] = limiter
	return pl
}

// Middleware returns the middleware function.
func (pl *PathLimiter) Middleware(opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		keyFunc:    IPKeyFunc,
		statusCode: http.StatusTooManyRequests,
		addHeaders: true,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.onLimitReached == nil {
		cfg.onLimitReached = cfg.defaultOnLimitReached
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skipFunc != nil && cfg.skipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			limiter := pl.fallback
			if l, ok := pl.limiters[r.URL.Path]; ok {
				limiter = l
			}

			key := cfg.keyFunc(r)
			result := limiter.Take(key)

			if cfg.addHeaders {
				AddRateLimitHeaders(w, result)
			}

			if !result.Allowed {
				cfg.onLimitReached(w, r, result)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// WaitMiddleware creates middleware that waits instead of rejecting.
func WaitMiddleware(limiter KeyedLimiter, timeout time.Duration, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	cfg := &middlewareConfig{
		keyFunc:    IPKeyFunc,
		statusCode: http.StatusTooManyRequests,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.onLimitReached == nil {
		cfg.onLimitReached = cfg.defaultOnLimitReached
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skipFunc != nil && cfg.skipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			key := cfg.keyFunc(r)
			if err := limiter.Wait(ctx, key); err != nil {
				cfg.onLimitReached(w, r, Result{Allowed: false})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
