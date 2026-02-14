package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	ratelimit "github.com/KARTIKrocks/go-ratelimit"
)

// Compile-time interface compliance checks.
var (
	_ ratelimit.KeyedResultLimiter = (*RedisTokenBucket)(nil)
	_ ratelimit.KeyedResultLimiter = (*RedisSlidingWindow)(nil)
	_ ratelimit.KeyedResultLimiter = (*RedisFixedWindow)(nil)

	_ RedisClient = (*RedisClientAdapter)(nil)
	_ RedisClient = (*RedisClusterClientAdapter)(nil)
)

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

// RedisTokenBucket implements distributed token bucket using Redis.
type RedisTokenBucket struct {
	client    RedisClient
	keyPrefix string
	rate      float64
	burst     int
}

// NewRedisTokenBucket creates a new Redis-backed token bucket limiter.
func NewRedisTokenBucket(client RedisClient, keyPrefix string, rate float64, burst int) *RedisTokenBucket {
	if client == nil {
		panic("ratelimit: client must not be nil")
	}
	if rate <= 0 {
		panic("ratelimit: rate must be positive")
	}
	if burst <= 0 {
		panic("ratelimit: burst must be positive")
	}
	return &RedisTokenBucket{
		client:    client,
		keyPrefix: keyPrefix,
		rate:      rate,
		burst:     burst,
	}
}

// NewRedisTokenBucketPerDuration creates a Redis token bucket with rate per duration.
func NewRedisTokenBucketPerDuration(client RedisClient, keyPrefix string, count int, per time.Duration, burst int) *RedisTokenBucket {
	if per <= 0 {
		panic("ratelimit: per duration must be positive")
	}
	rate := float64(count) / per.Seconds()
	return NewRedisTokenBucket(client, keyPrefix, rate, burst)
}

// Lua script for token bucket algorithm
var tokenBucketScript = `
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local data = redis.call('HMGET', key, 'tokens', 'last_update')
local tokens = tonumber(data[1]) or burst
local last_update = tonumber(data[2]) or now

-- Calculate tokens to add based on elapsed time
local elapsed = now - last_update
local add_tokens = elapsed * rate
tokens = math.min(burst, tokens + add_tokens)

-- Check if request can be fulfilled
local allowed = 0
local remaining = tokens
if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
    remaining = tokens
end

-- Calculate retry after
local retry_after = 0
if allowed == 0 then
    retry_after = (requested - tokens) / rate
end

-- Update state
redis.call('HMSET', key, 'tokens', tokens, 'last_update', now)
redis.call('EXPIRE', key, math.ceil(burst / rate) + 1)

return {allowed, remaining, retry_after}
`

func (rtb *RedisTokenBucket) key(k string) string {
	return rtb.keyPrefix + ":tb:" + k
}

