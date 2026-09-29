# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- IP-based key functions (`IPKeyFunc`, `IPPathKeyFunc`, `TrustedProxyKeyFunc`,
  `TrustedProxiesKeyFunc` and the `UserIDKeyFunc` fallback) key IPv6 clients
  by their /64 network instead of the full address. A client that controls a
  /64 could otherwise rotate through 2^64 addresses to avoid its limit.
  IPv4-mapped IPv6 addresses are keyed as IPv4. `GetClientIP` still returns
  the exact address.
- `HeaderKeyFunc` keys requests without the header by client IP. Previously
  they all shared one empty-key bucket, so one client could use it up for
  everyone.

### Changed

- **Breaking:** `HeaderKeyFunc` keys are now prefixed (`header:<value>`, or
  `ip:<address>` without the header), so a header value can't collide with
  another client's IP key. Existing Redis counters for these keys reset once.

## [1.1.0] - 2026-09-29

Released together with the first tagged versions of the submodules,
`redisstore/v1.0.0` and `metrics/v1.0.0`. `redisstore` requires the root
module at `v1.1.0` or later.

### Security

- `GetClientIPFromHeaders` and `TrustedProxyKeyFunc` now use the rightmost
  `X-Forwarded-For` entry instead of the leftmost, which clients can forge to
  bypass rate limiting. Deployments with several proxy hops should switch to
  the new `TrustedProxiesKeyFunc`.

  **Breaking:** with more than one proxy hop, the rightmost entry is now a
  proxy's address, so all clients behind that proxy share one bucket. For
  example, behind Cloudflare plus your own load balancer the key becomes a
  Cloudflare edge IP. Use `TrustedProxiesKeyFunc` with your proxy ranges, or
  `HeaderKeyFunc("CF-Connecting-IP")` if only Cloudflare can reach your
  origin.

### Added

- `TrustedProxiesKeyFunc(trustedCIDRs...)` for deployments behind one or more
  known proxies
- `ErrInvalidN` and `ErrExceedsLimit` errors
- `redisstore.WithFailClosed()` to deny requests on Redis errors, and
  `redisstore.WithErrorHandler(fn)` to log or count them. Redis constructors
  accept these as optional trailing arguments.
- `redisstore` limiters implement `ResetAll` (and `ResetAllCtx`), which was a
  silent no-op. It needs a client implementing the new optional `KeyDeleter`
  interface; both built-in adapters do, including on Redis Cluster. Other
  clients get `ErrResetAllUnsupported` through the error handler.

### Changed

- **Breaking (`redisstore`):** each rate-limit key is now stored in a single
  Redis hash (`<prefix>:<algorithm>:{<key>}`). Existing counters are ignored
  after upgrading, so every client starts with a fresh limit once.
- `redisstore` limiters read the time from Redis instead of each application
  server, so clock skew between servers no longer affects limits
- `redisstore` requires the root module at `v1.1.0`
- **Breaking:** when a keyed limiter reaches `SetMaxKeys`, it now evicts the
  least recently used key instead of denying every new key. Previously an
  attacker could fill the table with random keys and lock out real users until
  cleanup ran. An evicted key starts again with a fresh limit.
- Keyed limiters' background cleanup only visits expired keys instead of
  scanning every key while holding the lock
- Lowering `SetMaxKeys` below the current number of keys evicts the least
  recently used keys straight away

### Fixed

- Keyed limiters' `Check`/`CheckN` no longer create an entry for an unknown
  key, so read-only calls no longer use up `SetMaxKeys` slots
- `SlidingWindowCounter` and `KeyedSlidingWindow` report the earliest time a
  request fits as `RetryAfter`, instead of always the end of the window, so
  `WaitN` no longer blocks up to a full window longer than needed
- `AllowN`/`CheckN`/`TakeN`/`WaitN` now reject `n <= 0`. Negative `n` previously
  freed up capacity (e.g. `FixedWindow.AllowN(-100)` allowed 100 extra requests).
- `WaitN` returns `ErrExceedsLimit` when `n` exceeds the limiter's capacity
  instead of blocking until the context is done (forever with
  `context.Background()`)
- `Multi` no longer consumes capacity from some limiters when another denies
  the request; `Multi.WaitN` no longer consumes while it waits
- `redisstore` and `metrics` modules can now be installed: their `go.mod`
  required a non-existent root version `v0.0.0`
- `redisstore` sliding and fixed window limiters work on Redis Cluster; they
  created keys inside Lua without declaring them, which caused `CROSSSLOT`
  errors that were silently treated as "allow"
- `redisstore.RedisTokenBucket.WaitN` no longer busy-loops against Redis:
  sub-second retry times were truncated to 0
- `redisstore` windows under 1s no longer panic in `Reset`, and fractional
  windows (e.g. 1.5s) no longer make `Reset` delete the wrong key or make every
  request fail open
- `redisstore` constructors and adapters reject nil clients, including a nil
  pointer stored in the `RedisClient` interface, instead of panicking on first use
- `redisstore` limiters validate `n` like the in-memory limiters and no longer
  call Redis for invalid `n`

## [1.0.0] - 2026-02-17

Same code as 0.0.1, tagged as the first stable release.

## [0.0.1] - 2026-02-14

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

[Unreleased]: https://github.com/KARTIKrocks/go-ratelimit/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/KARTIKrocks/go-ratelimit/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/KARTIKrocks/go-ratelimit/releases/tag/v1.0.0
[0.0.1]: https://github.com/KARTIKrocks/go-ratelimit/releases/tag/v0.0.1
