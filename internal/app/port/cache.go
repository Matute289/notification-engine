package port

import (
	"context"
	"time"
)

// RateLimiter enforces a per-key request cap inside a sliding/fixed window.
type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// Deduper provides best-effort idempotency by claiming a one-time key. The
// canonical source of truth is still the database's unique index on event_id;
// this exists to avoid a DB round-trip on the hot path.
type Deduper interface {
	Claim(ctx context.Context, eventID string, ttl time.Duration) (claimed bool, err error)
}
