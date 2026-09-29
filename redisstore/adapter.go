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
	if client == nil {
		panic("ratelimit: client must not be nil")
	}
	return &RedisClientAdapter{client: client}
}

// DeleteMatching deletes every key matching the glob pattern.
func (a *RedisClientAdapter) DeleteMatching(ctx context.Context, pattern string) error {
	return deleteMatching(ctx, a.client, pattern)
}

// deleteBatch is the SCAN page size and the number of UNLINKs per pipeline.
const deleteBatch = 1000

// deleteMatching scans one node and unlinks matching keys in pipelined
// batches. Each UNLINK names a single key, so it also works on a Cluster
// node, where a multi-key command across slots is rejected.
func deleteMatching(ctx context.Context, c *redis.Client, pattern string) error {
	iter := c.Scan(ctx, 0, pattern, deleteBatch).Iterator()
	pipe := c.Pipeline()
	for iter.Next(ctx) {
		pipe.Unlink(ctx, iter.Val())
		if pipe.Len() >= deleteBatch {
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
		}
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if pipe.Len() > 0 {
		_, err := pipe.Exec(ctx)
		return err
	}
	return nil
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
	if client == nil {
		panic("ratelimit: client must not be nil")
	}
	return &RedisClusterClientAdapter{client: client}
}

// DeleteMatching deletes every key matching the glob pattern on every
// master node.
func (a *RedisClusterClientAdapter) DeleteMatching(ctx context.Context, pattern string) error {
	return a.client.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
		return deleteMatching(ctx, master, pattern)
	})
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