// Allow checks if a request for the key is allowed.
func (rtb *RedisTokenBucket) Allow(key string) bool {
	return rtb.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (rtb *RedisTokenBucket) AllowN(key string, n int) bool {
	result := rtb.TakeN(key, n)
	return result.Allowed
}

// Wait blocks until a request for the key is allowed.
func (rtb *RedisTokenBucket) Wait(ctx context.Context, key string) error {
	return rtb.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (rtb *RedisTokenBucket) WaitN(ctx context.Context, key string, n int) error {
	for {
		result := rtb.TakeNCtx(ctx, key, n)
		if result.Allowed {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		timer := time.NewTimer(result.RetryAfter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Reset resets the limiter for the given key.
func (rtb *RedisTokenBucket) Reset(key string) {
	rtb.ResetCtx(context.Background(), key)
}

// ResetCtx resets the limiter for the given key with a context.
func (rtb *RedisTokenBucket) ResetCtx(ctx context.Context, key string) {
	rtb.client.Del(ctx, rtb.key(key))
}

// ResetAll cannot reset all keys efficiently in Redis (would require scanning).
func (rtb *RedisTokenBucket) ResetAll() {
	// Not implemented - would require SCAN which can be expensive
}

// Check returns the current state for a key without consuming tokens.
func (rtb *RedisTokenBucket) Check(key string) ratelimit.Result {
	return rtb.CheckNCtx(context.Background(), key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (rtb *RedisTokenBucket) CheckN(key string, n int) ratelimit.Result {
	return rtb.CheckNCtx(context.Background(), key, n)
}

// CheckNCtx returns the state for n tokens without consuming, with context.
func (rtb *RedisTokenBucket) CheckNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().UnixNano()) / float64(time.Second)

	res, err := rtb.client.Eval(ctx, tokenBucketScript, []string{rtb.key(key)},
		rtb.rate, rtb.burst, now, 0)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rtb.burst, Remaining: rtb.burst}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 3 {
		return ratelimit.Result{Allowed: true, Limit: rtb.burst, Remaining: rtb.burst}
	}

	remaining := toInt(vals[1])
	result := ratelimit.Result{
		Limit:     rtb.burst,
		Remaining: remaining,
		Allowed:   remaining >= n,
	}

	if !result.Allowed {
		needed := float64(n) - float64(remaining)
		result.RetryAfter = time.Duration(needed / rtb.rate * float64(time.Second))
	}

	return result
}

// Take consumes a token for the key and returns the result.
func (rtb *RedisTokenBucket) Take(key string) ratelimit.Result {
	return rtb.TakeNCtx(context.Background(), key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (rtb *RedisTokenBucket) TakeN(key string, n int) ratelimit.Result {
	return rtb.TakeNCtx(context.Background(), key, n)
}

// TakeNCtx consumes n tokens for the key and returns the result, with context.
func (rtb *RedisTokenBucket) TakeNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().UnixNano()) / float64(time.Second)

	res, err := rtb.client.Eval(ctx, tokenBucketScript, []string{rtb.key(key)},
		rtb.rate, rtb.burst, now, n)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rtb.burst, Remaining: rtb.burst}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 3 {
		return ratelimit.Result{Allowed: true, Limit: rtb.burst, Remaining: rtb.burst}
	}

	return ratelimit.Result{
		Allowed:    toInt(vals[0]) == 1,
		Limit:      rtb.burst,
		Remaining:  toInt(vals[1]),
		RetryAfter: time.Duration(toFloat(vals[2]) * float64(time.Second)),
	}
}

// RedisSlidingWindow implements distributed sliding window using Redis.
type RedisSlidingWindow struct {
	client    RedisClient
	keyPrefix string
	limit     int
	window    time.Duration
}

// NewRedisSlidingWindow creates a new Redis-backed sliding window limiter.
func NewRedisSlidingWindow(client RedisClient, keyPrefix string, limit int, window time.Duration) *RedisSlidingWindow {
	if client == nil {
		panic("ratelimit: client must not be nil")
	}
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &RedisSlidingWindow{
		client:    client,
		keyPrefix: keyPrefix,
		limit:     limit,
		window:    window,
	}
}

// Lua script for sliding window counter algorithm
var slidingWindowScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- Get current and previous window data
local curr_key = key .. ':' .. math.floor(now / window)
local prev_key = key .. ':' .. (math.floor(now / window) - 1)

local curr_count = tonumber(redis.call('GET', curr_key)) or 0
local prev_count = tonumber(redis.call('GET', prev_key)) or 0

-- Calculate weighted count
local elapsed = now % window
local weight = elapsed / window
local count = prev_count * (1 - weight) + curr_count

-- Check if request can be fulfilled
local allowed = 0
local remaining = math.floor(limit - count)
if count + requested <= limit then
    allowed = 1
    redis.call('INCRBY', curr_key, requested)
    redis.call('EXPIRE', curr_key, window * 2)
    remaining = math.floor(limit - count - requested)
end

-- Calculate retry after
local retry_after = 0
if allowed == 0 then
    retry_after = window - elapsed
end

-- Calculate reset time
local reset_at = (math.floor(now / window) + 1) * window

return {allowed, remaining, retry_after, reset_at}
`

func (rsw *RedisSlidingWindow) key(k string) string {
	return rsw.keyPrefix + ":sw:" + k
}

// Allow checks if a request for the key is allowed.
func (rsw *RedisSlidingWindow) Allow(key string) bool {
	return rsw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (rsw *RedisSlidingWindow) AllowN(key string, n int) bool {
	result := rsw.TakeN(key, n)
	return result.Allowed
}

// Wait blocks until a request for the key is allowed.
func (rsw *RedisSlidingWindow) Wait(ctx context.Context, key string) error {
	return rsw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (rsw *RedisSlidingWindow) WaitN(ctx context.Context, key string, n int) error {
	for {
		result := rsw.TakeNCtx(ctx, key, n)
		if result.Allowed {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		timer := time.NewTimer(result.RetryAfter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Reset resets the limiter for the given key.
func (rsw *RedisSlidingWindow) Reset(key string) {
	rsw.ResetCtx(context.Background(), key)
}

// ResetCtx resets the limiter for the given key with a context.
func (rsw *RedisSlidingWindow) ResetCtx(ctx context.Context, key string) {
	now := time.Now().Unix()
	window := int64(rsw.window.Seconds())
	currKey := fmt.Sprintf("%s:%d", rsw.key(key), now/window)
	prevKey := fmt.Sprintf("%s:%d", rsw.key(key), now/window-1)
	rsw.client.Del(ctx, currKey, prevKey)
}

// ResetAll cannot reset all keys efficiently.
func (rsw *RedisSlidingWindow) ResetAll() {
	// Not implemented
}

// Check returns the current state for a key without consuming.
func (rsw *RedisSlidingWindow) Check(key string) ratelimit.Result {
	return rsw.CheckNCtx(context.Background(), key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (rsw *RedisSlidingWindow) CheckN(key string, n int) ratelimit.Result {
	return rsw.CheckNCtx(context.Background(), key, n)
}

// CheckNCtx returns the state for n tokens without consuming, with context.
func (rsw *RedisSlidingWindow) CheckNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().Unix())
	window := rsw.window.Seconds()

	res, err := rsw.client.Eval(ctx, slidingWindowScript, []string{rsw.key(key)},
		rsw.limit, window, now, 0)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rsw.limit, Remaining: rsw.limit}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 4 {
		return ratelimit.Result{Allowed: true, Limit: rsw.limit, Remaining: rsw.limit}
	}

	remaining := toInt(vals[1])
	result := ratelimit.Result{
		Limit:     rsw.limit,
		Remaining: remaining,
		Allowed:   remaining >= n,
		ResetAt:   time.Unix(int64(toFloat(vals[3])), 0),
	}

	if !result.Allowed {
		result.RetryAfter = time.Duration(toFloat(vals[2]) * float64(time.Second))
	}

	return result
}

// Take consumes a token for the key and returns the result.
func (rsw *RedisSlidingWindow) Take(key string) ratelimit.Result {
	return rsw.TakeNCtx(context.Background(), key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (rsw *RedisSlidingWindow) TakeN(key string, n int) ratelimit.Result {
	return rsw.TakeNCtx(context.Background(), key, n)
}

// TakeNCtx consumes n tokens for the key and returns the result, with context.
func (rsw *RedisSlidingWindow) TakeNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().Unix())
	window := rsw.window.Seconds()

	res, err := rsw.client.Eval(ctx, slidingWindowScript, []string{rsw.key(key)},
		rsw.limit, window, now, n)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rsw.limit, Remaining: rsw.limit}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 4 {
		return ratelimit.Result{Allowed: true, Limit: rsw.limit, Remaining: rsw.limit}
	}

	return ratelimit.Result{
		Allowed:    toInt(vals[0]) == 1,
		Limit:      rsw.limit,
		Remaining:  toInt(vals[1]),
		RetryAfter: time.Duration(toFloat(vals[2]) * float64(time.Second)),
		ResetAt:    time.Unix(int64(toFloat(vals[3])), 0),
	}
}

// RedisFixedWindow implements distributed fixed window using Redis.
type RedisFixedWindow struct {
	client    RedisClient
	keyPrefix string
	limit     int
	window    time.Duration
}

// NewRedisFixedWindow creates a new Redis-backed fixed window limiter.
func NewRedisFixedWindow(client RedisClient, keyPrefix string, limit int, window time.Duration) *RedisFixedWindow {
	if client == nil {
		panic("ratelimit: client must not be nil")
	}
	if limit <= 0 {
		panic("ratelimit: limit must be positive")
	}
	if window <= 0 {
		panic("ratelimit: window must be positive")
	}
	return &RedisFixedWindow{
		client:    client,
		keyPrefix: keyPrefix,
		limit:     limit,
		window:    window,
	}
}

// Lua script for fixed window algorithm.
var fixedWindowScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- Compute window key
local window_key = key .. ':' .. math.floor(now / window)

local count = tonumber(redis.call('GET', window_key)) or 0

-- Check if request can be fulfilled
local allowed = 0
local remaining = limit - count
if count + requested <= limit then
    allowed = 1
    redis.call('INCRBY', window_key, requested)
    redis.call('EXPIRE', window_key, window + 1)
    remaining = limit - count - requested
end

-- Calculate retry after and reset time
local retry_after = 0
if allowed == 0 then
    local elapsed = now % window
    retry_after = window - elapsed
end

local reset_at = (math.floor(now / window) + 1) * window

return {allowed, remaining, retry_after, reset_at}
`

func (rfw *RedisFixedWindow) key(k string) string {
	return rfw.keyPrefix + ":fw:" + k
}

// Allow checks if a request for the key is allowed.
func (rfw *RedisFixedWindow) Allow(key string) bool {
	return rfw.AllowN(key, 1)
}

// AllowN checks if n requests for the key are allowed.
func (rfw *RedisFixedWindow) AllowN(key string, n int) bool {
	result := rfw.TakeN(key, n)
	return result.Allowed
}

// Wait blocks until a request for the key is allowed.
func (rfw *RedisFixedWindow) Wait(ctx context.Context, key string) error {
	return rfw.WaitN(ctx, key, 1)
}

// WaitN blocks until n requests for the key are allowed.
func (rfw *RedisFixedWindow) WaitN(ctx context.Context, key string, n int) error {
	for {
		result := rfw.TakeNCtx(ctx, key, n)
		if result.Allowed {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		timer := time.NewTimer(result.RetryAfter)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Reset resets the limiter for the given key.
func (rfw *RedisFixedWindow) Reset(key string) {
	rfw.ResetCtx(context.Background(), key)
}

// ResetCtx resets the limiter for the given key with a context.
func (rfw *RedisFixedWindow) ResetCtx(ctx context.Context, key string) {
	now := time.Now().Unix()
	window := int64(rfw.window.Seconds())
	windowKey := fmt.Sprintf("%s:%d", rfw.key(key), now/window)
	rfw.client.Del(ctx, windowKey)
}

// ResetAll cannot reset all keys efficiently.
func (rfw *RedisFixedWindow) ResetAll() {
	// Not implemented - would require SCAN which can be expensive
}

// Check returns the current state for a key without consuming.
func (rfw *RedisFixedWindow) Check(key string) ratelimit.Result {
	return rfw.CheckNCtx(context.Background(), key, 1)
}

// CheckN returns the state for n tokens without consuming.
func (rfw *RedisFixedWindow) CheckN(key string, n int) ratelimit.Result {
	return rfw.CheckNCtx(context.Background(), key, n)
}

// CheckNCtx returns the state for n tokens without consuming, with context.
func (rfw *RedisFixedWindow) CheckNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().Unix())
	window := rfw.window.Seconds()

	res, err := rfw.client.Eval(ctx, fixedWindowScript, []string{rfw.key(key)},
		rfw.limit, window, now, 0)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rfw.limit, Remaining: rfw.limit}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 4 {
		return ratelimit.Result{Allowed: true, Limit: rfw.limit, Remaining: rfw.limit}
	}

	remaining := toInt(vals[1])
	result := ratelimit.Result{
		Limit:     rfw.limit,
		Remaining: remaining,
		Allowed:   remaining >= n,
		ResetAt:   time.Unix(int64(toFloat(vals[3])), 0),
	}

	if !result.Allowed {
		result.RetryAfter = time.Duration(toFloat(vals[2]) * float64(time.Second))
	}

	return result
}

// Take consumes a token for the key and returns the result.
func (rfw *RedisFixedWindow) Take(key string) ratelimit.Result {
	return rfw.TakeNCtx(context.Background(), key, 1)
}

// TakeN consumes n tokens for the key and returns the result.
func (rfw *RedisFixedWindow) TakeN(key string, n int) ratelimit.Result {
	return rfw.TakeNCtx(context.Background(), key, n)
}

// TakeNCtx consumes n tokens for the key and returns the result, with context.
func (rfw *RedisFixedWindow) TakeNCtx(ctx context.Context, key string, n int) ratelimit.Result {
	now := float64(time.Now().Unix())
	window := rfw.window.Seconds()

	res, err := rfw.client.Eval(ctx, fixedWindowScript, []string{rfw.key(key)},
		rfw.limit, window, now, n)

	if err != nil {
		// Fail open on Redis errors
		return ratelimit.Result{Allowed: true, Limit: rfw.limit, Remaining: rfw.limit}
	}

	vals, ok := res.([]any)
	if !ok || len(vals) < 4 {
		return ratelimit.Result{Allowed: true, Limit: rfw.limit, Remaining: rfw.limit}
	}

	return ratelimit.Result{
		Allowed:    toInt(vals[0]) == 1,
		Limit:      rfw.limit,
		Remaining:  toInt(vals[1]),
		RetryAfter: time.Duration(toFloat(vals[2]) * float64(time.Second)),
		ResetAt:    time.Unix(int64(toFloat(vals[3])), 0),
	}
}

// Helper functions

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}
