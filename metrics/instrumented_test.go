package metrics_test

import (
	"testing"
	"time"

	ratelimit "github.com/KARTIKrocks/go-ratelimit"
	"github.com/KARTIKrocks/go-ratelimit/metrics"
)

func TestInstrumented_RecordsAllowed(t *testing.T) {
	limiter := ratelimit.NewKeyedTokenBucket(100.0, 10, time.Minute)
	defer limiter.Close()

	inst := metrics.NewInstrumented(limiter, "test-allowed")

	for i := 0; i < 5; i++ {
		result := inst.Take("user1")
		if !result.Allowed {
			t.Errorf("request %d: expected Allowed=true, got false", i)
		}
	}

	stats := inst.GetStats()
	if stats.Allowed != 5 {
		t.Errorf("expected stats.Allowed=5, got %d", stats.Allowed)
	}
	if stats.Denied != 0 {
		t.Errorf("expected stats.Denied=0, got %d", stats.Denied)
	}
}

func TestInstrumented_RecordsDenied(t *testing.T) {
	limiter := ratelimit.NewKeyedTokenBucket(100.0, 10, time.Minute)
	defer limiter.Close()

	inst := metrics.NewInstrumented(limiter, "test-denied")

	// Exhaust all 10 tokens in the bucket
	for i := 0; i < 10; i++ {
		inst.Take("user1")
	}

	// Next requests should be denied
	for i := 0; i < 3; i++ {
		result := inst.Take("user1")
		if result.Allowed {
			t.Errorf("denied request %d: expected Allowed=false, got true", i)
		}
	}

	stats := inst.GetStats()
	if stats.Denied != 3 {
		t.Errorf("expected stats.Denied=3, got %d", stats.Denied)
	}
}

func TestInstrumented_Check(t *testing.T) {
	limiter := ratelimit.NewKeyedTokenBucket(100.0, 10, time.Minute)
	defer limiter.Close()

	inst := metrics.NewInstrumented(limiter, "test-check")

	result := inst.Check("user1")
	if !result.Allowed {
		t.Errorf("expected Check to return Allowed=true, got false")
	}

	// Check is a read-only peek and should NOT record metrics
	stats := inst.GetStats()
	if stats.Allowed != 0 {
		t.Errorf("expected stats.Allowed=0 after Check (read-only), got %d", stats.Allowed)
	}
}

func TestSimpleInstrumented_RecordsAllowed(t *testing.T) {
	limiter := ratelimit.NewTokenBucket(100.0, 10)

	inst := metrics.NewSimpleInstrumented(limiter, "simple-allowed")

	for i := 0; i < 5; i++ {
		result := inst.Take()
		if !result.Allowed {
			t.Errorf("request %d: expected Allowed=true, got false", i)
		}
	}

	stats := inst.GetStats()
	if stats.Allowed != 5 {
		t.Errorf("expected stats.Allowed=5, got %d", stats.Allowed)
	}
	if stats.Denied != 0 {
		t.Errorf("expected stats.Denied=0, got %d", stats.Denied)
	}
}

func TestSimpleInstrumented_RecordsDenied(t *testing.T) {
	limiter := ratelimit.NewTokenBucket(100.0, 10)

	inst := metrics.NewSimpleInstrumented(limiter, "simple-denied")

	// Exhaust all 10 tokens in the bucket
	for i := 0; i < 10; i++ {
		inst.Take()
	}

	// Next requests should be denied
	for i := 0; i < 3; i++ {
		result := inst.Take()
		if result.Allowed {
			t.Errorf("denied request %d: expected Allowed=false, got true", i)
		}
	}

	stats := inst.GetStats()
	if stats.Denied != 3 {
		t.Errorf("expected stats.Denied=3, got %d", stats.Denied)
	}
}
