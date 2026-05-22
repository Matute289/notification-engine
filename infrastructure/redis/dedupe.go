package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/redis/go-redis/v9"
)

// Deduper backs the at-least-once-with-dedupe contract. The first arrival
// wins; later arrivals collapse into the same notification.
type Deduper struct{ c *redis.Client }

func NewDeduper(c *redis.Client) *Deduper { return &Deduper{c: c} }

var _ port.Deduper = (*Deduper)(nil)

func (d *Deduper) Claim(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	ok, err := d.c.SetNX(ctx, "notif:dedupe:"+eventID, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("dedupe setnx: %w", err)
	}
	return ok, nil
}
