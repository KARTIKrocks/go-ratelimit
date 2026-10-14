---
id: sliding-window
title: Sliding Window
description: The sliding window log (exact, one timestamp per request) and sliding window counter (weighted approximation, constant memory) algorithms.
---

# Sliding Window

Both sliding window algorithms aim for "at most `limit` requests in any
`window`-long stretch", without the [fixed window](./fixed-window.md)'s
boundary burst. The log enforces it exactly; the counter estimates it, and can
admit somewhat more than `limit` when traffic clusters around a window
boundary.

## Sliding window log

`SlidingWindow` records the timestamp of every allowed request and counts those
younger than `window`. It is exact: there is no approximation and no boundary
effect.

```go
// At most 5 attempts in any 15 minutes
limiter := ratelimit.NewSlidingWindow(5, 15*time.Minute)
```

The cost is memory proportional to `limit`, since it stores up to `limit`
timestamps. That is the right trade for low limits where exactness matters —
login attempts, password resets, OTP sends — and the wrong one for limits in
the thousands.

The log has no keyed or Redis variant. For a per-key sliding window, use the
counter below.

| Field | Value |
| --- | --- |
| `Limit` | `limit` |
| `Remaining` | requests left in the trailing window |
| `RetryAfter` | when denied, the time until the oldest request leaves the window |
| `ResetAt` | when denied, the same moment as an absolute time; zero when allowed |

`limiter.Count()` returns the number of requests currently in the window.

## Sliding window counter

`SlidingWindowCounter` keeps only two numbers: the count for the current
fixed window and the count for the previous one. It estimates the trailing
window by weighting the previous count by how much of it still overlaps:

```text
estimate = previous × (1 − elapsed/window) + current
```

Twenty seconds into a one-minute window, a third of the way through, two
thirds of the previous minute's requests still count.

```go
// About 100 requests in any minute
limiter := ratelimit.NewSlidingWindowCounter(100, time.Minute)
```

The estimate assumes the previous window's requests were evenly spread. It is
never off by more than the previous window's count, and in practice it is
close — this is the algorithm Cloudflare and many API gateways use for exactly
that reason. Memory is constant per key.

| Field | Value |
| --- | --- |
| `Limit` | `limit` |
| `Remaining` | `limit` minus the weighted estimate |
| `RetryAfter` | when denied, the computed time until the estimate leaves room |
| `ResetAt` | the end of the current underlying window |

## Per-key

The keyed sliding window uses the counter algorithm:

```go
limiter := ratelimit.NewKeyedSlidingWindow(100, time.Minute, 5*time.Minute)
defer limiter.Close()

limiter.Allow("user:42")
```

See [Keyed Limiters](./keyed-limiters.md).

## Distributed

`redisstore.NewRedisSlidingWindow` implements the counter algorithm in Redis.
See [Redis](./redis.md).
