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
type TemplateCache struct {
	repo port.TemplateRepository
	rdb  *goredis.Client
	ttl  time.Duration
}

func NewTemplateCache(repo port.TemplateRepository, rdb *goredis.Client, ttl time.Duration) *TemplateCache {
	return &TemplateCache{repo: repo, rdb: rdb, ttl: ttl}
}

var _ port.TemplateRepository = (*TemplateCache)(nil)

func (c *TemplateCache) Create(ctx context.Context, t domain.Template) error {
	if err := c.repo.Create(ctx, t); err != nil {
		return err
	}
	c.set(ctx, t)
	return nil
}

func (c *TemplateCache) Get(ctx context.Context, id uuid.UUID) (domain.Template, error) {
	key := templateCachePrefix + id.String()
	if raw, err := c.rdb.Get(ctx, key).Bytes(); err == nil {
		var t domain.Template
		if jsonErr := json.Unmarshal(raw, &t); jsonErr == nil {
			return t, nil
		}
	}
	t, err := c.repo.Get(ctx, id)
	if err != nil {
		return t, err
	}
	c.set(ctx, t)
	return t, nil
}

func (c *TemplateCache) set(ctx context.Context, t domain.Template) {
	raw, err := json.Marshal(t)
	if err != nil {
		return
	}
	c.rdb.Set(ctx, templateCachePrefix+t.ID.String(), raw, c.ttl)
}
