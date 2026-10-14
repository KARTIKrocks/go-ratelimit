---
id: leaky-bucket
title: Leaky Bucket
description: The leaky bucket algorithm as a meter — a bucket that drains at a fixed rate and rejects requests that would overflow it — and how it relates to the token bucket.
---

# Leaky Bucket

A bucket of size `capacity` drains ("leaks") at `rate` units per second. Each
request pours one unit in (or `n` for `AllowN`). A request that would overflow
the bucket is denied.

```go
// Drains 10 per second, holds up to 50
limiter := ratelimit.NewLeakyBucket(10, 50)

// The same rate, written per duration
limiter = ratelimit.NewLeakyBucketPerDuration(600, time.Minute, 50)
```

The bucket **starts empty**, so the first `capacity` requests are accepted at
once.

:::note A meter, not a queue

This is the *leaky bucket as a meter*. Denied requests are rejected
immediately; nothing is queued and nothing is delayed. If you want a caller to
wait for room, use `Wait`, which blocks until the request fits.

:::

## Relation to the token bucket

Measured from the other side, the water level of a leaky bucket is the number
of tokens a token bucket is missing. With the same rate and size, the two make
identical decisions; the differences are presentational:

| | Token bucket | Leaky bucket |
| --- | --- | --- |
| Counts | tokens available | water used |
| Starts | full of tokens | empty of water |
| Size argument | `burst` | `capacity` |
| Inspect with | `Tokens()` | `Water()` |
| Redis implementation | ✓ | — |

Choose whichever models your problem more naturally. If you might move to
Redis later, choose the token bucket.

## Result fields

| Field | Value |
| --- | --- |
| `Limit` | `capacity` |
| `Remaining` | room left in the bucket after the decision |
| `RetryAfter` | when denied, the time until enough has drained |
| `ResetAt` | always zero |

## Inspecting the bucket

```go
limiter.Water()    // float64 — current level, after draining
limiter.Rate()     // drain rate per second
limiter.Capacity() // size
```

## Per-key

```go
limiter := ratelimit.NewKeyedLeakyBucket(10, 50, time.Minute)
defer limiter.Close()

limiter.Allow("user:42")
```

`NewKeyedLeakyBucketPerDuration(count, per, capacity, cleanupInterval)` is also
available. See [Keyed Limiters](./keyed-limiters.md).
