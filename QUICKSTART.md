# Quick Start Guide

## Installation

```bash
# Initialize your project (if not already done)
go mod init myapp

# Add the rate limiter
go get github.com/KARTIKrocks/go-ratelimit
```

## 5-Minute Tutorial

### Step 1: Basic Rate Limiting

```go
package main

import (
    "fmt"
    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // Create a limiter: 10 requests per second, burst of 20
    limiter := ratelimit.NewTokenBucket(10.0, 20)

    // Check if request is allowed
    if limiter.Allow() {
        fmt.Println("Request allowed!")
    } else {
        fmt.Println("Rate limited!")
    }
}
```

### Step 2: HTTP Server with Rate Limiting

```go
package main

import (
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // Create per-IP rate limiter
    limiter := ratelimit.NewKeyedTokenBucket(
        10.0,        // 10 requests per second
        20,          // burst of 20
        time.Minute, // cleanup inactive IPs every minute
    )
    defer limiter.Close()

    // Your API handler
    apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintf(w, "Hello! You made a request at %s\n", time.Now())
    })

    // Wrap with rate limiting
    http.Handle("/api", ratelimit.Middleware(limiter)(apiHandler))

    log.Println("Server running on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

**Test it:**

```bash
# Run the server
go run main.go

# In another terminal, make requests
for i in {1..25}; do curl http://localhost:8080/api; done
```

### Step 3: Different Limits per Endpoint

```go
package main

import (
    "fmt"
    "net/http"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // Generous limit for public endpoints
    publicLimiter := ratelimit.NewKeyedTokenBucket(100.0/60.0, 10, time.Minute)
    defer publicLimiter.Close()

    // Strict limit for expensive operations
    expensiveLimiter := ratelimit.NewKeyedTokenBucket(10.0/60.0, 2, time.Minute)
    defer expensiveLimiter.Close()

    // Create path-based limiter
    pathLimiter := ratelimit.NewPathLimiter(publicLimiter)
    pathLimiter.Add("/api/expensive", expensiveLimiter)

    // Setup handlers
    http.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprint(w, "Public endpoint - 100 req/min")
    })

    http.HandleFunc("/api/expensive", func(w http.ResponseWriter, r *http.Request) {
        time.Sleep(100 * time.Millisecond) // Simulate expensive operation
        fmt.Fprint(w, "Expensive endpoint - 10 req/min")
    })

    // Apply rate limiting
    http.ListenAndServe(":8080", pathLimiter.Middleware()(http.DefaultServeMux))
}
```

### Step 4: Distributed Rate Limiting with Redis

```go
package main

import (
    "log"
    "net/http"
    "os"

    "github.com/redis/go-redis/v9"
    "github.com/KARTIKrocks/go-ratelimit"
    "github.com/KARTIKrocks/go-ratelimit/redisstore"
)

func main() {
    // Connect to Redis
    client := redis.NewClient(&redis.Options{
        Addr: os.Getenv("REDIS_URL"), // e.g., "localhost:6379"
    })

    // Create distributed limiter
    adapter := redisstore.NewRedisClientAdapter(client)
    limiter := redisstore.NewRedisTokenBucket(
        adapter,
        "myapp",  // key prefix
        10.0,     // 10 requests per second
        20,       // burst of 20
    )

    // Now all your app instances share the same rate limits!
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Shared rate limit across all instances!\n"))
    })

    http.Handle("/api", ratelimit.Middleware(limiter)(handler))
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

**Run with Docker:**

```bash
# Start Redis
docker run -d -p 6379:6379 redis:7-alpine

# Set environment and run
export REDIS_URL=localhost:6379
go run main.go
```

### Step 5: Adding Metrics

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/KARTIKrocks/go-ratelimit"
    "github.com/KARTIKrocks/go-ratelimit/metrics"
)

func main() {
    // Register Prometheus metrics
    metrics.RegisterPrometheus(prometheus.DefaultRegisterer)

    // Create base limiter
    baseLimiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
    defer baseLimiter.Close()

    // Wrap with metrics collection
    limiter := metrics.NewInstrumented(baseLimiter, "api")

    // API endpoint
    http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("API response\n"))
    })

    // Metrics endpoint
    http.Handle("/metrics", promhttp.Handler())

    // Apply rate limiting
    handler := ratelimit.Middleware(limiter)(http.DefaultServeMux)

    log.Println("API: http://localhost:8080/api")
    log.Println("Metrics: http://localhost:8080/metrics")
    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

**View metrics:**

```bash
curl http://localhost:8080/metrics | grep ratelimit
```

## Common Patterns

### By User ID (from authentication)

```go
func getUserKey(r *http.Request) string {
    // Extract user ID from JWT, session, etc.
    userID := r.Context().Value("userID").(string)
    return "user:" + userID
}

handler := ratelimit.Middleware(limiter,
    ratelimit.WithKeyFunc(getUserKey),
)(yourHandler)
```

### By API Key

```go
handler := ratelimit.Middleware(limiter,
    ratelimit.WithKeyFunc(ratelimit.HeaderKeyFunc("X-API-Key")),
)(yourHandler)
```

### Skip Health Checks

```go
handler := ratelimit.Middleware(limiter,
    ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks),
)(yourHandler)
```

### Custom Error Response

```go
handler := ratelimit.Middleware(limiter,
    ratelimit.WithOnLimitReached(func(w http.ResponseWriter, r *http.Request, result ratelimit.Result) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusTooManyRequests)
        fmt.Fprintf(w, `{"error":"Too fast! Wait %d seconds"}`,
            int(math.Ceil(result.RetryAfter.Seconds())))
    }),
)(yourHandler)
```

## Algorithm Cheat Sheet

| Algorithm      | Best For        | Memory | Bursts     | Accuracy |
| -------------- | --------------- | ------ | ---------- | -------- |
| Token Bucket   | General purpose | Low    | Yes        | High     |
| Leaky Bucket   | Smooth rate     | Low    | No         | High     |
| Fixed Window   | Simple counting | Lowest | Edge cases | Medium   |
| Sliding Window | No edge cases   | Medium | Yes        | Highest  |

## Next Steps

1. Read the [README](README.md) for comprehensive documentation
2. Check [examples/](examples/) for complete applications
3. Review [ARCHITECTURE.md](ARCHITECTURE.md) to understand internals
4. Review [SECURITY.md](SECURITY.md) for security best practices
5. Contribute! See [CONTRIBUTING.md](CONTRIBUTING.md)

## Common Issues

### Issue: Rate limits not working

**Solution:** Check that you're using a keyed limiter for per-user/IP limits:

```go
// ❌ Wrong - all users share same limit
limiter := ratelimit.NewTokenBucket(10.0, 20)

// ✅ Correct - each user/IP gets own limit
limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
```

### Issue: Memory growing over time

**Solution:** Ensure cleanup is enabled and limiter is closed:

```go
limiter := ratelimit.NewKeyedTokenBucket(
    10.0, 20,
    time.Minute, // ✅ Enable cleanup
)
defer limiter.Close() // ✅ Always close when done
```

### Issue: Distributed limits not working

**Solution:** Verify Redis connectivity:

```go
ctx := context.Background()
if err := client.Ping(ctx).Err(); err != nil {
    log.Fatal("Redis connection failed:", err)
}
```

## Get Help

- **Documentation**: https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit
- **Examples**: [examples/](examples/) directory
- **Issues**: https://github.com/KARTIKrocks/go-ratelimit/issues
- **Discussions**: https://github.com/KARTIKrocks/go-ratelimit/discussions

Happy rate limiting! 🚀
