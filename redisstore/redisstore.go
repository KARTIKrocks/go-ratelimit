// Package redisstore provides distributed rate limiters backed by Redis.
//
// Each limiter runs a Lua script that reads the current time from Redis
// (so application servers' clocks do not matter) and touches exactly one key
// per rate-limit key, which makes it safe to use with Redis Cluster. Times
// are tracked with millisecond precision.
//
// By default limiters fail open: if Redis returns an error, requests are
// allowed. Use WithFailClosed to deny instead, and WithErrorHandler to log or
// count errors.
package redisstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	ratelimit "github.com/KARTIKrocks/go-ratelimit"
)

// Compile-time interface compliance checks.
var (
	_ ratelimit.KeyedLimiter       = (*RedisTokenBucket)(nil)
	_ ratelimit.KeyedLimiter       = (*RedisSlidingWindow)(nil)
	_ ratelimit.KeyedLimiter       = (*RedisFixedWindow)(nil)
	_ ratelimit.KeyedResultLimiter = (*RedisTokenBucket)(nil)
	_ ratelimit.KeyedResultLimiter = (*RedisSlidingWindow)(nil)
	_ ratelimit.KeyedResultLimiter = (*RedisFixedWindow)(nil)

	_ RedisClient = (*RedisClientAdapter)(nil)
	_ RedisClient = (*RedisClusterClientAdapter)(nil)
	_ KeyDeleter  = (*RedisClientAdapter)(nil)
	_ KeyDeleter  = (*RedisClusterClientAdapter)(nil)
)

// ErrResetAllUnsupported is reported to the error handler when ResetAll is
// called with a client that does not implement KeyDeleter.
var ErrResetAllUnsupported = errors.New("redisstore: client does not implement KeyDeleter, ResetAll is unsupported")

// KeyDeleter is optionally implemented by a RedisClient to support ResetAll.
// Both adapters in this package implement it.
type KeyDeleter interface {
	// DeleteMatching deletes every key matching the glob pattern.
	DeleteMatching(ctx context.Context, pattern string) error
}

// RedisClient is the interface for Redis operations.
type RedisClient interface {
	// Eval executes a Lua script.
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)

	// Get gets a value.
	Get(ctx context.Context, key string) (string, error)

	// Set sets a value with expiration.
	Set(ctx context.Context, key string, value any, expiration time.Duration) error

	// Del deletes keys.
	Del(ctx context.Context, keys ...string) error

	// Incr increments a key.
	Incr(ctx context.Context, key string) (int64, error)

	// Expire sets expiration on a key.
	Expire(ctx context.Context, key string, expiration time.Duration) (bool, error)

	// TTL gets the TTL of a key.
	TTL(ctx context.Context, key string) (time.Duration, error)
}

// Option configures a Redis-backed limiter.
type Option func(*redisLimiter)

// WithFailClosed makes the limiter deny requests when Redis returns an
// error, instead of allowing them (the default). Denied results carry a
// one-second RetryAfter, and WaitN returns the Redis error.
func WithFailClosed() Option {
	return func(l *redisLimiter) {
		l.failClosed = true
	}
}

// WithErrorHandler sets a function that is called with every Redis error,
// for logging or metrics. It is not called when the context is done.
func WithErrorHandler(fn func(error)) Option {
	return func(l *redisLimiter) {
		l.onError = fn
	}
}

// failClosedRetry is the RetryAfter reported when failing closed.
const failClosedRetry = time.Second

// timeNow is the Lua prelude shared by all scripts. It reads the time from
// Redis in milliseconds. replicate_commands is required before writing after
// TIME on Redis versions before 5 and is a no-op on newer versions.
const timeNow = `
if redis.replicate_commands then redis.replicate_commands() end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
`

// Every script takes KEYS[1] and ARGV = {limit, param, requested, consume}
// and returns {allowed, remaining, retry_after_ms, reset_at_ms}. Numbers are
// returned as integers because Redis truncates Lua floats in replies.

