---
id: concepts
title: Core Concepts
description: The Limiter, ResultLimiter, KeyedLimiter and KeyedResultLimiter interfaces, the Result type, and the difference between Allow, Check, Take, and Wait.
---

# Core Concepts

Every limiter in the package — in memory or Redis-backed — implements the same
small set of interfaces. Learn them once and every algorithm works the same
way.

## The four interfaces

| Interface | For | Methods |
| --- | --- | --- |
| `Limiter` | one shared limit | `Allow`, `AllowN`, `Wait`, `WaitN`, `Reset` |
| `ResultLimiter` | one shared limit, with detail | `Check`, `CheckN`, `Take`, `TakeN` |
| `KeyedLimiter` | a limit per key | `Allow`, `AllowN`, `Wait`, `WaitN`, `Reset`, `ResetAll` — each taking a key |
| `KeyedResultLimiter` | a limit per key, with detail | `Check`, `CheckN`, `Take`, `TakeN` — each taking a key |

Every single-limit type (`TokenBucket`, `LeakyBucket`, `FixedWindow`,
`SlidingWindow`, `SlidingWindowCounter`) implements both `Limiter` and
`ResultLimiter`. Every keyed type (`KeyedTokenBucket`, `KeyedLeakyBucket`,
`KeyedFixedWindow`, `KeyedSlidingWindow`, and the three Redis limiters)
implements both `KeyedLimiter` and `KeyedResultLimiter`.

Accept the narrowest interface your code needs. The HTTP middleware, for
example, takes a `KeyedResultLimiter`, so it works with any keyed limiter and
with the [metrics wrapper](./metrics.md).

## Allow, Check, Take, Wait

| Method | Consumes? | Blocks? | Returns |
| --- | --- | --- | --- |
| `Allow` / `AllowN` | if allowed | no | `bool` |
| `Take` / `TakeN` | if allowed | no | `Result` |
| `Check` / `CheckN` | never | no | `Result` |
| `Wait` / `WaitN` | yes | until allowed or `ctx` is done | `error` |

`Allow` and `Take` make the same decision; `Take` also tells you why. `Check`
answers "would this be allowed right now?" without spending anything — useful
for showing a quota in a UI, or for checking several limiters before committing
to any of them, which is exactly what [`Multi`](./multi.md) does.

The `N` variants consume `n` units at once, for operations that cost more than
one request — a bulk API call that counts as 50, for example:

```go
if !limiter.AllowN("user:42", 50) {
    return errQuotaExceeded
}
```

On a keyed limiter, `Check` and `CheckN` never create state for an unknown key:
checking a key nobody has used reports a full allowance and leaves the limiter
unchanged.

## Result

`Check` and `Take` return a `Result`:

```go
type Result struct {
    Allowed    bool          // whether the request is allowed
    Limit      int           // maximum requests allowed (burst, capacity, or window limit)
    Remaining  int           // requests left right now
    RetryAfter time.Duration // when denied, how long until it would be allowed
    ResetAt    time.Time     // when the current window resets; zero if the algorithm has no window
}
```

`RetryAfter` is only set when the request is denied. `ResetAt` is set by the
window-based algorithms (fixed window, sliding window counter, and the sliding
window log when it denies); the bucket algorithms refill continuously, so they
leave it zero. The HTTP middleware turns these fields into response headers —
see [Response headers](./middleware.md#response-headers).

## Waiting

`Wait` and `WaitN` sleep for the limiter's own estimate of when the request
will fit, then try again, until they succeed or the context ends:

```go
ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

if err := limiter.Wait(ctx, "user:42"); err != nil {
    return err // ctx.Err(), a validation error for n, or a Redis error when failing closed
}
```

Always pass a context with a deadline when the wait is on a request path. A
`Wait` with `context.Background()` on a heavily limited key can block for as
long as the limit takes to recover.

## Requests that can never fit

A request for more units than the limiter can ever hold — `AllowN(100)` on a
bucket with a burst of 20 — can never succeed, so the limiters do not pretend
it might:

- `Allow*` returns `false`.
- `Check*` and `Take*` return a denied `Result` with `Limit` set and a zero
  `RetryAfter`, because waiting will not help.
- `Wait*` returns `ErrExceedsLimit` immediately instead of blocking forever.

`n <= 0` is handled the same way, with `ErrInvalidN` from `Wait*`. See
[Errors](./errors.md).

## Reset

`Reset` returns a limiter to its starting state — a full bucket, or an empty
window. On a keyed limiter, `Reset(key)` forgets one key and `ResetAll()`
forgets every key. The Redis limiters can only reset all keys when their client
implements `KeyDeleter`; otherwise `ResetAll` reports `ErrResetAllUnsupported` to
the error handler and deletes nothing. See [Resetting](./redis.md#resetting).

## Thread safety

Every limiter is safe for concurrent use from any number of goroutines. The
in-memory limiters guard their state with a mutex; the Redis limiters run each
decision as a single atomic Lua script.
