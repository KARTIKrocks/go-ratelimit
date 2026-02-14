package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Stats holds rate limiter statistics.
type Stats struct {
	Allowed    uint64
	Denied     uint64
	Errors     uint64
	LastUpdate time.Time
}

// Collector collects rate limiter metrics.
type Collector struct {
	stats map[string]*limiterStats
	mu    sync.RWMutex
}

type limiterStats struct {
	allowed atomic.Uint64
	denied  atomic.Uint64
	errors  atomic.Uint64
	updated atomic.Int64
}

var (
	globalCollector = &Collector{
		stats: make(map[string]*limiterStats),
	}

	// Prometheus metrics
	promRequestsTotal   *prometheus.CounterVec
	promRequestsAllowed *prometheus.CounterVec
	promRequestsDenied  *prometheus.CounterVec
	promErrors          *prometheus.CounterVec
)

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		stats: make(map[string]*limiterStats),
	}
}

// getStats gets or creates stats for a name.
func (c *Collector) getStats(name string) *limiterStats {
	c.mu.RLock()
	stats, ok := c.stats[name]
	c.mu.RUnlock()

	if ok {
		return stats
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	stats, ok = c.stats[name]
	if ok {
		return stats
	}

	stats = &limiterStats{}
	stats.updated.Store(time.Now().Unix())
	c.stats[name] = stats
	return stats
}

// RecordAllowed records an allowed request.
func (c *Collector) RecordAllowed(name string) {
	stats := c.getStats(name)
	stats.allowed.Add(1)
	stats.updated.Store(time.Now().Unix())

	if promRequestsAllowed != nil {
		promRequestsAllowed.WithLabelValues(name).Inc()
		promRequestsTotal.WithLabelValues(name, "allowed").Inc()
	}
}

// RecordDenied records a denied request.
func (c *Collector) RecordDenied(name string) {
	stats := c.getStats(name)
	stats.denied.Add(1)
	stats.updated.Store(time.Now().Unix())

	if promRequestsDenied != nil {
		promRequestsDenied.WithLabelValues(name).Inc()
		promRequestsTotal.WithLabelValues(name, "denied").Inc()
	}
}

// RecordError records an error.
func (c *Collector) RecordError(name string) {
	stats := c.getStats(name)
	stats.errors.Add(1)
	stats.updated.Store(time.Now().Unix())

	if promErrors != nil {
		promErrors.WithLabelValues(name).Inc()
	}
}

// GetStats returns statistics for a named limiter.
func (c *Collector) GetStats(name string) Stats {
	stats := c.getStats(name)
	return Stats{
		Allowed:    stats.allowed.Load(),
		Denied:     stats.denied.Load(),
		Errors:     stats.errors.Load(),
		LastUpdate: time.Unix(stats.updated.Load(), 0),
	}
}

// GetAllStats returns statistics for all limiters.
func (c *Collector) GetAllStats() map[string]Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]Stats, len(c.stats))
	for name, stats := range c.stats {
		result[name] = Stats{
			Allowed:    stats.allowed.Load(),
			Denied:     stats.denied.Load(),
			Errors:     stats.errors.Load(),
			LastUpdate: time.Unix(stats.updated.Load(), 0),
		}
	}
	return result
}

// Reset resets statistics for a named limiter.
func (c *Collector) Reset(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.stats, name)
}

// ResetAll resets all statistics.
func (c *Collector) ResetAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = make(map[string]*limiterStats)
}

// Global functions using the default collector

// RecordAllowed records an allowed request in the global collector.
func RecordAllowed(name string) {
	globalCollector.RecordAllowed(name)
}

// RecordDenied records a denied request in the global collector.
func RecordDenied(name string) {
	globalCollector.RecordDenied(name)
}

// RecordError records an error in the global collector.
func RecordError(name string) {
	globalCollector.RecordError(name)
}

// GetStats returns statistics from the global collector.
func GetStats(name string) Stats {
	return globalCollector.GetStats(name)
}

// GetAllStats returns all statistics from the global collector.
func GetAllStats() map[string]Stats {
	return globalCollector.GetAllStats()
}

// RegisterPrometheus registers Prometheus metrics.
func RegisterPrometheus(reg prometheus.Registerer) {
	promRequestsTotal = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimit_requests_total",
			Help: "Total number of rate limit checks",
		},
		[]string{"limiter", "result"},
	)

	promRequestsAllowed = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimit_requests_allowed_total",
			Help: "Total number of allowed requests",
		},
		[]string{"limiter"},
	)

	promRequestsDenied = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimit_requests_denied_total",
			Help: "Total number of denied requests",
		},
		[]string{"limiter"},
	)

	promErrors = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Name: "ratelimit_errors_total",
			Help: "Total number of rate limiter errors",
		},
		[]string{"limiter"},
	)
}
