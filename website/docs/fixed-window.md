---
id: fixed-window
title: Fixed Window
description: The fixed window algorithm — a counter that resets on clock-aligned window boundaries — and its boundary burst trade-off.
---

# Fixed Window

Time is divided into windows of length `window`, and at most `limit` requests
are allowed in each. The counter resets at the start of every window.

```go
// 1000 requests per hour
limiter := ratelimit.NewFixedWindow(1000, time.Hour)
```

## Windows are aligned to the clock

Windows start at multiples of `window` (the start is `time.Now().Truncate(window)`),
not at the first request, counted from the zero time in UTC. With `time.Hour`,
every window starts on the UTC hour — at :30 or :45 local time in zones with a
fractional offset — and every key resets at the same moment. That makes the limit easy to explain
("1,000 per clock hour") and easy to show in a UI, since `ResetAt` is
predictable.

## The boundary burst

A client can spend its whole limit at the end of one window and its whole
limit again at the start of the next. With 100 per minute, that is up to 200
requests in the two seconds around 12:00:00.

If that matters, use the [sliding window counter](./sliding-window.md#sliding-window-counter),
which smooths across the boundary at the same memory cost.

## Result fields

| Field | Value |
| --- | --- |
| `Limit` | `limit` |
| `Remaining` | requests left in the current window |
| `RetryAfter` | when denied, the time until the window ends |
| `ResetAt` | the end of the current window |

## Inspecting the window

```go
limiter.Count() // requests counted in the current window
```

## Per-key

```go
limiter := ratelimit.NewKeyedFixedWindow(1000, time.Hour, 10*time.Minute)
defer limiter.Close()

limiter.Allow("api-key:abc")
```

See [Keyed Limiters](./keyed-limiters.md).

## Distributed

`redisstore.NewRedisFixedWindow` implements the same algorithm with windows
aligned to Redis server time. See [Redis](./redis.md).
