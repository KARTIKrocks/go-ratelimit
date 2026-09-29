package redisstore

import (
	"context"
	"errors"
	"testing"
	"time"

	ratelimit "github.com/KARTIKrocks/go-ratelimit"
)

// fakeClient returns a fixed Eval reply or error. Methods other than Eval
// and Del are not used by the limiters.
type fakeClient struct {
	RedisClient
	reply any
	err   error
	calls int
	keys  []string
}

func (c *fakeClient) Eval(_ context.Context, _ string, keys []string, _ ...any) (any, error) {
	c.calls++
	c.keys = keys
	return c.reply, c.err
}

func (c *fakeClient) Del(_ context.Context, keys ...string) error {
	c.keys = keys
	return c.err
}

var errRedis = errors.New("redis down")

func TestFailOpenByDefault(t *testing.T) {
	var reported error
	client := &fakeClient{err: errRedis}
	l := NewRedisFixedWindow(client, "p", 5, time.Second, WithErrorHandler(func(err error) { reported = err }))

	r := l.Take("k")
	if !r.Allowed || r.Remaining != 5 {
		t.Errorf("Take = %+v, want allowed with full remaining", r)
	}
	if !errors.Is(reported, errRedis) {
		t.Errorf("error handler got %v, want %v", reported, errRedis)
	}
	if err := l.Wait(context.Background(), "k"); err != nil {
		t.Errorf("Wait = %v, want nil when failing open", err)
	}
}

func TestFailClosed(t *testing.T) {
	client := &fakeClient{err: errRedis}
	l := NewRedisTokenBucket(client, "p", 10, 5, WithFailClosed())

	r := l.Take("k")
	if r.Allowed || r.RetryAfter != failClosedRetry {
		t.Errorf("Take = %+v, want denied with RetryAfter %v", r, failClosedRetry)
	}
	if err := l.Wait(context.Background(), "k"); !errors.Is(err, errRedis) {
		t.Errorf("Wait = %v, want %v", err, errRedis)
	}
}

func TestErrorHandlerSkipsCancelledContext(t *testing.T) {
	called := false
	client := &fakeClient{err: context.Canceled}
	l := NewRedisSlidingWindow(client, "p", 5, time.Second, WithErrorHandler(func(error) { called = true }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.WaitN(ctx, "k", 1); !errors.Is(err, context.Canceled) {
		t.Errorf("WaitN = %v, want context.Canceled", err)
	}
	l.TakeNCtx(ctx, "k", 1)
	if called {
		t.Error("error handler called for a cancelled context")
	}
}

func TestInvalidNSkipsRedis(t *testing.T) {
	client := &fakeClient{}
	l := NewRedisFixedWindow(client, "p", 5, time.Second)

	for _, n := range []int{0, -1, 6} {
		if l.AllowN("k", n) {
			t.Errorf("AllowN(%d) allowed", n)
		}
		if r := l.CheckN("k", n); r.Allowed {
			t.Errorf("CheckN(%d) allowed", n)
		}
	}
	if err := l.WaitN(context.Background(), "k", 0); !errors.Is(err, ratelimit.ErrInvalidN) {
		t.Errorf("WaitN(0) = %v, want ErrInvalidN", err)
	}
	if err := l.WaitN(context.Background(), "k", 6); !errors.Is(err, ratelimit.ErrExceedsLimit) {
		t.Errorf("WaitN(6) = %v, want ErrExceedsLimit", err)
	}
	if client.calls != 0 {
		t.Errorf("Redis called %d times for invalid n", client.calls)
	}
}

func TestReplyParsing(t *testing.T) {
	resetAt := time.Now().Add(time.Second).UnixMilli()
	tests := []struct {
		name  string
		reply any
		want  ratelimit.Result
	}{
		{
			name:  "int64 reply",
			reply: []any{int64(0), int64(2), int64(150), resetAt},
			want: ratelimit.Result{
				Limit: 5, Remaining: 2, RetryAfter: 150 * time.Millisecond,
				ResetAt: time.UnixMilli(resetAt),
			},
		},
		{
			name:  "string reply from custom client",
			reply: []any{"1", "4", "0", "0"},
			want:  ratelimit.Result{Allowed: true, Limit: 5, Remaining: 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewRedisFixedWindow(&fakeClient{reply: tt.reply}, "p", 5, time.Second)
			if got := l.Take("k"); got != tt.want {
				t.Errorf("Take = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMalformedReplyFailsOpen(t *testing.T) {
	var reported error
	l := NewRedisFixedWindow(&fakeClient{reply: []any{int64(1)}}, "p", 5, time.Second,
		WithErrorHandler(func(err error) { reported = err }))

	if r := l.Take("k"); !r.Allowed {
		t.Errorf("Take = %+v, want allowed (fail open)", r)
	}
	if reported == nil {
		t.Error("malformed reply was not reported")
	}
}

func TestKeysAreHashTagged(t *testing.T) {
	client := &fakeClient{reply: []any{int64(1), int64(0), int64(0), int64(0)}}
	l := NewRedisTokenBucket(client, "app", 1, 1)

	l.Take("user:1")
	if want := "app:tb:{user:1}"; len(client.keys) != 1 || client.keys[0] != want {
		t.Errorf("Eval keys = %v, want [%s]", client.keys, want)
	}
	l.Reset("user:1")
	if want := "app:tb:{user:1}"; len(client.keys) != 1 || client.keys[0] != want {
		t.Errorf("Del keys = %v, want [%s]", client.keys, want)
	}
}

func TestConstructorPanics(t *testing.T) {
	client := &fakeClient{}
	tests := map[string]func(){
		"nil client":        func() { NewRedisFixedWindow(nil, "p", 1, time.Second) },
		"zero limit":        func() { NewRedisSlidingWindow(client, "p", 0, time.Second) },
		"sub-ms window":     func() { NewRedisFixedWindow(client, "p", 1, time.Microsecond) },
		"zero rate":         func() { NewRedisTokenBucket(client, "p", 0, 1) },
		"zero burst":        func() { NewRedisTokenBucket(client, "p", 1, 0) },
		"zero per duration": func() { NewRedisTokenBucketPerDuration(client, "p", 1, 0, 1) },
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			fn()
		})
	}
}
