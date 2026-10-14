---
id: intro
title: Overview
sidebar_label: Overview
description: A production-ready rate limiting library for Go with five algorithms, per-key limits, Redis-backed distributed limiters, and HTTP middleware.
slug: /
---

# go-ratelimit

A production-ready rate limiting library for Go with multiple algorithms,
distributed support, and HTTP middleware.

```bash
go get github.com/KARTIKrocks/go-ratelimit
```

## What you get

| | |
| --- | --- |
| **Five algorithms** | Token bucket, leaky bucket, fixed window, sliding window log, sliding window counter |
| **Per-key limiting** | Independent limits per user, IP, or API key, with idle cleanup and an LRU key cap |
| **Distributed** | Redis-backed limiters that share one limit across every instance |
| **HTTP middleware** | `net/http` middleware with `X-RateLimit-*` and `Retry-After` headers |
| **Proxy-aware keys** | Safe `RemoteAddr` default, opt-in proxy header handling with a trusted-proxy list |
| **Composite limits** | Several limits checked together, consuming from none unless all allow |
| **Observability** | Instrumented wrappers and Prometheus counters in the `metrics` module |
| **Zero dependencies** | The core package uses only the standard library |
| **Thread safe** | Every limiter is safe for concurrent use |

## Modules

The repository holds three Go modules, so you only pull in the dependencies you
use:

| Module | Import path | Depends on |
| --- | --- | --- |
| Core | `github.com/KARTIKrocks/go-ratelimit` | standard library only |
| Redis | `github.com/KARTIKrocks/go-ratelimit/redisstore` | `github.com/redis/go-redis/v9` |
| Metrics | `github.com/KARTIKrocks/go-ratelimit/metrics` | `github.com/prometheus/client_golang` |

## Where to go next

- **[Getting Started](./getting-started.md)** — install and rate limit an HTTP server
- **[Core Concepts](./concepts.md)** — the interfaces every limiter shares
- **[Choosing an Algorithm](./algorithms.md)** — which limiter fits which job
- **[Redis](./redis.md)** — one limit across many instances
- **[API Reference](https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit)** —
  full generated godoc on pkg.go.dev

## Documentation layout

These guides explain concepts, patterns, and configuration. For exact type
signatures, method sets, and struct fields, use
[pkg.go.dev](https://pkg.go.dev/github.com/KARTIKrocks/go-ratelimit) — it is
generated from the source and is always authoritative.

## Requirements

Go 1.27 or later.
