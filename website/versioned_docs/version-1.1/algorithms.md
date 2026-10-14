---
id: algorithms
title: Choosing an Algorithm
description: How the five rate limiting algorithms compare on bursts, accuracy, and memory, and which one to use for APIs, background work, and distributed systems.
---

# Choosing an Algorithm

All five algorithms implement the [same interfaces](./concepts.md), so changing
your mind later is a one-line change. The choice comes down to how much burst
you will tolerate and how much state you are willing to keep.

## At a glance

| Algorithm | Bursts | Memory per key | Keyed | Redis |
| --- | --- | --- | --- | --- |
| [Token bucket](./token-bucket.md) | up to the burst size, then a steady rate | constant | ✓ | ✓ |
| [Leaky bucket](./leaky-bucket.md) | up to the capacity, then a steady rate | constant | ✓ | — |
| [Fixed window](./fixed-window.md) | up to 2× the limit across a window boundary | constant | ✓ | ✓ |
| [Sliding window log](./sliding-window.md#sliding-window-log) | up to the limit at once, never more than the limit in any window | one timestamp per request in the window | — | — |
| [Sliding window counter](./sliding-window.md#sliding-window-counter) | smoothed approximation | constant | ✓ | ✓ |

## Recommendations

**Start with the token bucket.** It is the right default for public APIs: it
lets a well-behaved client make a quick burst of calls (a page load fetching
ten resources) while holding the long-run rate to what you set. It is what
`golang.org/x/time/rate` and most API gateways use.

**Use a window when the limit is a quota.** "1,000 requests per hour" is a
window, not a rate. The sliding window counter enforces it without the fixed
window's boundary problem and keeps constant memory per key, so it is the
better default of the two. Pick the fixed window when you want windows aligned
to the clock — the limit resets on the hour — and the boundary burst is
acceptable.

**Use the sliding window log when exactness matters more than memory.** It is
the only algorithm with no approximation and no boundary effect, at the cost of
storing every request in the window. That is fine for low limits — 5 login
attempts per 15 minutes — and wasteful for high ones.

**Use the leaky bucket when you think in fill levels.** It makes the same
decisions as a token bucket with the same rate and size; the difference is
which way it counts and that it starts empty. See
[how they relate](./leaky-bucket.md#relation-to-the-token-bucket).

**Going distributed?** The Redis store implements the token bucket, fixed
window, and sliding window counter. See [Redis](./redis.md).

## Constructors

Every algorithm has a plain constructor, and the rate-based ones also take a
count per duration, which is usually easier to read:

```go
ratelimit.NewTokenBucket(100.0/60.0, 10)                      // 100/min, burst 10
ratelimit.NewTokenBucketPerDuration(100, time.Minute, 10)     // same thing

ratelimit.NewLeakyBucket(10, 50)                              // 10/s, capacity 50
ratelimit.NewLeakyBucketPerDuration(600, time.Minute, 50)     // same thing

ratelimit.NewFixedWindow(1000, time.Hour)
ratelimit.NewSlidingWindow(5, 15*time.Minute)                 // log
ratelimit.NewSlidingWindowCounter(100, time.Minute)           // counter
```

Each except the sliding window log has a keyed counterpart that takes an
extra cleanup interval — see
[Keyed Limiters](./keyed-limiters.md).

Constructors panic on arguments that cannot form a valid limiter: a rate,
burst, capacity, limit, or window that is zero or negative. These are
programming errors caught at startup, not runtime conditions.
