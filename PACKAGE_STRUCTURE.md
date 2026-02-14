# Package Structure

```
ratelimit/
├── README.md                   # Main documentation
├── LICENSE                     # MIT License
├── CHANGELOG.md               # Version history
├── CONTRIBUTING.md            # Contribution guidelines
├── SECURITY.md                # Security policy
├── ARCHITECTURE.md            # Architecture documentation
├── QUICKSTART.md              # Quick start guide
├── Makefile                   # Development tasks
├── go.mod                     # Go module definition
├── go.work                    # Go workspace (multi-module)
├── doc.go                     # Package documentation
│
├── .github/
│   └── workflows/
│       └── ci.yml             # GitHub Actions CI
│
├── Core Files
├── limiter.go                 # Core interfaces, types, Multi, MemoryStore
├── token_bucket.go            # Token bucket algorithm
├── leaky_bucket.go            # Leaky bucket algorithm
├── fixed_window.go            # Fixed window algorithm
├── sliding_window.go          # Sliding window algorithms
├── middleware.go              # HTTP middleware and utilities
│
├── Tests
├── tokenbucket_test.go        # Token bucket tests
├── leakybucket_test.go        # Leaky bucket tests
├── fixedwindow_test.go        # Fixed window tests
├── slidingwindow_test.go      # Sliding window tests
├── multi_test.go              # Multi (composite) limiter tests
├── store_test.go              # MemoryStore tests
├── middleware_test.go         # Middleware tests
├── validation_test.go         # Input validation tests
├── coverage_test.go           # Coverage helper tests
│
├── redisstore/                # Redis-backed distributed limiters (submodule)
│   ├── go.mod                 # Separate module (depends on go-redis)
│   ├── redisstore.go          # Redis limiter implementations
│   └── adapter.go             # Redis client adapter
│
├── metrics/                   # Metrics package (submodule)
│   ├── go.mod                 # Separate module (depends on prometheus)
│   ├── metrics.go             # Metrics collection and Prometheus integration
│   ├── instrumented.go        # Instrumented wrappers
│   └── metrics_test.go        # Metrics tests
│
└── examples/                  # Example applications
    ├── basic/                 # Basic HTTP server
    │   └── main.go
    ├── redis/                 # Distributed rate limiting
    │   └── main.go
    ├── multitier/             # Multi-tier limits
    │   └── main.go
    └── prometheus/            # Prometheus metrics
        └── main.go
```

## File Descriptions

### Documentation

- **README.md**: Main entry point with quickstart, features, and examples
- **LICENSE**: MIT license
- **CHANGELOG.md**: Version history and changes
- **CONTRIBUTING.md**: Guidelines for contributors
- **SECURITY.md**: Security policy and vulnerability reporting
- **ARCHITECTURE.md**: Design decisions and architecture overview
- **QUICKSTART.md**: Quick start guide with step-by-step tutorial
- **doc.go**: godoc package documentation

### Configuration

- **Makefile**: Common development tasks (test, lint, fmt, etc.)
- **go.mod**: Go module dependencies
- **go.work**: Go workspace for multi-module development
- **.github/workflows/ci.yml**: GitHub Actions CI pipeline

### Core Implementation

- **limiter.go**: Core interfaces (Limiter, KeyedLimiter, ResultLimiter, KeyedResultLimiter, Store, Closer), Result type, Multi composite limiter, MemoryStore
- **token_bucket.go**: Token bucket algorithm (single + keyed)
- **leaky_bucket.go**: Leaky bucket algorithm (single + keyed)
- **fixed_window.go**: Fixed window algorithm (single + keyed)
- **sliding_window.go**: Sliding window algorithms (log and counter, single + keyed)
- **middleware.go**: HTTP middleware, key functions, skip functions, response handlers

### Redis (submodule: `redisstore/`)

- **redisstore/redisstore.go**: Redis-backed distributed limiters (token bucket, sliding window, fixed window) with Lua scripts for atomicity
- **redisstore/adapter.go**: Adapter for go-redis client

### Testing

- **\*\_test.go**: Comprehensive unit and integration tests
- Benchmarks included in test files
- Race detector compatible
- \> 93% coverage

### Metrics (submodule: `metrics/`)

- **metrics/metrics.go**: Metrics collection and Prometheus integration
- **metrics/instrumented.go**: Wrapper for automatic metrics collection

### Examples

Each example is a complete, runnable application demonstrating:

- Different algorithms
- Redis integration
- Metrics collection
- Production patterns

## Key Features by File

### limiter.go

