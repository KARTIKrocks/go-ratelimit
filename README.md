<!-- The centred logo block opens the file, so there is no h1 on line 1. -->
<!-- markdownlint-disable-next-line MD041 -->
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="website/static/img/logo-dark.svg">
    <img src="website/static/img/logo.svg" alt="go-ratelimit" width="104" height="104">
  </picture>
</p>

<h1 align="center">go-ratelimit</h1>

<p align="center">
  A production-ready rate limiting library for Go with five algorithms,
  per-key limits, Redis-backed distributed limiters, and HTTP middleware.
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit"><img src="https://pkg.go.dev/badge/github.com/KARTIKrocks/go-ratelimit.svg" alt="Go Reference"></a>
  <a href="https://github.com/KARTIKrocks/go-ratelimit/releases"><img src="https://img.shields.io/github/v/tag/KARTIKrocks/go-ratelimit?filter=v*&amp;label=release" alt="Latest release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/KARTIKrocks/go-ratelimit" alt="Go version"></a>
  <a href="https://github.com/KARTIKrocks/go-ratelimit/actions/workflows/ci.yml"><img src="https://github.com/KARTIKrocks/go-ratelimit/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/KARTIKrocks/go-ratelimit"><img src="https://codecov.io/gh/KARTIKrocks/go-ratelimit/branch/main/graph/badge.svg" alt="Coverage"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/KARTIKrocks/go-ratelimit" alt="License: MIT"></a>
</p>

<p align="center">
  <b><a href="https://kartikrocks.github.io/go-ratelimit/">Documentation</a></b> ·
  <b><a href="https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit">API Reference</a></b> ·
  <b><a href="CHANGELOG.md">Changelog</a></b>
</p>

## Why go-ratelimit?

`golang.org/x/time/rate` gives you one token bucket. Rate limiting a real
service needs more than that:

| Capability | go-ratelimit | `x/time/rate` |
| - | - | - |
| Token bucket | ✓ | ✓ |
| Leaky bucket, fixed window, sliding window log & counter | ✓ | You build it |
| Per-key limits with idle cleanup and an LRU key cap | ✓ | You build it |
| One limit shared across instances (Redis, Cluster-safe) | ✓ | You build it |
| `net/http` middleware with `X-RateLimit-*` / `Retry-After` | ✓ | You build it |
| Proxy-aware client IP with a trusted-proxy list | ✓ | You build it |
| Composite limits (per second _and_ per hour) | ✓ | You build it |
| Prometheus metrics | ✓ | You build it |

Every limiter — in memory or in Redis — implements the same interfaces, so
changing algorithm or going distributed is a one-line change.

## Performance

```text
TokenBucket.Allow           50 ns/op    0 allocs
KeyedTokenBucket.Allow      74 ns/op    0 allocs
SlidingWindowCounter.Allow  74 ns/op    0 allocs
Contended Allow            104 ns/op    0 allocs   (12 goroutines, one bucket)
```

Every in-memory limiter decides a request without allocating. Reproduce with
`go test -bench=. -benchmem ./...` — full numbers in [Benchmarks](#benchmarks)
below.

## Features

- **Five Algorithms**: Token bucket, leaky bucket, fixed window, sliding window log, sliding window counter
- **Per-Key Limiting**: Independent limits per user, IP, or API key, with idle cleanup and an LRU cap on tracked keys
- **Distributed**: Redis-backed limiters using atomic Lua scripts and Redis server time; Redis Cluster safe
- **Fail Open or Closed**: Choose what happens when Redis is unreachable, and observe every error
- **HTTP Middleware**: `net/http` middleware with standard rate limit headers, custom responses, and skip rules
- **Proxy-Aware Keys**: Safe `RemoteAddr` default, opt-in `X-Forwarded-For` handling that only trusts listed proxies; IPv6 keyed by /64 (next release)
- **Per-Path Limits**: A strict limit on `/login`, a generous one everywhere else
- **Composite Limits**: Several limits checked before any is consumed
- **Observability**: Instrumented wrappers and Prometheus counters in the `metrics` module
- **Zero Dependencies**: The core package uses only the standard library
- **Thread Safe**: Every limiter is safe for concurrent use

## Installation

```bash
go get github.com/KARTIKrocks/go-ratelimit
```

Optional modules, each with its own dependencies:

```bash
go get github.com/KARTIKrocks/go-ratelimit/redisstore  # Redis-backed limiters
go get github.com/KARTIKrocks/go-ratelimit/metrics     # Prometheus metrics
```

## Quick Start

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // 10 requests/s per client, bursts of 20. Idle clients are cleaned up
    // every minute.
    limiter := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute)
    defer limiter.Close()

    mux := http.NewServeMux()
    mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("ok"))
    })

    handler := ratelimit.Middleware(limiter,
        ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
        ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks),
    )(mux)

    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

Without HTTP:

```go
limiter := ratelimit.NewTokenBucket(10, 20) // 10/s, burst of 20

if limiter.Allow() {
    // proceed
}

// Or block until allowed
if err := limiter.Wait(ctx); err != nil {
    return err
}
```

