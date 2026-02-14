# ratelimit

[![Go Reference](https://pkg.go.dev/badge/github.com/KARTIKrocks/go-ratelimit.svg)](https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit)
[![Go Report Card](https://goreportcard.com/badge/github.com/KARTIKrocks/go-ratelimit)](https://goreportcard.com/report/github.com/KARTIKrocks/go-ratelimit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A production-ready, high-performance rate limiting library for Go with multiple algorithms, distributed support, and comprehensive middleware.

## Features

- 🚀 **Multiple Algorithms**: Token Bucket, Leaky Bucket, Fixed Window, Sliding Window Log, Sliding Window Counter
- 🌍 **Distributed Support**: Redis-backed limiters for multi-instance deployments
- 🔌 **HTTP Middleware**: Ready-to-use middleware for popular frameworks
- 📊 **Observability**: Built-in metrics and Prometheus integration
- ⚡ **High Performance**: Minimal allocations, concurrent-safe operations
- 🛡️ **Production Ready**: Comprehensive error handling, graceful degradation
- 🎯 **Flexible**: Per-key limiting, composite limiters, custom backends
- 📦 **Zero Dependencies**: Core package has no external dependencies

## Installation

```bash
go get github.com/KARTIKrocks/go-ratelimit
```

## Quick Start

### Basic Usage

```go
package main

import (
    "fmt"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // Create a token bucket limiter: 100 requests per minute, burst of 10
    limiter := ratelimit.NewTokenBucket(100.0/60.0, 10)

    if limiter.Allow() {
        fmt.Println("Request allowed")
    } else {
        fmt.Println("Rate limit exceeded")
    }
}
```

### HTTP Middleware

```go
package main

import (
    "net/http"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // Create a keyed limiter (per-IP)
    limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)

    // Wrap your handler
    mux := http.NewServeMux()
    mux.HandleFunc("/api", apiHandler)

    // Apply rate limiting middleware
    handler := ratelimit.Middleware(limiter,
        ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
        ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
    )(mux)

    http.ListenAndServe(":8080", handler)
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("API response"))
}
```

## Algorithms

### Token Bucket

Best for: APIs with burst traffic, general purpose rate limiting

```go
// 100 requests per minute, burst of 20
limiter := ratelimit.NewTokenBucket(100.0/60.0, 20)
```

**Pros**: Allows bursts, smooth rate limiting  
**Cons**: More complex than fixed window

### Leaky Bucket

Best for: Smooth, consistent rate limiting without bursts

```go
// 10 requests per second, queue capacity of 50
limiter := ratelimit.NewLeakyBucket(10.0, 50)
```

**Pros**: Smooth rate, no bursts  
**Cons**: Queues requests, may add latency

### Fixed Window

Best for: Simple counting, memory-efficient

```go
// 1000 requests per hour
limiter := ratelimit.NewFixedWindow(1000, time.Hour)
```

**Pros**: Simple, memory efficient  
**Cons**: Boundary issues (2x burst at window edges)

### Sliding Window

Best for: Accurate rate limiting without boundary issues

```go
// 100 requests per minute
limiter := ratelimit.NewSlidingWindowCounter(100, time.Minute)
```

**Pros**: Accurate, no boundary issues  
**Cons**: Slightly more memory than fixed window

## Per-Key Rate Limiting

All algorithms support per-key limiting (e.g., per-user, per-IP):

```go
// Create keyed limiter
limiter := ratelimit.NewKeyedTokenBucket(
    10.0,           // rate per second
    20,             // burst
    time.Minute,    // cleanup interval
)

// Different keys get independent limits
limiter.Allow("user:123")
limiter.Allow("user:456")
limiter.Allow("ip:192.168.1.1")
```

## Distributed Rate Limiting (Redis)

For multi-instance deployments:

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/KARTIKrocks/go-ratelimit/redisstore"
)

// Create Redis client
client := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})

// Create distributed limiter
adapter := redisstore.NewRedisClientAdapter(client)
limiter := redisstore.NewRedisTokenBucket(
    adapter,
    "myapp",    // key prefix
    10.0,       // rate per second
    20,         // burst
)

// Use normally - works across all instances
limiter.Allow("user:123")

// Context-aware operations (for cancellation/timeouts)
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
result := limiter.TakeNCtx(ctx, "user:123", 1)
```

## HTTP Middleware Options

### Custom Key Functions

```go
// By IP address (default, uses RemoteAddr - safe, not spoofable)
ratelimit.WithKeyFunc(ratelimit.IPKeyFunc)

// By IP from proxy headers (opt-in, trusts X-Forwarded-For/X-Real-IP/CF-Connecting-IP)
ratelimit.WithKeyFunc(ratelimit.TrustedProxyKeyFunc)

// By header
ratelimit.WithKeyFunc(ratelimit.HeaderKeyFunc("X-API-Key"))

// By user ID from context
ratelimit.WithKeyFunc(ratelimit.UserIDKeyFunc("userID"))

// By path
ratelimit.WithKeyFunc(ratelimit.PathKeyFunc)

// Composite (IP + Path)
ratelimit.WithKeyFunc(ratelimit.IPPathKeyFunc)
```

### Custom Responses

```go
// JSON response (built-in, 429 status)
ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached)

// JSON response with custom status code
ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReachedWithCode(http.StatusServiceUnavailable))

