// Package redis bundles Redis-backed implementations of port.RateLimiter and
// port.Deduper. The same *redis.Client is reused for both — they share TTLs
// and connection budget.
package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Connect opens a Redis client and pings it to fail fast.
// addr may be "host:port" or a full Redis URL ("redis://" / "rediss://"),
// which allows Render's connectionString property to be used directly.
func Connect(ctx context.Context, addr string, db int) (*redis.Client, error) {
	var opts *redis.Options
	if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
		var err error
		opts, err = redis.ParseURL(addr)
		if err != nil {
			return nil, fmt.Errorf("redis parse url: %w", err)
		}
	} else {
		opts = &redis.Options{Addr: addr, DB: db}
	}
	c := redis.NewClient(opts)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return c, nil
}
