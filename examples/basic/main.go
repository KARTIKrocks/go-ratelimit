package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/KARTIKrocks/go-ratelimit"
)

func main() {
	// Create a keyed token bucket limiter
	// 10 requests per second, burst of 20, cleanup every minute
	limiter := ratelimit.NewKeyedTokenBucket(10.0, 20, time.Minute)
	defer limiter.Close()

	// Create HTTP mux
	mux := http.NewServeMux()

	// Add routes
	mux.HandleFunc("/api/data", dataHandler)
	mux.HandleFunc("/api/expensive", expensiveHandler)
	mux.HandleFunc("/health", healthHandler)

	// Apply rate limiting middleware
	handler := ratelimit.Middleware(limiter,
		ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
		ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
		ratelimit.WithSkipFunc(ratelimit.SkipHealthChecks),
	)(mux)

	// Start server
	log.Println("Server starting on :8080")
	log.Println("Try: curl http://localhost:8080/api/data")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Data retrieved successfully","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

func expensiveHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate expensive operation
	time.Sleep(100 * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Expensive operation completed","timestamp":"%s"}`, time.Now().Format(time.RFC3339))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}
