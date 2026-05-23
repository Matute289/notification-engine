package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const templateCachePrefix = "notif:tmpl:"

// TemplateCache wraps any port.TemplateRepository with a Redis read-through
// write-through cache. It acts as the shared L2 cache; the renderer's
// in-process map is the L1 (hot) layer.
//
// When a CircuitBreaker is configured and Redis is unhealthy, Get and Create
// bypass the Redis layer entirely and fall through to the underlying repository
// (MongoDB). This avoids connection-timeout latency on every request during
// an outage.
type TemplateCache struct {
	repo port.TemplateRepository
	rdb  *goredis.Client
	ttl  time.Duration
	cb   *CircuitBreaker
}

// NewTemplateCache creates a TemplateCache. Pass a non-nil cb to enable
// circuit breaker protection: on Redis errors the cache bypasses Redis and
// queries the underlying repository directly.
func NewTemplateCache(repo port.TemplateRepository, rdb *goredis.Client, ttl time.Duration, cb *CircuitBreaker) *TemplateCache {
	return &TemplateCache{repo: repo, rdb: rdb, ttl: ttl, cb: cb}
}

var _ port.TemplateRepository = (*TemplateCache)(nil)

func (c *TemplateCache) Create(ctx context.Context, t domain.Template) error {
	if err := c.repo.Create(ctx, t); err != nil {
		return err
	}
	if c.cb == nil || c.cb.Allow() {
		c.set(ctx, t)
	}
	return nil
}

func (c *TemplateCache) Get(ctx context.Context, id uuid.UUID) (domain.Template, error) {
	// redisOK tracks whether it is safe to attempt (or write back to) Redis in
	// this call. A single Allow() check at the top avoids a state-transition
	// race between the read and the write-back.
	redisOK := c.cb == nil || c.cb.Allow()
	if redisOK {
		key := templateCachePrefix + id.String()
		if raw, err := c.rdb.Get(ctx, key).Bytes(); err == nil {
			var t domain.Template
			if json.Unmarshal(raw, &t) == nil {
				if c.cb != nil {
					c.cb.RecordSuccess()
				}
				return t, nil
			}
		} else if c.cb != nil && isRedisError(err) {
			c.cb.RecordFailure()
			redisOK = false // skip write-back after a Redis failure
		}
		// redis.Nil (cache miss) falls through normally without recording a failure.
	}
	t, err := c.repo.Get(ctx, id)
	if err != nil {
		return t, err
	}
	if redisOK {
		c.set(ctx, t)
	}
	return t, nil
}

func (c *TemplateCache) set(ctx context.Context, t domain.Template) {
	raw, err := json.Marshal(t)
	if err != nil {
		return
	}
	if setErr := c.rdb.Set(ctx, templateCachePrefix+t.ID.String(), raw, c.ttl).Err(); setErr != nil {
		if c.cb != nil && isRedisError(setErr) {
			c.cb.RecordFailure()
		}
	} else if c.cb != nil {
		c.cb.RecordSuccess()
	}
}
