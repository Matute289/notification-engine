// Package redis bundles Redis-backed implementations of port.RateLimiter and
// port.Deduper. The same *redis.Client is reused for both — they share TTLs
// and connection budget.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Connect opens a Redis client and pings it to fail fast.
func Connect(ctx context.Context, addr string, db int) (*redis.Client, error) {
	c := redis.NewClient(&redis.Options{Addr: addr, DB: db})
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return c, nil
}
