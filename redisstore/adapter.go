package redisstore

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClientAdapter adapts go-redis client to RedisClient interface.
type RedisClientAdapter struct {
	client *redis.Client
}

// NewRedisClientAdapter creates a new Redis client adapter.
func NewRedisClientAdapter(client *redis.Client) *RedisClientAdapter {
	return &RedisClientAdapter{client: client}
}

// Eval executes a Lua script.
func (a *RedisClientAdapter) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return a.client.Eval(ctx, script, keys, args...).Result()
}

// Get gets a value.
func (a *RedisClientAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

// Set sets a value with expiration.
func (a *RedisClientAdapter) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return a.client.Set(ctx, key, value, expiration).Err()
}

// Del deletes keys.
func (a *RedisClientAdapter) Del(ctx context.Context, keys ...string) error {
	return a.client.Del(ctx, keys...).Err()
}

// Incr increments a key.
func (a *RedisClientAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.client.Incr(ctx, key).Result()
}

// Expire sets expiration on a key.
func (a *RedisClientAdapter) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	return a.client.Expire(ctx, key, expiration).Result()
}

// TTL gets the TTL of a key.
func (a *RedisClientAdapter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return a.client.TTL(ctx, key).Result()
}

// RedisClusterClientAdapter adapts go-redis cluster client to RedisClient interface.
type RedisClusterClientAdapter struct {
	client *redis.ClusterClient
}

// NewRedisClusterClientAdapter creates a new Redis cluster client adapter.
func NewRedisClusterClientAdapter(client *redis.ClusterClient) *RedisClusterClientAdapter {
	return &RedisClusterClientAdapter{client: client}
}

// Eval executes a Lua script.
func (a *RedisClusterClientAdapter) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return a.client.Eval(ctx, script, keys, args...).Result()
}

// Get gets a value.
func (a *RedisClusterClientAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.client.Get(ctx, key).Result()
}

// Set sets a value with expiration.
func (a *RedisClusterClientAdapter) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return a.client.Set(ctx, key, value, expiration).Err()
}

// Del deletes keys.
func (a *RedisClusterClientAdapter) Del(ctx context.Context, keys ...string) error {
	return a.client.Del(ctx, keys...).Err()
}

// Incr increments a key.
func (a *RedisClusterClientAdapter) Incr(ctx context.Context, key string) (int64, error) {
	return a.client.Incr(ctx, key).Result()
}

// Expire sets expiration on a key.
func (a *RedisClusterClientAdapter) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	return a.client.Expire(ctx, key, expiration).Result()
}

// TTL gets the TTL of a key.
func (a *RedisClusterClientAdapter) TTL(ctx context.Context, key string) (time.Duration, error) {
	return a.client.TTL(ctx, key).Result()
}
