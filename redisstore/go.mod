module github.com/KARTIKrocks/go-ratelimit/redisstore

go 1.27

require (
	github.com/KARTIKrocks/go-ratelimit v0.0.0
	github.com/redis/go-redis/v9 v9.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
)

replace github.com/KARTIKrocks/go-ratelimit => ../
