---
id: errors
title: Errors
description: Sentinel errors returned by go-ratelimit, how invalid request sizes are reported, constructor panics, and Redis error handling.
---

# Errors

Rate limiting decisions are not errors: a denied request is a normal `false`
or a `Result` with `Allowed: false`. Errors are reserved for requests that can
never succeed and for infrastructure failures.

## Sentinel errors

| Error | Returned by | Meaning |
| --- | --- | --- |
| `ErrInvalidN` | `WaitN` | `n` is zero or negative |
| `ErrExceedsLimit` | `WaitN` | `n` is larger than the limiter's capacity, so it can never be allowed |
| `ErrRateLimitExceeded` | nothing in this package | a ready-made error for your own code to return when a limit is hit |
| `redisstore.ErrResetAllUnsupported` | the Redis error handler | `ResetAll` was called on a client that does not implement `KeyDeleter` |

Match them with `errors.Is`:

```go
if err := limiter.WaitN(ctx, key, n); err != nil {
    switch {
    case errors.Is(err, ratelimit.ErrExceedsLimit):
        return fmt.Errorf("batch of %d is larger than the limit allows", n)
    case errors.Is(err, context.DeadlineExceeded):
        return errTooBusy
    default:
        return err
    }
}
```

`Wait` and `WaitN` otherwise return `ctx.Err()` when the context ends first.

## Invalid sizes without an error

The non-waiting methods have no error return, so they report an impossible `n`
through their normal results:

| Method | For `n <= 0` or `n` above capacity |
| --- | --- |
| `Allow`, `AllowN` | `false` |
| `Check*`, `Take*` | `Result{Allowed: false, Limit: <capacity>}` with a zero `RetryAfter` |

A denied `Result` with a zero `RetryAfter` means "waiting will not help". If
you accept `n` from a caller, validate it against the limit before asking the
limiter.

## Constructor panics

Constructors panic on arguments that cannot describe a limiter, with a message
prefixed `ratelimit:`:

- a zero or negative rate, burst, capacity, limit, or window,
- a zero or negative `per` duration in the `...PerDuration` constructors,
- a nil Redis client, or a Redis window under 1ms,
- an unparseable CIDR passed to `TrustedProxiesKeyFunc`.

These come from configuration, so they surface at startup rather than on a
request. If limits come from user-editable config, validate them before
constructing.

## Redis failures

The Redis limiters never return Redis errors from `Allow`, `Check`, or `Take`.
They report them to the `WithErrorHandler` callback and then fail open (allow)
or, with `WithFailClosed`, deny. `WaitN` returns the error when failing closed.
See [When Redis fails](./redis.md#when-redis-fails).