- Core interfaces: `Limiter`, `KeyedLimiter`, `ResultLimiter`, `KeyedResultLimiter`
- `Store` interface for pluggable storage backends
- `Closer` interface for lifecycle management
- `Result` type with limit, remaining, retry-after, reset-at
- `NewMulti`: Composite limiter with mutex-serialized AND logic
- `NewMemoryStore`: In-memory store with automatic cleanup
- Compile-time interface compliance checks

### token_bucket.go

- `NewTokenBucket`: Creates single-instance limiter
- `NewKeyedTokenBucket`: Creates per-key limiter
- `SetMaxKeys`: Cap tracked keys to prevent memory exhaustion
- Supports burst traffic
- Tokens refill over time
- O(1) operations

### leaky_bucket.go

- `NewLeakyBucket`: Creates single-instance limiter
- `NewKeyedLeakyBucket`: Creates per-key limiter
- `SetMaxKeys`: Cap tracked keys to prevent memory exhaustion
- Smooth rate limiting
- No burst support (by design)
- Queue-based implementation

### fixed_window.go

- `NewFixedWindow`: Creates single-instance limiter
- `NewKeyedFixedWindow`: Creates per-key limiter
- `SetMaxKeys`: Cap tracked keys to prevent memory exhaustion
- Simple and memory-efficient
- Boundary issues (can allow 2x at edges)
- Fast O(1) operations

### sliding_window.go

- `NewSlidingWindow`: Timestamp-based (accurate)
- `NewSlidingWindowCounter`: Counter-based (efficient)
- `NewKeyedSlidingWindow`: Per-key version
- `SetMaxKeys`: Cap tracked keys to prevent memory exhaustion
- No boundary issues
- More accurate than fixed window

### redisstore/redisstore.go

- `NewRedisTokenBucket`: Distributed token bucket
- `NewRedisSlidingWindow`: Distributed sliding window
- `NewRedisFixedWindow`: Distributed fixed window
- Context-aware methods (`TakeNCtx`, `CheckNCtx`, `ResetCtx`)
- Lua scripts for atomicity
- Cluster support
- Fail-open on Redis errors

### middleware.go

- `Middleware`: Main HTTP middleware
- `WithKeyFunc`: Custom key extraction
- `WithOnLimitReached`: Custom error handling
- `WithSkipFunc`: Skip conditions
- `PathLimiter`: Per-path limits
- `GetClientIP`: Safe IP extraction (RemoteAddr only)
- `GetClientIPFromHeaders`: Proxy-aware IP extraction (opt-in)
- `TrustedProxyKeyFunc`: Key function for trusted proxy deployments
- `JSONOnLimitReached`: Built-in JSON error response
- `JSONOnLimitReachedWithCode`: JSON error response with custom status code
- Various key functions (IP, header, path, composite)
- Skip functions (health checks, private IPs, paths, methods)

## Usage Patterns

### Import

```go
import "github.com/KARTIKrocks/go-ratelimit"
```

### Basic Limiter

```go
limiter := ratelimit.NewTokenBucket(10.0, 20)
```

### Per-Key Limiter

```go
limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
limiter.SetMaxKeys(10000) // Optional: cap tracked keys
defer limiter.Close()
```

### HTTP Middleware

```go
handler := ratelimit.Middleware(limiter)(yourHandler)
```

### With Metrics

```go
import "github.com/KARTIKrocks/go-ratelimit/metrics"

limiter := metrics.NewInstrumented(baseLimiter, "api")
```

### Distributed (Redis)

```go
import "github.com/KARTIKrocks/go-ratelimit/redisstore"

adapter := redisstore.NewRedisClientAdapter(client)
limiter := redisstore.NewRedisTokenBucket(adapter, "myapp", 10.0, 20)
```

## Development Workflow

1. **Install tools**: `make install-tools`
2. **Run tests**: `make test-race`
3. **Check coverage**: `make test-cover`
4. **Lint code**: `make lint`
5. **Format code**: `make fmt`
6. **Run examples**: `make example-basic`
7. **Pre-commit**: `make pre-commit`

## Publishing Workflow

1. Update version in CHANGELOG.md
2. Run `make ci` to verify all checks pass
3. Commit and push changes
4. Create and push tag: `git tag v0.1.0 && git push origin v0.1.0`
5. GitHub Actions will run CI
6. Package appears on pkg.go.dev automatically

## Support and Resources

- **Documentation**: https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit
- **Issues**: https://github.com/KARTIKrocks/go-ratelimit/issues
- **Discussions**: https://github.com/KARTIKrocks/go-ratelimit/discussions
