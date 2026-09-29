//go:build integration

package redisstore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	ratelimit "github.com/KARTIKrocks/go-ratelimit"
	"github.com/redis/go-redis/v9"
)

// newTestClient connects to REDIS_URL (default localhost:6379) and returns
// a unique key prefix so tests never see each other's state.
func newTestClient(t *testing.T) (RedisClient, string) {
	t.Helper()
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", addr, err)
	}
	return NewRedisClientAdapter(client), fmt.Sprintf("test:%s:%d", t.Name(), time.Now().UnixNano())
}

// failOnError makes silent fail-open impossible to miss in tests.
func failOnError(t *testing.T) Option {
	return WithErrorHandler(func(err error) { t.Errorf("redis error: %v", err) })
}

func TestIntegrationTokenBucket(t *testing.T) {
	client, prefix := newTestClient(t)
	l := NewRedisTokenBucket(client, prefix, 10, 3, failOnError(t))

	for i := range 3 {
		if r := l.Take("k"); !r.Allowed {
			t.Fatalf("request %d denied: %+v", i, r)
		}
	}
	r := l.Take("k")
	if r.Allowed {
		t.Fatal("request over burst allowed")
	}
	// At 10 tokens/s one token takes ~100ms. The old script truncated this
	// sub-second value to 0.
	if r.RetryAfter <= 0 || r.RetryAfter > 150*time.Millisecond {
		t.Errorf("RetryAfter = %v, want about 100ms", r.RetryAfter)
	}
}

func TestIntegrationCheckDoesNotConsume(t *testing.T) {
	client, prefix := newTestClient(t)
	limiters := map[string]interface {
		Allow(string) bool
		CheckN(string, int) ratelimit.Result
	}{
		"token bucket":   NewRedisTokenBucket(client, prefix, 0.001, 2, failOnError(t)),
		"fixed window":   NewRedisFixedWindow(client, prefix, 2, time.Hour, failOnError(t)),
		"sliding window": NewRedisSlidingWindow(client, prefix, 2, time.Hour, failOnError(t)),
	}
	for name, l := range limiters {
		t.Run(name, func(t *testing.T) {
			for range 5 {
				if r := l.CheckN("k", 1); !r.Allowed {
					t.Fatalf("CheckN denied: %+v", r)
				}
			}
			for i := range 2 {
				if !l.Allow("k") {
					t.Fatalf("request %d denied: checks consumed capacity", i)
				}
			}
			if l.Allow("k") {
				t.Fatal("request over limit allowed")
			}
		})
	}
}

func TestIntegrationWaitDoesNotSpin(t *testing.T) {
	client, prefix := newTestClient(t)
	calls := 0
	counting := &countingClient{RedisClient: client, calls: &calls}
	l := NewRedisTokenBucket(counting, prefix, 20, 1, failOnError(t))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for range 3 {
		if err := l.Wait(ctx, "k"); err != nil {
			t.Fatalf("Wait = %v", err)
		}
	}
	// 3 waits at 20/s should take a handful of round trips, not thousands.
	if calls > 20 {
		t.Errorf("Wait made %d Redis calls, want a handful", calls)
	}
}

type countingClient struct {
	RedisClient
	calls *int
}

func (c *countingClient) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	*c.calls++
	return c.RedisClient.Eval(ctx, script, keys, args...)
}

func TestIntegrationWindows(t *testing.T) {
	client, prefix := newTestClient(t)
	// Sub-second and fractional windows used to panic in Reset or break EXPIRE.
	windows := []time.Duration{200 * time.Millisecond, 1500 * time.Millisecond}
	for _, window := range windows {
		limiters := map[string]interface {
			Allow(string) bool
			Take(string) ratelimit.Result
			Reset(string)
		}{
			"fixed":   NewRedisFixedWindow(client, prefix, 2, window, failOnError(t)),
			"sliding": NewRedisSlidingWindow(client, prefix, 2, window, failOnError(t)),
		}
		for name, l := range limiters {
			t.Run(fmt.Sprintf("%s/%v", name, window), func(t *testing.T) {
				key := t.Name()
				l.Allow(key)
				l.Allow(key)
				r := l.Take(key)
				if r.Allowed {
					t.Fatal("request over limit allowed")
				}
				if r.RetryAfter <= 0 || r.RetryAfter > window {
					t.Errorf("RetryAfter = %v, want within (0, %v]", r.RetryAfter, window)
				}
				if r.ResetAt.IsZero() {
					t.Error("ResetAt not set")
				}

				l.Reset(key)
				if !l.Allow(key) {
					t.Error("request denied after Reset")
				}
			})
		}
	}
}

func TestIntegrationFixedWindowRollsOver(t *testing.T) {
	client, prefix := newTestClient(t)
	l := NewRedisFixedWindow(client, prefix, 1, 100*time.Millisecond, failOnError(t))

	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("second request in window allowed")
	}
	time.Sleep(110 * time.Millisecond)
	if !l.Allow("k") {
		t.Error("request in next window denied")
	}
}

func TestIntegrationSlidingWindowWeightsPreviousWindow(t *testing.T) {
	client, prefix := newTestClient(t)
	window := 200 * time.Millisecond
	l := NewRedisSlidingWindow(client, prefix, 4, window, failOnError(t))

	// Align to the start of a window so the whole limit lands in one window.
	time.Sleep(time.Until(time.Now().Truncate(window).Add(window)))
	for range 4 {
		if !l.Allow("k") {
			t.Fatal("request within limit denied")
		}
	}
	// Early in the next window most of the previous count still applies.
	time.Sleep(window + 10*time.Millisecond)
	if l.Take("k").Remaining > 1 {
		t.Error("previous window count was not carried over")
	}
}

func TestIntegrationResetAll(t *testing.T) {
	client, prefix := newTestClient(t)
	l := NewRedisFixedWindow(client, prefix, 1, time.Hour, failOnError(t))
	other := NewRedisFixedWindow(client, prefix+"-other", 1, time.Hour, failOnError(t))

	for _, k := range []string{"a", "b", "c"} {
		l.Allow(k)
	}
	other.Allow("a")

	l.ResetAll()
	for _, k := range []string{"a", "b", "c"} {
		if !l.Allow(k) {
			t.Errorf("key %q still limited after ResetAll", k)
		}
	}
	if other.Allow("a") {
		t.Error("ResetAll deleted another limiter's keys")
	}
}
