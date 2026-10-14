---
id: token-bucket
title: Token Bucket
description: The token bucket algorithm — a steady refill rate with a burst allowance — and the TokenBucket and KeyedTokenBucket limiters.
---

# Token Bucket

A bucket holds up to `burst` tokens and refills at `rate` tokens per second.
Each request spends one token (or `n` for `AllowN`). When the bucket is empty,
requests are denied until it refills.

The bucket **starts full**, so a new client can immediately make `burst`
requests; after that it is held to `rate`.

```go
// 10 requests per second, bursts of up to 20
limiter := ratelimit.NewTokenBucket(10, 20)

// The same rate, written per duration
limiter = ratelimit.NewTokenBucketPerDuration(600, time.Minute, 20)
```

## Choosing rate and burst

- **`rate`** is the long-run limit. Over any long stretch, a client gets at
  most `rate` requests per second.
- **`burst`** is how far ahead of that rate a client may get. It is also the
  largest `n` a single `AllowN` can ever succeed with.

A `burst` of 1 gives strict spacing: one request every `1/rate` seconds and no
bursts at all. A large `burst` is generous to bursty-but-honest clients.

## Result fields

| Field | Value |
| --- | --- |
| `Limit` | `burst` |
| `Remaining` | whole tokens left after the decision |
| `RetryAfter` | when denied, the time until enough tokens have refilled |
| `ResetAt` | always zero — a bucket refills continuously and has no window |

## Inspecting the bucket

```go
limiter.Tokens() // float64 — current tokens, after refilling
limiter.Rate()   // tokens per second
limiter.Burst()  // capacity
```

## Per-key

```go
limiter := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute)
defer limiter.Close()

limiter.Allow("user:42")
```

`NewKeyedTokenBucketPerDuration(count, per, burst, cleanupInterval)` is also
available. Cleanup, the key cap, and `Close` work the same for every keyed
limiter — see [Keyed Limiters](./keyed-limiters.md).

## Distributed

`redisstore.NewRedisTokenBucket` runs the same algorithm in Redis, with the
same arguments plus a client and key prefix. See [Redis](./redis.md).
