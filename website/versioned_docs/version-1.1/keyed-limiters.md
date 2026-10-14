---
id: keyed-limiters
title: Keyed Limiters
description: Per-key rate limiting with KeyedTokenBucket, KeyedLeakyBucket, KeyedFixedWindow and KeyedSlidingWindow — idle cleanup, the LRU key cap, and shutting down.
---

# Keyed Limiters

A keyed limiter keeps an independent limit for every key you pass it — a user
ID, an IP address, an API key, or any string you build. Each in-memory
algorithm has a keyed form:

| Constructor | Algorithm |
| --- | --- |
| `NewKeyedTokenBucket(rate, burst, cleanupInterval)` | [token bucket](./token-bucket.md) |
| `NewKeyedTokenBucketPerDuration(count, per, burst, cleanupInterval)` | token bucket |
| `NewKeyedLeakyBucket(rate, capacity, cleanupInterval)` | [leaky bucket](./leaky-bucket.md) |
| `NewKeyedLeakyBucketPerDuration(count, per, capacity, cleanupInterval)` | leaky bucket |
| `NewKeyedFixedWindow(limit, window, cleanupInterval)` | [fixed window](./fixed-window.md) |
| `NewKeyedSlidingWindow(limit, window, cleanupInterval)` | [sliding window counter](./sliding-window.md#sliding-window-counter) |

```go
limiter := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute)
defer limiter.Close()

limiter.Allow("user:123")       // independent of...
limiter.Allow("user:456")       // ...this one
limiter.Allow("ip:192.0.2.10")
```

Keys are opaque strings. Namespace them (`user:`, `ip:`) when one limiter
serves several kinds of key, so a user ID can never collide with an address.

## Memory: cleanup and the key cap

Every key the limiter sees costs a small entry. Two mechanisms keep that bounded.

### Idle cleanup

If `cleanupInterval` is positive, a background goroutine runs every
`cleanupInterval` and removes keys that have not been used for **twice** that
long. A removed key starts fresh the next time it appears. That is harmless
only if twice the interval covers a full recovery — `burst / rate` for the
buckets, the window length for the windows. A 1-hour window with a 1-minute
cleanup interval would reset clients after 2 idle minutes, so pick an interval
of at least half the recovery time.

Pass `0` to disable the goroutine. Without it, keys are only removed by the
key cap or `Reset`.

### Key cap

`SetMaxKeys` bounds how many keys are tracked at once. When the limiter is
full, the **least recently used** key is evicted to make room:

```go
limiter := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute).SetMaxKeys(100_000)
```

It returns the limiter, so it chains off the constructor. Zero means
unlimited, which is the default. Lowering the cap below the current number of
keys evicts the excess immediately.

:::warning Size the cap well above your active key count

An evicted key starts again with a fresh allowance. If an attacker can create
keys faster than real clients use theirs — rotating source addresses, or
inventing API keys that reach the limiter before authentication — they can push
a legitimate client's key out and reset its limit. Set the cap comfortably
above the number of keys active at once, and key on something an attacker
cannot mint for free.

:::

Eviction and cleanup touch only the oldest entries, so neither scans the whole
key set.

### Watching the size

```go
limiter.Len() // number of keys currently tracked
```

## Shutdown

`Close` stops the cleanup goroutine. Call it when the limiter is no longer
needed, typically with `defer`:

```go
limiter := ratelimit.NewKeyedFixedWindow(1000, time.Hour, 10*time.Minute)
defer limiter.Close()
```

`Close` is safe to call more than once. Every keyed limiter (and
`MemoryStore`) implements the `Closer` interface, so shutdown code can close
limiters without knowing their concrete type:

```go
if c, ok := limiter.(ratelimit.Closer); ok {
    c.Close()
}
```

## Resetting

```go
limiter.Reset("user:123") // forget one key
limiter.ResetAll()        // forget every key
```