Sharing one limit across every instance:

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
limiter := redisstore.NewRedisTokenBucket(
    redisstore.NewRedisClientAdapter(client), "myapp", 10, 20,
    redisstore.WithErrorHandler(func(err error) { slog.Error("ratelimit", "err", err) }),
)
```

> **Note:** behind a load balancer or CDN, `RemoteAddr` is the proxy, so every
> client shares one limit. Use `TrustedProxiesKeyFunc` with your proxies'
> networks — see
> [Key Functions → Behind a reverse proxy](https://kartikrocks.github.io/go-ratelimit/docs/key-functions#behind-a-reverse-proxy).

## Documentation

Full guides live at **[kartikrocks.github.io/go-ratelimit](https://kartikrocks.github.io/go-ratelimit/)**:

| Guide | Covers |
| --- | --- |
| [Getting Started](https://kartikrocks.github.io/go-ratelimit/docs/getting-started) | Install and rate limit an HTTP server |
| [Core Concepts](https://kartikrocks.github.io/go-ratelimit/docs/concepts) | `Allow`, `Check`, `Take`, `Wait`, and `Result` |
| [Choosing an Algorithm](https://kartikrocks.github.io/go-ratelimit/docs/algorithms) | Bursts, accuracy, and memory compared |
| [Token Bucket](https://kartikrocks.github.io/go-ratelimit/docs/token-bucket) | Steady rate with a burst allowance |
| [Leaky Bucket](https://kartikrocks.github.io/go-ratelimit/docs/leaky-bucket) | Meter-style bucket and how it relates to the token bucket |
| [Fixed Window](https://kartikrocks.github.io/go-ratelimit/docs/fixed-window) | Clock-aligned windows and the boundary burst |
| [Sliding Window](https://kartikrocks.github.io/go-ratelimit/docs/sliding-window) | Exact log and constant-memory counter |
| [Keyed Limiters](https://kartikrocks.github.io/go-ratelimit/docs/keyed-limiters) | Per-key limits, cleanup, and the LRU key cap |
| [Composite Limits](https://kartikrocks.github.io/go-ratelimit/docs/multi) | Tiered limits that consume only when all allow |
| [HTTP Middleware](https://kartikrocks.github.io/go-ratelimit/docs/middleware) | Options, headers, responses, and skip rules |
| [Key Functions](https://kartikrocks.github.io/go-ratelimit/docs/key-functions) | Client IP, proxies, API keys, and users |
| [Path Limits](https://kartikrocks.github.io/go-ratelimit/docs/path-limits) | Different limits for different routes |
| [Redis](https://kartikrocks.github.io/go-ratelimit/docs/redis) | Distributed limits, Cluster, and failure modes |
| [Metrics](https://kartikrocks.github.io/go-ratelimit/docs/metrics) | Instrumented wrappers and Prometheus |
| [Errors](https://kartikrocks.github.io/go-ratelimit/docs/errors) | Sentinel errors and invalid request sizes |

Exact type signatures are generated from source on
[pkg.go.dev](https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit).

Runnable programs are in [`examples/`](examples/) — basic, multitier, redis,
and prometheus.

## Benchmarks

Measured on an Intel i5-11400H @ 2.70GHz (12 cores), Go 1.27, Linux; median of
three runs.

```bash
go test -bench=. -benchmem ./...
```

### Single limiters

| Benchmark | Single goroutine | 12 goroutines, one limiter | Allocs |
| --- | --- | --- | --- |
| `TokenBucket.Allow` | 49.7 ns/op | 103.5 ns/op | 0 |
| `LeakyBucket.Allow` | 50.8 ns/op | 103.4 ns/op | 0 |
| `FixedWindow.Allow` | 52.7 ns/op | 105.3 ns/op | 0 |
| `SlidingWindow.Allow` (log) | 46.7 ns/op | — | 0 |
| `SlidingWindowCounter.Allow` | 73.8 ns/op | 125.0 ns/op | 0 |

### Keyed limiters

| Benchmark | ns/op | Allocs |
| --- | --- | --- |
| `KeyedTokenBucket.Allow` | 74.1 | 0 |
| `KeyedTokenBucket.Allow`, 12 goroutines | 143.3 | 0 |
| `KeyedTokenBucket.Allow`, many keys | 73.6 | 0 |
| `KeyedLeakyBucket.Allow` | 74.6 | 0 |
| `KeyedFixedWindow.Allow` | 81.4 | 0 |
| `KeyedSlidingWindow.Allow` | 101.8 | 0 |

### HTTP middleware

| Benchmark | ns/op | B/op | Allocs |
| --- | --- | --- | --- |
| `Middleware`, full request through `ServeHTTP` | ~1,100 | 1,056 | 14 |

The middleware figure includes allocating an `httptest.ResponseRecorder` and
its header map for every request, which a real server does in `net/http`
regardless of rate limiting.

## Thread Safety

Every limiter is safe for concurrent use. The in-memory limiters guard their
state with a mutex; the Redis limiters run each decision as one atomic Lua
script, so instances never race each other.

## Security

A rate limiter is a security control, so its defaults are chosen to be safe
rather than convenient:

- **Client IPs come from `RemoteAddr` by default.** Proxy headers are opt-in,
  and `TrustedProxiesKeyFunc` only honours them on requests that actually come
  from a proxy you listed.
- **IPv6 clients are keyed by their /64 network** (next release; v1.1.0 keys
  each address separately), so a client cannot rotate through its own address
  block to reset its limit.
- **Key-set growth can be bounded** with `SetMaxKeys`, so a flood of new keys
  cannot exhaust memory. It is unlimited by default; set it on any limiter keyed
  by client-controlled values.
- **Redis failures are observable**, and can fail closed for limits that guard
  against abuse.

Every push and pull request to `main` is scanned by
[CodeQL](https://github.com/KARTIKrocks/go-ratelimit/actions/workflows/codeql.yml),
and `govulncheck` gates every merge on advisories reachable from this code's
call graph — run separately against the root module, `redisstore`, and
`metrics`, because a scan started from the root stops at nested `go.mod`
boundaries.

See [SECURITY.md](SECURITY.md) for supported versions and how to report a
vulnerability privately.

## License

[MIT](LICENSE)

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Credits

Inspired by [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate),
[Stripe's rate limiter design](https://stripe.com/blog/rate-limiters), and the
[token bucket algorithm](https://en.wikipedia.org/wiki/Token_bucket).
