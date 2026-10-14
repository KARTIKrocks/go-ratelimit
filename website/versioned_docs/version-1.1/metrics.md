---
id: metrics
title: Metrics
description: Count allowed, denied, and errored rate limit checks with the metrics module's instrumented wrappers, read them in-process, and export them to Prometheus.
---

# Metrics

The `metrics` module counts how many checks each limiter allowed, denied, and
failed, and can export those counts to Prometheus.

```bash
go get github.com/KARTIKrocks/go-ratelimit/metrics
```

## Instrumenting a limiter

Wrap a keyed limiter with `NewInstrumented`, giving it a name:

```go
import "github.com/KARTIKrocks/go-ratelimit/metrics"

base := ratelimit.NewKeyedTokenBucket(10, 20, time.Minute)
defer base.Close()

limiter := metrics.NewInstrumented(base, "api")

handler := ratelimit.Middleware(limiter)(mux)
```

`Instrumented` implements `KeyedResultLimiter`, so it works wherever that
interface is accepted — the middleware, `PathLimiter`, or your own code. It
has no `Allow`, `Wait`, or `Reset`; call those on the base limiter, and note
that `WaitMiddleware`, which needs a `KeyedLimiter`, cannot take it. Every `Take`
and `TakeN` records one allowed or denied check. `Check` and `CheckN` are
read-only peeks and record nothing.

For a single (non-keyed) limiter, use `NewSimpleInstrumented`:

```go
global := metrics.NewSimpleInstrumented(ratelimit.NewTokenBucket(500, 100), "global")
handler := ratelimit.MiddlewareFunc(global)(mux)
```

## Reading the counts

```go
stats := limiter.GetStats()        // this limiter
stats = metrics.GetStats("api")    // any limiter, by name
all := metrics.GetAllStats()       // map[string]metrics.Stats

fmt.Println(stats.Allowed, stats.Denied, stats.Errors, stats.LastUpdate)
```

`LastUpdate` has one-second resolution.

### Errors

The wrappers only see `Result`s, so they cannot tell a Redis failure from a
decision. Record errors yourself from the Redis
[error handler](./redis.md#when-redis-fails):

```go
limiter := redisstore.NewRedisTokenBucket(store, "myapp", 10, 20,
    redisstore.WithErrorHandler(func(err error) {
        metrics.RecordError("api")
    }),
)
```

## Prometheus

Call `RegisterPrometheus` once at startup, then serve the registry as usual:

```go
metrics.RegisterPrometheus(prometheus.DefaultRegisterer)

mux.Handle("/metrics", promhttp.Handler())
```

| Metric | Labels | Counts |
| --- | --- | --- |
| `ratelimit_requests_total` | `limiter`, `result` (`allowed` / `denied`) | every recorded check |
| `ratelimit_requests_allowed_total` | `limiter` | allowed checks |
| `ratelimit_requests_denied_total` | `limiter` | denied checks |
| `ratelimit_errors_total` | `limiter` | recorded errors |

The `limiter` label is the name you gave the wrapper. A useful alert is the
denial ratio per limiter:

```text
sum by (limiter) (rate(ratelimit_requests_denied_total[5m]))
  / sum by (limiter) (rate(ratelimit_requests_total[5m]))
```

:::warning Register once

The Prometheus collectors are package-level. Calling `RegisterPrometheus` twice
with the same registerer panics with a duplicate registration error, so call it
from `main`, not from code that can run more than once. Checks recorded before
it is called are not exported.

:::

## Separate collectors

The package-level functions use a global `Collector`. For isolated counts — in
tests, or for a component that should not share names with the rest of the
process — create one with `NewCollector` and pass it to
`NewInstrumentedWithCollector`:

```go
collector := metrics.NewCollector()
limiter := metrics.NewInstrumentedWithCollector(base, "api", collector)

collector.GetStats("api")
collector.ResetAll()
```

Prometheus export is shared: once registered, every collector also increments
the Prometheus counters.
