# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-02-14

### Added

- Token bucket algorithm (with burst support)
- Leaky bucket algorithm (smooth rate limiting)
- Fixed window algorithm (memory efficient)
- Sliding window log algorithm (accurate, no boundary issues)
- Sliding window counter (memory efficient approximation)
- Per-key rate limiting for all algorithms
- `SetMaxKeys` on all keyed limiters to cap tracked keys and prevent memory exhaustion
- Redis-backed distributed rate limiting (token bucket, sliding window, fixed window)
- Context-aware Redis methods (`TakeNCtx`, `CheckNCtx`, `ResetCtx`)
- HTTP middleware with comprehensive options
- `JSONOnLimitReachedWithCode` factory for custom status codes
- `GetClientIPFromHeaders` and `TrustedProxyKeyFunc` for trusted proxy deployments
- `Closer` interface for lifecycle management of keyed limiters
- Compile-time interface compliance checks
- Composite limiters with mutex-serialized AND logic
- Prometheus metrics integration (via `metrics` subpackage)
- Detailed result information (limit, remaining, retry-after, reset-at)
- Graceful degradation (fail-open on Redis errors)
- Multiple key extraction functions (IP, header, user ID, path, composite)
- Skip functions (health checks, private IPs, paths, methods)
- Path-based rate limiting (`PathLimiter`)
- Wait/block functionality with context support
- Standard rate limit headers (`X-RateLimit-*`, `Retry-After`)
- Comprehensive test suite with >93% coverage
- Benchmarks for performance tracking
- Complete documentation and examples

### Security

- `GetClientIP` uses only `RemoteAddr` by default (not spoofable)
- Proxy header trust is opt-in via `GetClientIPFromHeaders` / `TrustedProxyKeyFunc`
- Proper timer cleanup (`if !timer.Stop() { <-timer.C }`) prevents goroutine leaks
- `Retry-After` header uses `math.Ceil` for accurate values

### Features

- Zero dependencies in core package
- Concurrent-safe operations (all public methods are goroutine-safe)
- Automatic cleanup of inactive keys
- Redis cluster support
- CI/CD with GitHub Actions
- golangci-lint configuration
- Makefile for common tasks
