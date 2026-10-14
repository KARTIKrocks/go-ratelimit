---
id: redis
title: Redis
description: Distributed rate limiting with the redisstore module — Redis token bucket, sliding window and fixed window limiters, Redis Cluster, fail-open and fail-closed behaviour, and resets.
---

# Redis

An in-memory limiter only knows about the requests its own process has seen.
Run three instances behind a load balancer and a client gets three times the
limit. The `redisstore` module keeps the state in Redis instead, so every
instance enforces one shared limit.

```bash
go get github.com/KARTIKrocks/go-ratelimit/redisstore
```

`redisstore` v1.0.0 requires the core module at v1.1.0 or later.

## Quick start

```go
import (
    "github.com/redis/go-redis/v9"

    "github.com/KARTIKrocks/go-ratelimit"
    "github.com/KARTIKrocks/go-ratelimit/redisstore"
)

client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
store := redisstore.NewRedisClientAdapter(client)

limiter := redisstore.NewRedisTokenBucket(store, "myapp", 10, 20) // 10/s, burst 20

handler := ratelimit.Middleware(limiter)(mux)
```

The Redis limiters implement `KeyedLimiter` and `KeyedResultLimiter`, so they
plug into the middleware, `PathLimiter`, and the metrics wrapper exactly like
the in-memory ones.

## Limiters

| Constructor | Algorithm |
| --- | --- |
| `NewRedisTokenBucket(client, prefix, rate, burst, opts...)` | [token bucket](./token-bucket.md) |
| `NewRedisTokenBucketPerDuration(client, prefix, count, per, burst, opts...)` | token bucket |
| `NewRedisSlidingWindow(client, prefix, limit, window, opts...)` | [sliding window counter](./sliding-window.md#sliding-window-counter) |
| `NewRedisFixedWindow(client, prefix, limit, window, opts...)` | [fixed window](./fixed-window.md) |

Windows have millisecond precision: they are rounded down to whole
milliseconds and must be at least 1ms.

## How it works

Each decision is one Lua script, run atomically by Redis:

- **Redis server time.** Scripts read the clock with Redis `TIME`, so clock
  drift between your application servers cannot skew the limit.
- **One Redis key per rate-limit key**, stored as a small hash with an expiry,
  so idle keys clean themselves up and no cleanup goroutine is needed.
- **Redis Cluster safe.** Since a script touches a single key, it never spans
  hash slots.

Keys are named `<prefix>:<algorithm>:{<key>}`, where the algorithm is `tb`,
`sw`, or `fw`. Use a distinct prefix per limiter so two limits never share
state:

```text
myapp:tb:{user:42}
myapp:fw:{203.0.113.9}
```

## Redis Cluster

Wrap a cluster client with the cluster adapter; nothing else changes:

```go
cluster := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs: []string{"redis-0:6379", "redis-1:6379", "redis-2:6379"},
})
store := redisstore.NewRedisClusterClientAdapter(cluster)
```

## When Redis fails

A rate limiter on the request path should not take your service down with
it. By default the limiters **fail open**: if Redis returns an error, the
request is allowed.

```go
limiter := redisstore.NewRedisTokenBucket(store, "myapp", 10, 20,
    redisstore.WithFailClosed(),
    redisstore.WithErrorHandler(func(err error) {
        slog.Error("rate limiter", "err", err)
        metrics.RecordError("api")
    }),
)
```

| Option | Effect |
| --- | --- |
| `WithFailClosed()` | deny on a Redis error; the denied `Result` carries a one-second `RetryAfter`, and `WaitN` returns the error |
| `WithErrorHandler(fn)` | call `fn` with every Redis error, for logging or metrics; not called when the error is the caller's context ending |

Fail open for general API traffic, where an outage of the limiter should not
be an outage of the API. Fail closed for limits that guard against abuse or
cost — login attempts, SMS sends, paid upstream APIs.

Always set an error handler. Failing open silently means you will not notice
that rate limiting has stopped working.

## Context and timeouts

The interface methods use `context.Background()`. For control over how long a
check may wait on Redis, use the context variants:

```go
ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
defer cancel()

result := limiter.TakeNCtx(ctx, key, 1)
```

| Method | Context variant |
| --- | --- |
| `Take`, `TakeN` | `TakeNCtx(ctx, key, n)` |
| `Check`, `CheckN` | `CheckNCtx(ctx, key, n)` |
| `Reset` | `ResetCtx(ctx, key)` |
| `ResetAll` | `ResetAllCtx(ctx)` |
| `Wait`, `WaitN` | already take a context |

Also set sensible `DialTimeout`, `ReadTimeout`, and `WriteTimeout` on the
go-redis client itself.

## Resetting

`Reset(key)` deletes one key. `ResetAll()` deletes every key under the
limiter's prefix by scanning for it with `SCAN` and removing matches in
batches, on every master in a cluster. On a large keyspace that takes a while;
it is meant for tests and administrative tools, not the request path.

## Custom clients

The limiters accept any `RedisClient`, an interface covering `Eval`, `Get`,
`Set`, `Del`, `Incr`, `Expire`, and `TTL`. Implement it to use a different
Redis library or to add instrumentation. To support `ResetAll`, also implement
`KeyDeleter`; without it, `ResetAll` reports `ErrResetAllUnsupported` to the
error handler and deletes nothing.

`Eval` must return the script's reply as a `[]any` of integers (`int64`, `int`,
or numeric strings), as go-redis does.
