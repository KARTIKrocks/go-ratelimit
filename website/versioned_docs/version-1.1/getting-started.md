---
id: getting-started
title: Getting Started
description: Install go-ratelimit, limit a function call, and put per-client rate limiting in front of an HTTP server.
---

# Getting Started

## Installation

Requires **Go 1.27+**.

```bash
go get github.com/KARTIKrocks/go-ratelimit
```

The Redis and Prometheus integrations are separate modules, installed only if
you need them:

```bash
go get github.com/KARTIKrocks/go-ratelimit/redisstore
go get github.com/KARTIKrocks/go-ratelimit/metrics
```

## Limit a single resource

A token bucket allows a steady rate with short bursts. This one refills at 10
tokens per second and holds at most 20:

```go
package main

import (
    "fmt"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    limiter := ratelimit.NewTokenBucket(10, 20) // 10/s, burst of 20

    for i := range 25 {
        if limiter.Allow() {
            fmt.Println(i, "allowed")
        } else {
            fmt.Println(i, "rate limited")
        }
    }
}
```

The bucket starts full, so the first 20 calls succeed immediately and the rest
are denied until tokens refill.

To block until a request fits instead of rejecting it, use `Wait`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

if err := limiter.Wait(ctx); err != nil {
    return err // context cancelled or deadline exceeded
}
```

## Rate limit an HTTP server

Most servers want a separate limit per client. A keyed limiter tracks one
bucket per key, and `Middleware` derives the key from each request — by
default, the client IP from `RemoteAddr`:

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/KARTIKrocks/go-ratelimit"
)

func main() {
    // 10 requests/s per client, bursts of 20. Idle clients are removed by a
    // background cleanup that runs every minute.
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

Every response carries `X-RateLimit-Limit` and `X-RateLimit-Remaining`. A
rejected request gets a `429 Too Many Requests` with a `Retry-After` header and
a JSON body:

```json
{"error":"rate_limit_exceeded","message":"Too many requests","retry_after":1}
```

:::warning Behind a load balancer?

`RemoteAddr` is the address of whatever connected to your server. Behind a
reverse proxy every request appears to come from the proxy, so every client
shares one limit. See [Key Functions](./key-functions.md#behind-a-reverse-proxy)
before deploying behind one.

:::

## Next steps

- [Core Concepts](./concepts.md) — `Allow`, `Check`, `Take`, `Wait`, and `Result`
- [Choosing an Algorithm](./algorithms.md) — when a token bucket is the wrong fit
- [HTTP Middleware](./middleware.md) — headers, responses, and skip rules
- [Redis](./redis.md) — sharing a limit across instances

Runnable programs are in
[`examples/`](https://github.com/KARTIKrocks/go-ratelimit/tree/main/examples) —
basic, multitier, redis, and prometheus.
