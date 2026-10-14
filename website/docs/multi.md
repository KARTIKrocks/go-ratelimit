---
id: multi
title: Composite Limits
description: Combine several limiters with Multi so a request must pass all of them — per-second, per-minute and per-hour limits together — without one denial wasting capacity in the others.
---

# Composite Limits

Real limits often come in tiers: "10 per second, 300 per minute, 5,000 per
hour". `Multi` combines several limiters with AND logic — a request is allowed
only if every one of them allows it.

```go
multi := ratelimit.NewMulti(
    ratelimit.NewTokenBucket(10, 10),                        // 10/s
    ratelimit.NewSlidingWindowCounter(300, time.Minute),     // 300/min
    ratelimit.NewSlidingWindowCounter(5000, time.Hour),      // 5,000/h
)

if multi.Allow() {
    // allowed by all three
}
```

`Multi` is itself a `Limiter`, so it supports `AllowN`, `Wait`, `WaitN`, and
`Reset`, and can be nested or passed anywhere a `Limiter` is accepted.

## Check before consuming

The naive approach — call `Allow` on each limiter in turn — has a flaw: if the
hourly limit denies, the per-second and per-minute limits have already spent a
unit on a request that was never served.

When every limiter implements `ResultLimiter` (all built-in limiters do),
`Multi` avoids that: it first runs `CheckN` against all of them, and only
consumes when every check passes. A denied request costs nothing.

If any limiter does not implement `ResultLimiter`, `Multi` falls back to
consuming from each limiter in turn, and cannot undo earlier consumption when
a later one denies. `Multi` itself is not a `ResultLimiter`, so nesting one
`Multi` inside another triggers this fallback — pass all limiters to a single
`NewMulti` instead.

## Waiting

When every limiter implements `ResultLimiter`, `WaitN` on a `Multi` waits for
the **longest** `RetryAfter` reported by the limiters that denied, then checks
all of them again. It returns `ErrExceedsLimit` straight away if `n` is larger
than any limiter's capacity, as reported in `Result.Limit`. Built-in limiters
always set it; a custom `ResultLimiter` that leaves it zero makes `WaitN` retry
until the context ends.

Otherwise `WaitN` calls each limiter's `WaitN` in turn, so earlier limiters are
consumed while later ones are still being waited on, and nothing is undone if
a later wait fails.

## Concurrency

A mutex serializes each check-then-consume step, so two goroutines calling the
same `Multi` cannot both pass the checks and then both consume past a limit.
That guarantee only covers calls that go **through** the `Multi`: if you also
call the wrapped limiters directly, those calls can race with it.

## Keyed composite limits

`Multi` combines single limiters. For tiered limits per key, call each keyed
limiter in your own code, checking all of them before taking from any:

```go
func allow(key string) bool {
    if !perSecond.Check(key).Allowed || !perHour.Check(key).Allowed {
        return false
    }
    return perSecond.Take(key).Allowed && perHour.Take(key).Allowed
}
```

This is not atomic across the two limiters; under concurrent requests for the
same key it can occasionally consume from one and be denied by the other.
