package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/KARTIKrocks/go-ratelimit"
	"github.com/KARTIKrocks/go-ratelimit/redisstore"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Get Redis URL from environment or use default
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "localhost:6379"
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         redisURL,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully")

	// Create distributed rate limiter
	adapter := redisstore.NewRedisClientAdapter(client)
	limiter := redisstore.NewRedisTokenBucket(
		adapter,
		"myapp", // key prefix
		10.0,    // 10 requests per second
		20,      // burst of 20
	)

	// Create HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", dataHandler)
	mux.HandleFunc("/stats", statsHandler(limiter))

	// Apply rate limiting middleware
	handler := ratelimit.Middleware(limiter,
		ratelimit.WithKeyFunc(ratelimit.IPKeyFunc),
		ratelimit.WithOnLimitReached(ratelimit.JSONOnLimitReached),
	)(mux)

	// Start server
	log.Println("Distributed rate limiting server on :8080")
	log.Println("Rate limit shared across all instances via Redis")
	log.Println("Try: curl http://localhost:8080/api/data")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(err)
	}
}

func dataHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"Data from distributed system","timestamp":"%s"}`,
		time.Now().Format(time.RFC3339))
}

func statsHandler(limiter *redisstore.RedisTokenBucket) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		ip := ratelimit.GetClientIP(r)

		// Check current state
		result := limiter.Check(ip)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"ip": "%s",
			"limit": %d,
			"remaining": %d,
			"allowed": %v
		}`, ip, result.Limit, result.Remaining, result.Allowed)
	}
}
