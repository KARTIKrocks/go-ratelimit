package metrics

import (
	ratelimit "github.com/KARTIKrocks/go-ratelimit"
)

// Instrumented wraps a limiter and collects metrics.
type Instrumented struct {
	limiter   ratelimit.KeyedResultLimiter
	name      string
	collector *Collector
}

// NewInstrumented creates a new instrumented limiter using the global collector.
func NewInstrumented(limiter ratelimit.KeyedResultLimiter, name string) *Instrumented {
	return NewInstrumentedWithCollector(limiter, name, globalCollector)
}

// NewInstrumentedWithCollector creates a new instrumented limiter with a custom collector.
func NewInstrumentedWithCollector(limiter ratelimit.KeyedResultLimiter, name string, collector *Collector) *Instrumented {
	return &Instrumented{
		limiter:   limiter,
		name:      name,
		collector: collector,
	}
}

// Check checks if a request is allowed without consuming tokens.
// This is a read-only peek and does not record metrics.
func (i *Instrumented) Check(key string) ratelimit.Result {
	return i.limiter.Check(key)
}

// CheckN checks if n requests are allowed without consuming tokens.
// This is a read-only peek and does not record metrics.
func (i *Instrumented) CheckN(key string, n int) ratelimit.Result {
	return i.limiter.CheckN(key, n)
}

// Take consumes a token and returns the result.
func (i *Instrumented) Take(key string) ratelimit.Result {
	result := i.limiter.Take(key)
	if result.Allowed {
		i.collector.RecordAllowed(i.name)
	} else {
		i.collector.RecordDenied(i.name)
	}
	return result
}

// TakeN consumes n tokens and returns the result.
func (i *Instrumented) TakeN(key string, n int) ratelimit.Result {
	result := i.limiter.TakeN(key, n)
	if result.Allowed {
		i.collector.RecordAllowed(i.name)
	} else {
		i.collector.RecordDenied(i.name)
	}
	return result
}

// GetStats returns statistics for this limiter.
func (i *Instrumented) GetStats() Stats {
	return i.collector.GetStats(i.name)
}

// SimpleInstrumented wraps a simple (non-keyed) limiter.
type SimpleInstrumented struct {
	limiter   ratelimit.ResultLimiter
	name      string
	collector *Collector
}

// NewSimpleInstrumented creates an instrumented wrapper for non-keyed limiters.
func NewSimpleInstrumented(limiter ratelimit.ResultLimiter, name string) *SimpleInstrumented {
	return &SimpleInstrumented{
		limiter:   limiter,
		name:      name,
		collector: globalCollector,
	}
}

// Check checks if a request is allowed without consuming tokens.
// This is a read-only peek and does not record metrics.
func (s *SimpleInstrumented) Check() ratelimit.Result {
	return s.limiter.Check()
}

// CheckN checks if n requests are allowed without consuming tokens.
// This is a read-only peek and does not record metrics.
func (s *SimpleInstrumented) CheckN(n int) ratelimit.Result {
	return s.limiter.CheckN(n)
}

// Take consumes a token and returns the result.
func (s *SimpleInstrumented) Take() ratelimit.Result {
	result := s.limiter.Take()
	if result.Allowed {
		s.collector.RecordAllowed(s.name)
	} else {
		s.collector.RecordDenied(s.name)
	}
	return result
}

// TakeN consumes n tokens and returns the result.
func (s *SimpleInstrumented) TakeN(n int) ratelimit.Result {
	result := s.limiter.TakeN(n)
	if result.Allowed {
		s.collector.RecordAllowed(s.name)
	} else {
		s.collector.RecordDenied(s.name)
	}
	return result
}

// GetStats returns statistics for this limiter.
func (s *SimpleInstrumented) GetStats() Stats {
	return s.collector.GetStats(s.name)
}
