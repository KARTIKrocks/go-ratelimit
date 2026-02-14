// Package ratelimit provides production-ready rate limiting with multiple algorithms,
// distributed support, and comprehensive HTTP middleware.
//
// # Features
//
// - Multiple algorithms: Token Bucket, Leaky Bucket, Fixed Window, Sliding Window
// - Distributed rate limiting with Redis
// - HTTP middleware for popular frameworks
// - Prometheus metrics integration
// - Per-key and global rate limiting
// - Context-aware operations
// - Graceful degradation
//
// # Quick Start
//
// Basic rate limiting:
//
//	limiter := ratelimit.NewTokenBucket(10.0, 20) // 10 req/s, burst of 20
//	if limiter.Allow() {
//	    // Process request
//	}
//
// HTTP middleware:
//
//	limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
//	handler := ratelimit.Middleware(limiter)(yourHandler)
//	http.ListenAndServe(":8080", handler)
//
// # Algorithms
//
// Token Bucket - Best for general purpose with burst support:
//
//	limiter := ratelimit.NewTokenBucket(
//	    100.0/60.0, // rate: 100 per minute
//	    20,         // burst: 20
//	)
//
// Leaky Bucket - Best for smooth rate limiting without bursts:
//
//	limiter := ratelimit.NewLeakyBucket(
//	    10.0, // rate: 10 per second
//	    50,   // capacity: 50
//	)
//
// Fixed Window - Best for simple counting:
//
//	limiter := ratelimit.NewFixedWindow(
//	    1000,      // limit: 1000
//	    time.Hour, // window: 1 hour
//	)
//
// Sliding Window - Best for accuracy:
//
//	limiter := ratelimit.NewSlidingWindowCounter(
//	    100,       // limit: 100
//	    time.Minute, // window: 1 minute
//	)
//
// # Per-Key Rate Limiting
//
// All algorithms support per-key limiting:
//
//	limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
//	limiter.SetMaxKeys(10000) // Optional: cap tracked keys
//	defer limiter.Close()     // Always close to stop cleanup goroutine
//	limiter.Allow("user:123")
//	limiter.Allow("user:456")
//	limiter.Allow("ip:192.168.1.1")
//
// # Distributed Rate Limiting
//
// Use Redis for multi-instance deployments (via the redisstore submodule):
//
//	import "github.com/KARTIKrocks/go-ratelimit/redisstore"
//
//	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	adapter := redisstore.NewRedisClientAdapter(client)
//	limiter := redisstore.NewRedisTokenBucket(adapter, "myapp", 10.0, 20)
//
// # HTTP Middleware
//
// Basic usage:
//
//	limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
//	handler := ratelimit.Middleware(limiter)(yourHandler)
//
// With options:
//
//	handler := ratelimit.Middleware(limiter,
//	    ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
//	    ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
//	    ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks),
//	)(yourHandler)
//
// Different limits per path:
//
//	pathLimiter := ratelimit.NewPathLimiter(defaultLimiter)
//	pathLimiter.Add("/api/expensive", strictLimiter)
//	handler := pathLimiter.Middleware()(yourHandler)
//
// # Key Functions
//
// Extract rate limit keys from requests:
//
//	ratelimit.IPKeyFunc                    // By IP address (RemoteAddr, safe default)
//	ratelimit.TrustedProxyKeyFunc          // By IP from proxy headers (opt-in)
//	ratelimit.HeaderKeyFunc("X-API-Key")   // By header
//	ratelimit.PathKeyFunc                  // By path
//	ratelimit.IPPathKeyFunc                // By IP + path
//	ratelimit.UserIDKeyFunc("userID")      // By user ID from context
//
// IP extraction functions:
//
//	ratelimit.GetClientIP(r)               // Safe: uses RemoteAddr only
//	ratelimit.GetClientIPFromHeaders(r)    // Trusts X-Forwarded-For, X-Real-IP, CF-Connecting-IP
//
// # Metrics
//
// Built-in metrics collection:
//
//	import "github.com/KARTIKrocks/go-ratelimit/metrics"
//
//	limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
//	instrumented := metrics.NewInstrumented(limiter, "api")
//
//	stats := metrics.GetStats("api")
//	fmt.Printf("Allowed: %d, Denied: %d\n", stats.Allowed, stats.Denied)
//
// Prometheus integration:
//
//	metrics.RegisterPrometheus(prometheus.DefaultRegisterer)
//	// Metrics available at /metrics
//
// # Waiting for Availability
//
// Block until request is allowed:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	if err := limiter.Wait(ctx, "user:123"); err != nil {
//	    // Timeout or cancelled
//	}
//
// # Detailed Results
//
// Get detailed rate limit information:
//
//	result := limiter.Take("user:123")
//	if !result.Allowed {
//	    fmt.Printf("Rate limited. Retry after: %v\n", result.RetryAfter)
//	    fmt.Printf("Limit: %d, Remaining: %d\n", result.Limit, result.Remaining)
//	    fmt.Printf("Resets at: %v\n", result.ResetAt)
//	}
//
// # Composite Limiters
//
// Combine multiple limiters with mutex-serialized AND logic:
//
//	multi := ratelimit.NewMulti(
//	    perSecondLimiter,
//	    perMinuteLimiter,
//	    perHourLimiter,
//	)
//
// # Custom Error Responses
//
// Built-in JSON response with custom status code:
//
//	ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached)
//	ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReachedWithCode(http.StatusServiceUnavailable))
//
// # Error Handling
//
// The library gracefully handles errors:
//
//	limiter := redisstore.NewRedisTokenBucket(client, "app", 10.0, 20)
//	// If Redis is down, requests fail open (allowed) by default
//
// # Performance
//
// All implementations are designed for high performance:
//
//   - Token Bucket: ~115 ns/op, 0 allocs
//   - Keyed Token Bucket: ~240 ns/op, 0 allocs
//   - Redis Token Bucket: ~12 μs/op (network overhead)
//   - Middleware: ~850 ns/op, 2 allocs
//
// # Thread Safety
//
// All methods are thread-safe and can be called concurrently.
//
// # Cleanup
//
// Keyed limiters automatically clean up inactive keys:
//
//	limiter := ratelimit.NewKeyedTokenBucket(
//	    10.0,
//	    20,
//	    5*time.Minute, // Cleanup every 5 minutes
//	)
//	defer limiter.Close() // Stop cleanup goroutine
//
// # Examples
//
// See the examples directory for complete working examples:
//   - examples/basic: Basic HTTP server
//   - examples/redis: Distributed rate limiting
//   - examples/multitier: Different limits per endpoint
//   - examples/prometheus: Metrics integration
package ratelimit
