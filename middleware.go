package ratelimit

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"slices"
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

// ipKey turns a client IP into a rate-limit key. IPv6 addresses are reduced
// to their /64 network: a single client is usually given a whole /64, so
// keying by full address would let it rotate through 2^64 keys. IPv4-mapped
// IPv6 addresses are keyed as IPv4. Strings that are not IPs are returned
// unchanged.
func ipKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

// IPKeyFunc uses the client IP address (RemoteAddr) as the rate limit key.
// IPv6 clients are keyed by their /64 network.
func IPKeyFunc(r *http.Request) string {
	return ipKey(GetClientIP(r))
}

// HeaderKeyFunc creates a key function that uses a header value, such as an
// API key. Requests without the header are keyed by client IP instead of
// sharing one bucket. Header and IP keys are prefixed differently, so a
// header value can never collide with another client's IP key.
func HeaderKeyFunc(header string) KeyFunc {
	return func(r *http.Request) string {
		if v := r.Header.Get(header); v != "" {
			return "header:" + v
		}
		return "ip:" + IPKeyFunc(r)
	}
}

// UserIDKeyFunc creates a key function that uses a user ID from context.
func UserIDKeyFunc(ctxKey any) KeyFunc {
	return func(r *http.Request) string {
		if userID := r.Context().Value(ctxKey); userID != nil {
			return fmt.Sprintf("%v", userID)
		}
		return IPKeyFunc(r)
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

// IPPathKeyFunc uses IP + path as the key. IPv6 clients are keyed by their
// /64 network.
func IPPathKeyFunc(r *http.Request) string {
	return IPKeyFunc(r) + ":" + r.URL.Path
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
// CF-Connecting-IP in order. Only use this when the server is behind exactly
// one trusted reverse proxy that sets these headers.
//
// For X-Forwarded-For it uses the rightmost entry, which is the address the
// proxy itself observed. Entries further left are supplied by the client and
// can be forged. For deployments with several proxy hops, use
// TrustedProxiesKeyFunc.
func GetClientIPFromHeaders(r *http.Request) string {
	if ips := forwardedFor(r); len(ips) > 0 && ips[len(ips)-1] != nil {
		return ips[len(ips)-1].String()
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
// proxy headers. Only use when the server is behind a single trusted reverse
// proxy. See GetClientIPFromHeaders. IPv6 clients are keyed by their /64
// network.
func TrustedProxyKeyFunc(r *http.Request) string {
	return ipKey(GetClientIPFromHeaders(r))
}

// TrustedProxiesKeyFunc creates a key function for deployments behind one or
// more reverse proxies. trustedCIDRs lists the proxy networks, for example
// "10.0.0.0/8"; a bare IP is treated as a single-address network.
//
// Proxy headers are honored only when the request comes directly from a
// trusted proxy. The client IP is the rightmost X-Forwarded-For entry that is
// not itself a trusted proxy. Otherwise the key is RemoteAddr. IPv6 clients
// are keyed by their /64 network.
// It panics if a CIDR cannot be parsed.
func TrustedProxiesKeyFunc(trustedCIDRs ...string) KeyFunc {
	nets := make([]*net.IPNet, 0, len(trustedCIDRs))
	for _, c := range trustedCIDRs {
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil && ip.To4() != nil {
				// Normalise IPv4-mapped IPv6 (::ffff:a.b.c.d) to plain IPv4
				// so the /32 applies to the IPv4 address.
				c = ip.To4().String() + "/32"
			} else {
				c += "/128"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("ratelimit: invalid trusted proxy CIDR " + strconv.Quote(c))
		}
		nets = append(nets, n)
	}

	trusted := func(ip net.IP) bool {
		for _, n := range nets {
			if n.Contains(ip) {
				return true
			}
		}
		return false
	}

	return func(r *http.Request) string {
		remote := GetClientIP(r)
		if ip := net.ParseIP(remote); ip == nil || !trusted(ip) {
			return ipKey(remote)
		}
		ips := forwardedFor(r)
		client := remote
		for _, ip := range slices.Backward(ips) {
			if ip == nil {
				// Trusted proxies always append valid IPs, so a malformed
				// entry was not written by one. Stop at the last good hop.
				break
			}
			client = ip.String()
			if !trusted(ip) {
				break
			}
		}
		return ipKey(client)
	}
}

// forwardedFor parses every X-Forwarded-For header on r, in order.
// Entries that are not valid IPs are kept as nil so positions are preserved.
func forwardedFor(r *http.Request) []net.IP {
	var ips []net.IP
	for _, h := range r.Header.Values("X-Forwarded-For") {
		for part := range strings.SplitSeq(h, ",") {
			ips = append(ips, net.ParseIP(strings.TrimSpace(part)))
		}
	}
	return ips
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
