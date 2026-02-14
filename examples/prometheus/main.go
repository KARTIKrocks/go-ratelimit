package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/KARTIKrocks/go-ratelimit"
	"github.com/KARTIKrocks/go-ratelimit/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// Register Prometheus metrics
	metrics.RegisterPrometheus(prometheus.DefaultRegisterer)

	// Create base limiter
	baseLimiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
	defer baseLimiter.Close()

	// Wrap with metrics collection
	limiter := metrics.NewInstrumented(baseLimiter, "api")

	// Create HTTP mux
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/data", dataHandler)
	mux.HandleFunc("/api/status", statusHandler(limiter))

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Apply rate limiting middleware
	handler := ratelimit.Middleware(limiter,
		ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
		ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
	)(mux)

	log.Println("Server with Prometheus metrics on :8080")
	log.Println("API endpoint: http://localhost:8080/api/data")
	log.Println("Metrics endpoint: http://localhost:8080/metrics")
	log.Println("Status endpoint: http://localhost:8080/api/status")

	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Data retrieved","timestamp":"%s"}`,
		time.Now().Format(time.RFC3339))
}

func statusHandler(limiter *metrics.Instrumented) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := limiter.GetStats()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"limiter": "api",
			"allowed": %d,
			"denied": %d,
			"errors": %d,
			"last_update": "%s"
		}`, stats.Allowed, stats.Denied, stats.Errors,
			stats.LastUpdate.Format(time.RFC3339))
	}
}
