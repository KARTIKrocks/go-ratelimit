---
id: middleware
title: HTTP Middleware
description: net/http middleware for keyed and global limiters — options, rate limit response headers, custom 429 responses, skip rules, and the waiting variant.
---

# HTTP Middleware

The middleware works with plain `net/http`, so it fits any router that accepts
`func(http.Handler) http.Handler` — the standard `ServeMux`, chi, gorilla/mux,
and most others.

## Per-client limits

`Middleware` takes a [keyed limiter](./keyed-limiters.md) and derives each
request's key with a [key function](./key-functions.md) — by default
`IPKeyFunc`, the client address from `RemoteAddr`:

```go
limiter := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute)
defer limiter.Close()

handler := ratelimit.Middleware(limiter,
    ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
    ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
)(mux)
```

For a single handler, `Handler` and `HandlerFunc` apply the same middleware
in one call:

```go
http.Handle("/api", ratelimit.Handler(apiHandler, limiter))
http.HandleFunc("/upload", ratelimit.HandlerFunc(upload, uploadLimiter))
```

## One global limit

`MiddlewareFunc` takes a single (non-keyed) limiter, so every request shares
one limit — useful to protect a downstream dependency with a fixed capacity:

```go
global := ratelimit.NewTokenBucket(500, 100)
handler := ratelimit.MiddlewareFunc(global)(mux)
```

Key functions do not apply here; `WithKeyFunc` is ignored.

## Options

| Option | Default | Effect |
| --- | --- | --- |
| `WithKeyFunc(fn)` | `IPKeyFunc` | how a request maps to a key — see [Key Functions](./key-functions.md) |
| `WithOnLimitReached(fn)` | plain-text error | writes the response for a rejected request |
| `WithStatusCode(code)` | `429` | status used by the default rejection response |
| `WithHeaders(enabled)` | `true` | whether to set the rate limit headers |
| `WithSkipFunc(fn)` | none | requests for which `fn` returns true bypass the limiter |

## Response headers

With headers enabled, every response that passes through the limiter gets:

| Header | Value | When |
| --- | --- | --- |
| `X-RateLimit-Limit` | `Result.Limit` | always |
| `X-RateLimit-Remaining` | `Result.Remaining` | always |
| `X-RateLimit-Reset` | `Result.ResetAt` as Unix seconds | when the algorithm has a window |
| `Retry-After` | `Result.RetryAfter`, rounded up to whole seconds | when rejected |

Token and leaky buckets refill continuously and have no reset time, so they do
not send `X-RateLimit-Reset`.

To set the same headers from your own handler — after calling `Take` yourself,
for example — use `AddRateLimitHeaders(w, result)`.

## Rejection responses

Without `WithOnLimitReached`, a rejected request gets a plain-text
`Rate limit exceeded` with the status from `WithStatusCode` (429 by default).

The package ships two JSON alternatives:

```go
// 429 with a JSON body
ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached)

// The same body with a different status
ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReachedWithCode(http.StatusServiceUnavailable))
```

Both write:

```json
{"error":"rate_limit_exceeded","message":"Too many requests","retry_after":3}
```

For anything else, pass your own `OnLimitReached`. It receives the `Result`, so
it can report whatever the client needs:

```go
ratelimit.WithOnLimitReached(func(w http.ResponseWriter, r *http.Request, res ratelimit.Result) {
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(http.StatusTooManyRequests)
    fmt.Fprintf(w, `{"title":"Slow down","retry_after":%d}`,
        int(math.Ceil(res.RetryAfter.Seconds())))
})
```

:::note WithStatusCode only affects the default response

A custom `OnLimitReached` writes its own status. The exported
`DefaultOnLimitReached` also always uses 429; to change the status of the
plain-text response, use `WithStatusCode` *without* `WithOnLimitReached`.

:::

## Skipping requests

`WithSkipFunc` lets some requests through without touching the limiter. The
package provides the common cases:

| Skip function | Skips |
| --- | --- |
| `SkipHealthChecks` | `/health`, `/healthz`, `/ready`, `/readyz`, `/live`, `/livez`, `/ping` |
| `SkipPaths(paths...)` | exact path matches |
| `SkipMethods(methods...)` | the listed HTTP methods, e.g. `OPTIONS` |
| `SkipPrivateIPs` | loopback and private-range client addresses (from `RemoteAddr`) |
| `SkipIf(fns...)` | any request matched by one of `fns` |

```go
ratelimit.WithSkipFunc(ratelimit.SkipIf(
    ratelimit.SkipHealthChecks,
    ratelimit.SkipMethods(http.MethodOptions),
    ratelimit.SkipPaths("/metrics"),
))
```

Skipped requests get no rate limit headers.

## Waiting instead of rejecting

`WaitMiddleware` holds a request until the limiter allows it, up to `timeout`,
and only rejects if the wait fails:

```go
handler := ratelimit.WaitMiddleware(limiter, 2*time.Second,
    ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
)(mux)
```

It smooths short spikes from trusted callers — internal services, webhooks you
send yourself — at the cost of holding a goroutine and a connection per
waiting request. Do not put it in front of untrusted traffic, where it turns a
flood into a pile of parked requests.

The wait also ends if the client disconnects, since it uses the request's
context. A rejected request reaches `OnLimitReached` with an empty `Result`, so
`WaitMiddleware` sets no rate limit headers. A `timeout` of zero waits as long
as the request context allows.

## Different limits per route

To give `/login` a stricter limit than the rest of the API, see
[Path Limits](./path-limits.md).
