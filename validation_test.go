package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucket(0, 10)
}

func TestTokenBucket_PanicOnInvalidBurst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucket(10.0, 0)
}

func TestTokenBucketPerDuration_PanicOnInvalidPer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewTokenBucketPerDuration(10, 0, 10)
}

func TestKeyedTokenBucket_PanicOnInvalidRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucket(0, 10, time.Minute)
}

func TestKeyedTokenBucket_PanicOnInvalidBurst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucket(10.0, 0, time.Minute)
}

func TestKeyedTokenBucketPerDuration_PanicOnInvalidPer(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	NewKeyedTokenBucketPerDuration(10, 0, 10, time.Minute)
}
