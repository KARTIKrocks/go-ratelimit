# Architecture

This document describes the architecture and design decisions of the ratelimit package.

## Overview

The package is designed around clean interfaces, allowing for:

- Multiple algorithm implementations
- Pluggable storage backends
- Easy testing and mocking
- Future extensibility

## Core Components

### 1. Algorithms

Each algorithm implements the `Limiter` and/or `KeyedLimiter` interfaces:

```go
type Limiter interface {
    Allow() bool
    AllowN(n int) bool
    Wait(ctx context.Context) error
    WaitN(ctx context.Context, n int) error
    Reset()
}

type KeyedLimiter interface {
    Allow(key string) bool
    AllowN(key string, n int) bool
    Wait(ctx context.Context, key string) error
    WaitN(ctx context.Context, key string, n int) error
    Reset(key string)
    ResetAll()
}
```

#### Token Bucket

- **Best for**: General purpose, bursty traffic
- **Memory**: O(1) for single, O(n) for keyed (n = active keys)
- **Accuracy**: High
- **Burst**: Yes, configurable
- **Implementation**: Tokens regenerate over time, consumed per request

#### Leaky Bucket

- **Best for**: Smooth rate limiting, no bursts
- **Memory**: O(1) for single, O(n) for keyed
- **Accuracy**: High
- **Burst**: No (by design)
- **Implementation**: Water leaks at constant rate, requests add water

#### Fixed Window

- **Best for**: Simple counting, memory efficiency
- **Memory**: O(1) for single, O(n) for keyed
- **Accuracy**: Medium (boundary issues)
- **Burst**: Can allow 2x at window boundaries
- **Implementation**: Counter resets at window boundaries

#### Sliding Window

- **Best for**: Accuracy without boundary issues
- **Memory**: O(m) where m = requests in window
- **Accuracy**: Highest
- **Burst**: Configurable
- **Implementation**: Tracks individual request timestamps

#### Sliding Window Counter

- **Best for**: Balance of accuracy and memory
- **Memory**: O(1) for single, O(n) for keyed
- **Accuracy**: High (approximation)
- **Burst**: Configurable
- **Implementation**: Weighted average of current and previous windows

### 2. Storage Backends

#### Memory Store

- **Use**: Single instance deployments
- **Pros**: Fast, no external dependencies
- **Cons**: Not shared across instances
- **Cleanup**: Automatic background goroutine

#### Redis Store

- **Use**: Distributed deployments
- **Pros**: Shared state, persistent
- **Cons**: Network latency, external dependency
- **Features**: Lua scripts for atomicity, supports clusters

### 3. Middleware

HTTP middleware provides:

- Request rate limiting
- Standard headers (X-RateLimit-\*)
- Flexible key extraction
- Skip conditions
- Custom error responses
- Path-based limits

## Design Decisions

### Interfaces Over Concrete Types

Using interfaces allows:

- Easy mocking in tests
- Pluggable implementations
- Future extensibility
- Composition (Multi, Instrumented)

### Context Support

All blocking operations accept `context.Context`:

- Allows cancellation
- Supports timeouts
- Integrates with request lifecycle
- Enables graceful shutdown

### Composite Limiter Safety

The `Multi` limiter uses a mutex to serialize `AllowN` calls across all
sub-limiters, preventing TOCTOU (time-of-check-time-of-use) races when
checking multiple limiters in sequence.

### IP Extraction Security

`GetClientIP` uses only `RemoteAddr` by default, which cannot be spoofed.
Proxy header trust is opt-in via `GetClientIPFromHeaders` or
`TrustedProxyKeyFunc`, preventing IP spoofing attacks.

### Memory Exhaustion Protection

All keyed limiters support `SetMaxKeys(n)` to cap the number of tracked keys.
When the limit is reached, new keys are denied (fail-closed), preventing
attackers from exhausting memory with unique keys.

### Lifecycle Management

The `Closer` interface ensures keyed limiters (which spawn background cleanup
goroutines) can be properly shut down, preventing goroutine leaks.

### Timer Cleanup

All `Wait`/`WaitN` methods use proper timer cleanup (`if !timer.Stop() { <-timer.C }`)
to prevent goroutine leaks from unread timer channels.

### Context Propagation

Redis-backed limiters provide context-aware methods (`TakeNCtx`, `CheckNCtx`,
`ResetCtx`) that propagate caller context through to Redis operations, enabling
proper cancellation and timeout handling in distributed setups.

### Atomic Operations

Redis operations use Lua scripts to ensure:

- Atomicity (no race conditions)
- Efficiency (single network round-trip)
- Consistency (all or nothing)

### Cleanup Strategy

Keyed limiters use background goroutines:

- Periodic cleanup of inactive keys
- Prevents memory leaks
- Configurable interval
- Graceful shutdown via context

### Error Handling

Philosophy:

- Return errors, don't panic
- Fail open by default (allow on errors)
- Provide fail-closed option for strict requirements
- Graceful degradation

## Performance Considerations

### Memory Allocation

- Pre-allocate maps with expected capacity
- Reuse structures where possible
- Minimize allocations in hot paths
- Use sync.Pool for frequent allocations (future)

### Lock Granularity

- RWMutex for read-heavy workloads
- Per-key locking where feasible (future)
- Minimize critical sections
- Avoid holding locks during I/O

### Redis Performance

- Batch operations where possible
- Use Lua scripts for atomicity
- Connection pooling
- Pipeline for bulk operations (future)

## Concurrency Model

### Thread Safety

All public methods are thread-safe:

- Mutex protection for shared state
- Atomic operations for counters
- Goroutine-safe cleanup

### Background Goroutines

Each keyed limiter spawns:

- One cleanup goroutine
- Controlled lifetime via context
- Graceful shutdown on Close()

## Extension Points

### Custom Algorithms

Implement the `Limiter` or `KeyedLimiter` interface:

```go
type CustomLimiter struct {
    // ...
}

func (c *CustomLimiter) Allow() bool {
    // custom logic
}
```

### Custom Storage

Implement the `Store` interface:

```go
type CustomStore struct {
    // ...
}

func (s *CustomStore) Get(ctx context.Context, key string) (int64, time.Time, error) {
    // custom logic
}
```

### Custom Middleware

Build on provided primitives:

```go
func CustomMiddleware(limiter Limiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // custom logic
            next.ServeHTTP(w, r)
        })
    }
}
```

## Future Enhancements

### Planned Features

1. **Distributed consensus**: Use Redis cluster or etcd for distributed limits
2. **Rate limit profiles**: Different limits for different user tiers
3. **Dynamic limits**: Adjust limits based on system load
4. **Batch operations**: Process multiple keys efficiently
5. **Enhanced metrics**: Latency histograms, percentiles
6. **gRPC interceptors**: Native gRPC support
7. **Circuit breaker**: Automatic failure detection
8. **Adaptive rate limiting**: ML-based rate adjustment

### API Stability

- v0.x: Breaking changes possible
- v1.0: Stable API guaranteed
- Deprecation policy: 2 minor versions notice

## Testing Strategy

### Unit Tests

- Test each algorithm independently
- Cover edge cases and error conditions
- Test concurrent access
- Verify cleanup behavior

### Integration Tests

- Test with real Redis
- Test distributed scenarios
- Test failure modes
- Load testing

### Benchmarks

- Track performance regressions
- Compare algorithms
- Measure allocation overhead
- Profile hot paths

## Security Considerations

See [SECURITY.md](SECURITY.md) for:

- Input validation
- DoS protection
- Secret management
- Vulnerability reporting
