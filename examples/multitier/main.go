package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/KARTIKrocks/go-ratelimit"
)

func main() {
	// Different limits for different endpoints
	publicLimiter := ratelimit.NewKeyedTokenBucket(
		100.0/60.0, // 100 requests per minute
		10,         // burst of 10
		time.Minute,
	)
	defer publicLimiter.Close()

	authLimiter := ratelimit.NewKeyedTokenBucket(
		1000.0/60.0, // 1000 requests per minute
		50,          // burst of 50
		time.Minute,
	)
	defer authLimiter.Close()

	expensiveLimiter := ratelimit.NewKeyedTokenBucket(
		10.0/60.0, // 10 requests per minute
		2,         // burst of 2
		time.Minute,
	)
	defer expensiveLimiter.Close()

	// Create path-based limiter
	pathLimiter := ratelimit.NewPathLimiter(publicLimiter)
	pathLimiter.Add("/api/auth", authLimiter)
	pathLimiter.Add("/api/expensive", expensiveLimiter)

	// Create mux
	mux := http.NewServeMux()
	mux.HandleFunc("/api/public", publicHandler)
	mux.HandleFunc("/api/auth", authHandler)
	mux.HandleFunc("/api/expensive", expensiveHandler)

	// Apply middleware
	handler := pathLimiter.Middleware(
		ratelimit.WithKeyFunc(getUserKey),
		ratelimit.WithOnLimitReached(customLimitReached),
	)(mux)

	log.Println("Multi-tier rate limiting server on :8080")
	log.Println("Public endpoint: 100/min, Auth: 1000/min, Expensive: 10/min")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

// getUserKey extracts user ID from header, falls back to IP
func getUserKey(r *http.Request) string {
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return "user:" + userID
	}
	return "ip:" + ratelimit.GetClientIP(r)
}

func customLimitReached(w http.ResponseWriter, r *http.Request, result ratelimit.Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	retryAfter := int(result.RetryAfter.Seconds()) + 1
	resetAt := result.ResetAt.Format(time.RFC3339)

	fmt.Fprintf(w, `{
		"error": "rate_limit_exceeded",
		"message": "Too many requests. Please slow down.",
		"limit": %d,
		"remaining": %d,
		"retry_after": %d,
		"reset_at": "%s"
	}`, result.Limit, result.Remaining, retryAfter, resetAt)
}

func publicHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Public endpoint","tier":"100 requests/min"}`)
}

func authHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Authenticated endpoint","tier":"1000 requests/min"}`)
}

func expensiveHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate expensive operation
	time.Sleep(100 * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Expensive operation","tier":"10 requests/min"}`)
}
