---
id: path-limits
title: Path Limits
description: Give different routes different rate limits with PathLimiter — a strict limit on login, a generous one on reads, and a fallback for everything else.
---

# Path Limits

Not every endpoint deserves the same limit. A login endpoint should allow a
handful of attempts; a read-only listing can allow hundreds. `PathLimiter`
picks a limiter by request path, with a fallback for everything else:

```go
general := ratelimit.NewKeyedTokenBucket(100.0/60.0, 10, time.Minute)
login := ratelimit.NewKeyedSlidingWindow(5, 15*time.Minute, time.Minute)
search := ratelimit.NewKeyedTokenBucket(1, 3, time.Minute)
defer general.Close()
defer login.Close()
defer search.Close()

paths := ratelimit.NewPathLimiter(general).
    Add("/api/login", login).
    Add("/api/search", search)

handler := paths.Middleware(
    ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
)(mux)
```

`Middleware` takes the same [options](./middleware.md#options) as the regular
middleware: key function, rejection response, headers, and skip rules.

## How paths match

Paths match **exactly** against `r.URL.Path`. `/api/login` does not match
`/api/login/` or `/api/login/sso`. There is no prefix or pattern matching —
anything not listed uses the fallback.

Each listed path has its own limiter, so a client's login attempts do not use
up its search allowance. Paths that fall through to the fallback share it.

`Add` is meant for setup. Do not call it while the middleware is serving
requests.

## Alternatives

**Route-level middleware.** If your router supports per-route middleware, wrap
just the routes that need a different limit. It handles path parameters, which
`PathLimiter`'s exact matching cannot:

```go
mux.Handle("POST /api/login", ratelimit.Middleware(login)(loginHandler))
```

**Key by path.** To give each client a separate limit on every endpoint using
one limiter, key on IP and path together:

```go
ratelimit.Middleware(limiter, ratelimit.WithKeyFunc(ratelimit.IPPathKeyFunc))
```