// Fully custom response
ratelimit.WithOnLimitReached(func(w http.ResponseWriter, r *http.Request, result ratelimit.Result) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusTooManyRequests)
    fmt.Fprintf(w, `{"error":"slow down!","retry_after":%d}`,
        int(math.Ceil(result.RetryAfter.Seconds())))
})
```

### Skip Conditions

```go
// Skip health checks
ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks)

// Skip private IPs
ratelimit.WithSkipFunc(ratelimit.SkipPrivateIPs)

// Skip specific paths
ratelimit.WithSkipFunc(ratelimit.SkipPaths("/metrics", "/health"))

// Combine multiple conditions
ratelimit.WithSkipFunc(ratelimit.SkipIf(
    ratelimit.SkipHealthChecks,
    ratelimit.SkipPrivateIPs,
))
```

## Different Limits Per Path

```go
// Create path-based limiter
pathLimiter := ratelimit.NewPathLimiter(
    defaultLimiter, // fallback for unlisted paths
)

// Add specific limits
pathLimiter.Add("/api/expensive", strictLimiter)
pathLimiter.Add("/api/login", loginLimiter)

// Use as middleware
handler := pathLimiter.Middleware()(yourHandler)
```

## Metrics and Monitoring

### Built-in Metrics

```go
import "github.com/KARTIKrocks/go-ratelimit/metrics"

// Create limiter with metrics
limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
metricsLimiter := metrics.NewInstrumented(limiter, "api")

// Metrics are automatically collected
metricsLimiter.Allow("user:123")

// Get metrics
stats := metrics.GetStats("api")
fmt.Printf("Allowed: %d, Denied: %d\n", stats.Allowed, stats.Denied)
```

### Prometheus Integration

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/KARTIKrocks/go-ratelimit/metrics"
)

// Register Prometheus metrics
metrics.RegisterPrometheus(prometheus.DefaultRegisterer)

// Use instrumented limiter
limiter := metrics.NewInstrumented(baseLimiter, "api")

// Metrics available at /metrics endpoint
```

## Advanced Features

### Composite Limiters

Combine multiple limiters with AND logic:

```go
// Both limits must allow the request
multi := ratelimit.NewMulti(
    perSecondLimiter,
    perMinuteLimiter,
    perHourLimiter,
)

if multi.Allow() {
    // Request allowed by all limiters
}
```

### Waiting for Availability

```go
ctx := context.Background()

// Block until request is allowed (or context cancelled)
if err := limiter.Wait(ctx, "user:123"); err != nil {
    // Context cancelled or deadline exceeded
    return err
}

// Request is now allowed
processRequest()
```

### Detailed Results

```go
result := limiter.Take("user:123")

fmt.Printf("Allowed: %v\n", result.Allowed)
fmt.Printf("Limit: %d\n", result.Limit)
fmt.Printf("Remaining: %d\n", result.Remaining)
fmt.Printf("Retry After: %v\n", result.RetryAfter)
fmt.Printf("Reset At: %v\n", result.ResetAt)
```

## Configuration Best Practices

### API Rate Limiting

```go
// Public endpoints: 100 req/min per IP
publicLimiter := ratelimit.NewKeyedTokenBucket(100.0/60.0, 10, time.Minute)

// Authenticated: 1000 req/min per user
authLimiter := ratelimit.NewKeyedTokenBucket(1000.0/60.0, 50, time.Minute)

// Expensive operations: 10 req/min per user
expensiveLimiter := ratelimit.NewKeyedTokenBucket(10.0/60.0, 2, time.Minute)
```

### Distributed Systems

```go
// Use Redis for shared state across instances
limiter := redisstore.NewRedisTokenBucket(
    adapter,
    "prod:api",
    100.0/60.0,
    20,
)
```

### Memory Management

```go
// Set cleanup interval to remove inactive keys
limiter := ratelimit.NewKeyedTokenBucket(
    10.0,
    20,
    5*time.Minute, // cleanup every 5 minutes
)

// Cap tracked keys to prevent memory exhaustion
limiter.SetMaxKeys(10000)

// Don't forget to close on shutdown
defer limiter.Close()
```

## Error Handling

The library provides graceful degradation:

```go
// Redis errors won't crash your app
limiter := redisstore.NewRedisTokenBucket(adapter, "app", 10.0, 20)

// If Redis is down, requests fail open (allowed) by default
// This prevents Redis outages from blocking all traffic
result := limiter.Take("user:123")
// result.Allowed == true when Redis is unreachable
```

## Examples

See the [examples](examples/) directory for complete working examples:

- [Basic HTTP Server](examples/basic/main.go)
- [Redis Distributed](examples/redis/main.go)
- [Multi-tier Limits](examples/multitier/main.go)
- [Prometheus Metrics](examples/prometheus/main.go)

## Testing

Run tests:

```bash
go test ./...
```

Run with race detector:

```bash
go test -race ./...
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./...
```

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Credits

Inspired by:

- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)
- [Stripe's rate limiting architecture](https://stripe.com/blog/rate-limiters)
- [Token Bucket algorithm](https://en.wikipedia.org/wiki/Token_bucket)

## Support

- 📚 [Documentation](https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit)
- 🐛 [Issue Tracker](https://github.com/KARTIKrocks/go-ratelimit/issues)
- 💬 [Discussions](https://github.com/KARTIKrocks/go-ratelimit/discussions)