// tokenBucketScript: param is the refill rate in tokens per millisecond.
var tokenBucketScript = timeNow + `
local key = KEYS[1]
local burst = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local consume = ARGV[4] == '1'

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1]) or burst
local ts = tonumber(data[2]) or now
if now > ts then
    tokens = math.min(burst, tokens + (now - ts) * rate)
end

if tokens >= requested then
    if consume then
        tokens = tokens - requested
        redis.call('HSET', key, 'tokens', tokens, 'ts', math.max(now, ts))
        redis.call('PEXPIRE', key, math.ceil(burst / rate) + 1000)
    end
    return {1, math.floor(tokens), 0, 0}
end
return {0, math.floor(tokens), math.ceil((requested - tokens) / rate), 0}
`

// slidingWindowScript: param is the window length in milliseconds.
var slidingWindowScript = timeNow + `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local consume = ARGV[4] == '1'

local idx = math.floor(now / window)
local reset_at = (idx + 1) * window

local data = redis.call('HMGET', key, 'idx', 'curr', 'prev')
local stored = tonumber(data[1])
local curr, prev = 0, 0
if stored == idx then
    curr = tonumber(data[2]) or 0
    prev = tonumber(data[3]) or 0
elseif stored == idx - 1 then
    prev = tonumber(data[2]) or 0
end

local weight = (now - idx * window) / window
local count = prev * (1 - weight) + curr

if count + requested <= limit then
    if consume then
        curr = curr + requested
        count = count + requested
        redis.call('HSET', key, 'idx', idx, 'curr', curr, 'prev', prev)
        redis.call('PEXPIRE', key, reset_at - now + window)
    end
    return {1, math.floor(limit - count), 0, reset_at}
end
return {0, math.max(0, math.floor(limit - count)), reset_at - now, reset_at}
`

// fixedWindowScript: param is the window length in milliseconds.
var fixedWindowScript = timeNow + `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local consume = ARGV[4] == '1'

local idx = math.floor(now / window)
local reset_at = (idx + 1) * window

local data = redis.call('HMGET', key, 'idx', 'count')
local count = 0
if tonumber(data[1]) == idx then
    count = tonumber(data[2]) or 0
end

if count + requested <= limit then
    if consume then
        count = count + requested
        redis.call('HSET', key, 'idx', idx, 'count', count)
        redis.call('PEXPIRE', key, reset_at - now)
    end
    return {1, limit - count, 0, reset_at}
end
return {0, limit - count, reset_at - now, reset_at}
`

// redisLimiter holds the logic shared by all Redis-backed algorithms.
type redisLimiter struct {
	client     RedisClient
	keyPrefix  string // "<prefix>:<algorithm>:"
	script     string
	limit      int
	param      any // algorithm parameter passed to the script as ARGV[2]
	failClosed bool
	onError    func(error)
}

func newRedisLimiter(client RedisClient, keyPrefix, algorithm, script string, limit int, param any, opts []Option) redisLimiter {
	l := redisLimiter{
		client:    client,
		keyPrefix: keyPrefix + ":" + algorithm + ":",
		script:    script,
		limit:     limit,
		param:     param,
	}
	for _, opt := range opts {
		opt(&l)
	}
	return l
}

// key wraps k in a hash tag so that, if a script ever needs more than one
// key per rate-limit key, they all land on the same Redis Cluster slot.
func (l *redisLimiter) key(k string) string {
	return l.keyPrefix + "{" + k + "}"
}

// eval runs the script for key and parses its reply.
func (l *redisLimiter) eval(ctx context.Context, key string, n int, consume bool) (ratelimit.Result, error) {
	c := 0
	if consume {
		c = 1
	}
	res, err := l.client.Eval(ctx, l.script, []string{l.key(key)}, l.limit, l.param, n, c)
	if err != nil {
		return ratelimit.Result{}, err
	}

	vals, ok := res.([]any)
	if !ok || len(vals) != 4 {
		return ratelimit.Result{}, fmt.Errorf("redisstore: unexpected script reply %v", res)
	}
	var nums [4]int64
	for i, v := range vals {
		if nums[i], ok = toInt64(v); !ok {
			return ratelimit.Result{}, fmt.Errorf("redisstore: unexpected script reply %v", res)
		}
	}

	// Clamp rather than reject: remaining goes negative when the limit is
	// lowered while Redis still holds a higher count, and rejecting would
	// fail open. Bounding by l.limit also keeps the int conversion safe.
	remaining := min(max(0, nums[1]), int64(l.limit))

	result := ratelimit.Result{
		Allowed:    nums[0] == 1,
		Limit:      l.limit,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(nums[2]) * time.Millisecond,
	}
	if nums[3] > 0 {
		result.ResetAt = time.UnixMilli(nums[3])
	}
	return result, nil
}

