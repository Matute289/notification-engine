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
//
// When a CircuitBreaker is configured and Redis is unhealthy, Claim fails open
// (returns true, nil) so submissions are never blocked. The database unique
// index on event_id remains the authoritative backstop for correctness.
type Deduper struct {
	c  *redis.Client
	cb *CircuitBreaker
}

// NewDeduper creates a Deduper. Pass a non-nil cb to enable circuit breaker
// protection: on Redis errors the deduper fails open rather than surfacing
// the error to callers.
func NewDeduper(c *redis.Client, cb *CircuitBreaker) *Deduper { return &Deduper{c: c, cb: cb} }

var _ port.Deduper = (*Deduper)(nil)

func (d *Deduper) Claim(ctx context.Context, eventID string, ttl time.Duration) (bool, error) {
	if d.cb != nil && !d.cb.Allow() {
		return true, nil // circuit open → fail-open (DB unique index is backstop)
	}
	ok, err := d.c.SetNX(ctx, "notif:dedupe:"+eventID, "1", ttl).Result()
	if err != nil {
		if d.cb != nil && isRedisError(err) {
			d.cb.RecordFailure()
			return true, nil // fail-open; don't surface the infrastructure error
		}
		return false, fmt.Errorf("dedupe setnx: %w", err)
	}
	if d.cb != nil {
		d.cb.RecordSuccess()
	}
	return ok, nil
}
