package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestCollector_RecordAllowed(t *testing.T) {
	c := NewCollector()

	c.RecordAllowed("api")
	c.RecordAllowed("api")
	c.RecordAllowed("api")

	stats := c.GetStats("api")
	if stats.Allowed != 3 {
		t.Errorf("expected Allowed=3, got %d", stats.Allowed)
	}
}

func TestCollector_RecordDenied(t *testing.T) {
	c := NewCollector()

	c.RecordDenied("api")
	c.RecordDenied("api")
	c.RecordDenied("api")
	c.RecordDenied("api")

	stats := c.GetStats("api")
	if stats.Denied != 4 {
		t.Errorf("expected Denied=4, got %d", stats.Denied)
	}
}

func TestCollector_RecordError(t *testing.T) {
	c := NewCollector()

	c.RecordError("api")
	c.RecordError("api")

	stats := c.GetStats("api")
	if stats.Errors != 2 {
		t.Errorf("expected Errors=2, got %d", stats.Errors)
	}
}

func TestCollector_GetStats_Empty(t *testing.T) {
	c := NewCollector()

	stats := c.GetStats("nonexistent")
	if stats.Allowed != 0 {
		t.Errorf("expected Allowed=0 for unknown name, got %d", stats.Allowed)
	}
	if stats.Denied != 0 {
		t.Errorf("expected Denied=0 for unknown name, got %d", stats.Denied)
	}
	if stats.Errors != 0 {
		t.Errorf("expected Errors=0 for unknown name, got %d", stats.Errors)
	}
}

func TestCollector_GetAllStats(t *testing.T) {
	c := NewCollector()

	c.RecordAllowed("api")
	c.RecordAllowed("api")
	c.RecordDenied("api")

	c.RecordAllowed("web")
	c.RecordDenied("web")
	c.RecordDenied("web")
	c.RecordError("web")

	all := c.GetAllStats()

	if len(all) != 2 {
		t.Errorf("expected 2 entries in GetAllStats, got %d", len(all))
	}

	apiStats, ok := all["api"]
	if !ok {
		t.Errorf("expected 'api' entry in GetAllStats")
	} else {
		if apiStats.Allowed != 2 {
			t.Errorf("api: expected Allowed=2, got %d", apiStats.Allowed)
		}
		if apiStats.Denied != 1 {
			t.Errorf("api: expected Denied=1, got %d", apiStats.Denied)
		}
		if apiStats.Errors != 0 {
			t.Errorf("api: expected Errors=0, got %d", apiStats.Errors)
		}
	}

	webStats, ok := all["web"]
	if !ok {
		t.Errorf("expected 'web' entry in GetAllStats")
	} else {
		if webStats.Allowed != 1 {
			t.Errorf("web: expected Allowed=1, got %d", webStats.Allowed)
		}
		if webStats.Denied != 2 {
			t.Errorf("web: expected Denied=2, got %d", webStats.Denied)
		}
		if webStats.Errors != 1 {
			t.Errorf("web: expected Errors=1, got %d", webStats.Errors)
		}
	}
}

func TestCollector_Reset(t *testing.T) {
	c := NewCollector()

	c.RecordAllowed("api")
	c.RecordAllowed("api")
	c.RecordDenied("api")
	c.RecordError("api")

	c.Reset("api")

	stats := c.GetStats("api")
	if stats.Allowed != 0 {
		t.Errorf("expected Allowed=0 after reset, got %d", stats.Allowed)
	}
	if stats.Denied != 0 {
		t.Errorf("expected Denied=0 after reset, got %d", stats.Denied)
	}
	if stats.Errors != 0 {
		t.Errorf("expected Errors=0 after reset, got %d", stats.Errors)
	}
}

func TestCollector_ResetAll(t *testing.T) {
	c := NewCollector()

	c.RecordAllowed("api")
	c.RecordDenied("web")
	c.RecordError("db")

	c.ResetAll()

	all := c.GetAllStats()
	if len(all) != 0 {
		t.Errorf("expected 0 entries after ResetAll, got %d", len(all))
	}
}

func TestCollector_Concurrent(t *testing.T) {
	c := NewCollector()

	var wg sync.WaitGroup
	goroutines := 100
	iterations := 50

	wg.Add(goroutines * 3)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.RecordAllowed("concurrent")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.RecordDenied("concurrent")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				c.RecordError("concurrent")
			}
		}()
	}

	wg.Wait()

	stats := c.GetStats("concurrent")

	expectedAllowed := uint64(goroutines * iterations)
	if stats.Allowed != expectedAllowed {
		t.Errorf("expected Allowed=%d, got %d", expectedAllowed, stats.Allowed)
	}

	expectedDenied := uint64(goroutines * iterations)
	if stats.Denied != expectedDenied {
		t.Errorf("expected Denied=%d, got %d", expectedDenied, stats.Denied)
	}

	expectedErrors := uint64(goroutines * iterations)
	if stats.Errors != expectedErrors {
		t.Errorf("expected Errors=%d, got %d", expectedErrors, stats.Errors)
	}
}

func TestCollector_LastUpdate(t *testing.T) {
	c := NewCollector()

	before := time.Now().Add(-time.Second)
	c.RecordAllowed("api")
	after := time.Now().Add(time.Second)

	stats := c.GetStats("api")

	if stats.LastUpdate.Before(before) {
		t.Errorf("LastUpdate %v is before the recording time %v", stats.LastUpdate, before)
	}
	if stats.LastUpdate.After(after) {
		t.Errorf("LastUpdate %v is after the recording time %v", stats.LastUpdate, after)
	}
}