// do validates n, runs the script and applies the fail-open/closed policy.
func (l *redisLimiter) do(ctx context.Context, key string, n int, consume bool) ratelimit.Result {
	if validateN(n, l.limit) != nil {
		return ratelimit.Result{Limit: l.limit}
	}

	result, err := l.eval(ctx, key, n, consume)
	if err == nil {
		return result
	}

	l.report(ctx, err)
	if l.failClosed {
		return ratelimit.Result{Limit: l.limit, RetryAfter: failClosedRetry}
	}
	return ratelimit.Result{Allowed: true, Limit: l.limit, Remaining: l.limit}
}

func (l *redisLimiter) report(ctx context.Context, err error) {
	if l.onError != nil && ctx.Err() == nil {
		l.onError(err)
	}
}

// Allow checks if a request for the key is allowed.
func (l *redisLimiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (l *redisLimiter) AllowN(key string, n int) bool {
	return l.TakeN(key, n).Allowed
}

// Wait blocks until a request for the key is allowed.
func (l *redisLimiter) Wait(ctx context.Context, key string) error {
	return l.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed or ctx is done.
// It returns ratelimit.ErrInvalidN or ratelimit.ErrExceedsLimit if n can
// never be allowed. On a Redis error it returns nil when failing open, or
// the error when failing closed.
func (l *redisLimiter) WaitN(ctx context.Context, key string, n int) error {
	if err := validateN(n, l.limit); err != nil {
		return err
	}

	for {
		result, err := l.eval(ctx, key, n, true)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			l.report(ctx, err)
			if l.failClosed {
				return err
			}
			return nil
		}
		if result.Allowed {
			return nil
		}

		timer := time.NewTimer(max(result.RetryAfter, time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Reset resets the limiter for the given key.
func (l *redisLimiter) Reset(key string) {
	l.ResetCtx(context.Background(), key)
}

// ResetCtx resets the limiter for the given key with a context.
func (l *redisLimiter) ResetCtx(ctx context.Context, key string) {
	if err := l.client.Del(ctx, l.key(key)); err != nil {
		l.report(ctx, err)
	}
}

// ResetAll resets every key of this limiter. See ResetAllCtx.
func (l *redisLimiter) ResetAll() {
	l.ResetAllCtx(context.Background())
}

// ResetAllCtx resets every key of this limiter by scanning for its key
// prefix, which can be slow on large keyspaces. The client must implement
// KeyDeleter; otherwise ErrResetAllUnsupported is reported to the error
// handler and nothing is deleted.
func (l *redisLimiter) ResetAllCtx(ctx context.Context) {
	d, ok := l.client.(KeyDeleter)
	if !ok {
		l.report(ctx, ErrResetAllUnsupported)
		return
	}
	if err := d.DeleteMatching(ctx, globEscaper.Replace(l.keyPrefix)+"*"); err != nil {
		l.report(ctx, err)
	}
}

// globEscaper escapes Redis glob metacharacters in a literal key prefix.
var globEscaper = strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`)

// Check returns the current state for a key without consuming.
func (l *redisLimiter) Check(key string) ratelimit.Result {
	return l.CheckNCtx(context.Background(), key, 1)
}

// CheckN returns the state for n requests without consuming.
func (l *redisLimiter) CheckN(key string, n int) ratelimit.Result {
	return l.CheckNCtx(context.Background(), key, n)
}

// CheckNCtx returns the state for n requests without consuming, with context.
func (l *redisLimiter) CheckNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	return l.do(ctx, key, n, false)
}

// Take consumes one request for the key and returns the result.
func (l *redisLimiter) Take(key string) ratelimit.Result {
	return l.TakeNCtx(context.Background(), key, 1)
}

// TakeN consumes n requests for the key and returns the result.
func (l *redisLimiter) TakeN(key string, n int) ratelimit.Result {
	return l.TakeNCtx(context.Background(), key, n)
}

// TakeNCtx consumes n requests for the key and returns the result, with context.
func (l *redisLimiter) TakeNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	return l.do(ctx, key, n, true)
}

// RedisTokenBucket implements distributed token bucket using Redis.
type RedisTokenBucket struct {
	redisLimiter
}

// NewRedisTokenBucket creates a new Redis-backed token bucket limiter.
// rate is in tokens per second.
func NewRedisTokenBucket(client RedisClient, keyPrefix string, rate float64, burst int, opts ...Option) *RedisTokenBucket {
	if isNil(client) {
		panic("ratelimit: client must not be nil")
	}
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if burst <= 0 {
		panic("ratelimit: burst must be positive")
	}
	return &RedisTokenBucket{
		redisLimiter: newRedisLimiter(client, keyPrefix, "tb", tokenBucketScript, burst, rate/1000, opts),
	}
}

// NewRedisTokenBucketPerDuration creates a Redis token bucket with rate per duration.
func NewRedisTokenBucketPerDuration(client RedisClient, keyPrefix string, count int, per time.Duration, burst int, opts ...Option) *RedisTokenBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewRedisTokenBucket(client, keyPrefix, rate, burst, opts...)
}

// RedisSlidingWindow implements distributed sliding window counter using Redis.
type RedisSlidingWindow struct {
	redisLimiter
}

// NewRedisSlidingWindow creates a new Redis-backed sliding window limiter.
// window must be at least one millisecond; it is rounded down to whole
// milliseconds.
func NewRedisSlidingWindow(client RedisClient, keyPrefix string, limit int, window time.Duration, opts ...Option) *RedisSlidingWindow {
	validateWindowArgs(client, limit, window)
	return &RedisSlidingWindow{
		redisLimiter: newRedisLimiter(client, keyPrefix, "sw", slidingWindowScript, limit, window.Milliseconds(), opts),
	}
}

// RedisFixedWindow implements distributed fixed window using Redis.
type RedisFixedWindow struct {
	redisLimiter
}

// NewRedisFixedWindow creates a new Redis-backed fixed window limiter.
// window must be at least one millisecond; it is rounded down to whole
// milliseconds.
func NewRedisFixedWindow(client RedisClient, keyPrefix string, limit int, window time.Duration, opts ...Option) *RedisFixedWindow {
	validateWindowArgs(client, limit, window)
	return &RedisFixedWindow{
		redisLimiter: newRedisLimiter(client, keyPrefix, "fw", fixedWindowScript, limit, window.Milliseconds(), opts),
	}
}

// Helper functions

func validateWindowArgs(client RedisClient, limit int, window time.Duration) {
	if isNil(client) {
		panic("ratelimit: client must not be nil")
	}
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window < time.Millisecond {
		panic("ratelimit: window must be at least 1ms")
	}
}

// isNil reports whether c is nil or holds a nil pointer, which would pass a
// plain nil check and panic on first use.
func isNil(c RedisClient) bool {
	if c == nil {
		return true
	}
	v := reflect.ValueOf(c)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// validateN mirrors the root package's validation of n.
func validateN(n, limit int) error {
	if n <= 0 {
		return ratelimit.ErrInvalidN
	}
	if n > limit {
		return ratelimit.ErrExceedsLimit
	}
	return nil
}

// toInt64 converts a script reply value. go-redis returns int64; other
// RedisClient implementations may return int or string.
func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int64:
		return val, true
	case int:
		return int64(val), true
	case string:
		// Atoi parses straight into int, so widening to int64 is always safe.
		i, err := strconv.Atoi(val)
		return int64(i), err == nil
	default:
		return 0, false
	}
}
