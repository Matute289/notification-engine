package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRedis(t *testing.T) (*goredis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = c.Close() })
	return c, mr
}

func TestRateLimiter_BurstUpToCapacity(t *testing.T) {
	c, _ := newRedis(t)
	rl := NewRateLimiter(c)
	for i := 0; i < 5; i++ {
		ok, err := rl.Allow(context.Background(), "k", 5, time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "burst call %d should be allowed", i)
	}
	ok, err := rl.Allow(context.Background(), "k", 5, time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "after burst the bucket is empty")
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	c, mr := newRedis(t)
	rl := NewRateLimiter(c)
	start := time.Unix(1700000000, 0)
	mr.SetTime(start)

	// Drain the bucket entirely (capacity = 2, rate = 2/min).
	for i := 0; i < 2; i++ {
		_, _ = rl.Allow(context.Background(), "k", 2, time.Minute)
	}
	denied, err := rl.Allow(context.Background(), "k", 2, time.Minute)
	require.NoError(t, err)
	require.False(t, denied)

	// Advance the simulated clock enough to refill ≥1 token.
	mr.SetTime(start.Add(40 * time.Second))
	mr.FastForward(40 * time.Second)
	allowed, err := rl.Allow(context.Background(), "k", 2, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed, "after refill we should allow at least one call")
}

func TestRateLimiter_KeysAreIndependent(t *testing.T) {
	c, _ := newRedis(t)
	rl := NewRateLimiter(c)
	_, _ = rl.Allow(context.Background(), "a", 1, time.Minute)
	ok, err := rl.Allow(context.Background(), "b", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRateLimiter_NoLimit(t *testing.T) {
	c, _ := newRedis(t)
	rl := NewRateLimiter(c)
	for i := 0; i < 100; i++ {
		ok, err := rl.Allow(context.Background(), "k", 0, time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "limit=0 disables the limiter")
	}
}

func TestDeduper_FirstClaimWins(t *testing.T) {
	c, _ := newRedis(t)
	d := NewDeduper(c)
	first, _ := d.Claim(context.Background(), "evt-42", time.Minute)
	require.True(t, first)
	second, _ := d.Claim(context.Background(), "evt-42", time.Minute)
	require.False(t, second)
}

func TestDeduper_TTLReleasesClaim(t *testing.T) {
	c, mr := newRedis(t)
	d := NewDeduper(c)
	_, _ = d.Claim(context.Background(), "evt-x", time.Second)
	mr.FastForward(2 * time.Second)
	again, err := d.Claim(context.Background(), "evt-x", time.Second)
	require.NoError(t, err)
	require.True(t, again)
}

// --- TemplateCache tests ---

type stubTemplateRepo struct {
	tpls  map[uuid.UUID]domain.Template
	calls int
}

func (s *stubTemplateRepo) Create(_ context.Context, t domain.Template) error {
	s.tpls[t.ID] = t
	s.calls++
	return nil
}

func (s *stubTemplateRepo) Get(_ context.Context, id uuid.UUID) (domain.Template, error) {
	s.calls++
	t, ok := s.tpls[id]
	if !ok {
		return domain.Template{}, domain.ErrNotFound
	}
	return t, nil
}

func seedTemplate(id uuid.UUID) domain.Template {
	t, _ := domain.NewTemplate(id, "welcome", domain.ChannelEmail, "en",
		"Hello {{.Name}}", "Welcome {{.Name}}", nil, 1, time.Now())
	return t
}

func TestTemplateCache_MissPopulatesCache(t *testing.T) {
	c, _ := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cache := NewTemplateCache(stub, c, time.Minute)

	// First call: cache miss, should hit the underlying repo.
	got, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, 1, stub.calls)

	// Second call: should be served from Redis without hitting the repo again.
	got2, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got2.ID)
	require.Equal(t, 1, stub.calls, "repo should not be called again on cache hit")
}

func TestTemplateCache_CreateWritesToCache(t *testing.T) {
	c, _ := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	tmpl := seedTemplate(id)

	cache := NewTemplateCache(stub, c, time.Minute)

	require.NoError(t, cache.Create(context.Background(), tmpl))
	require.Equal(t, 1, stub.calls)

	// Get should be served from cache — repo call count stays at 1.
	got, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, 1, stub.calls, "Get after Create should hit cache, not repo")
}

func TestTemplateCache_TTLEviction(t *testing.T) {
	c, mr := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cache := NewTemplateCache(stub, c, time.Second)

	_, _ = cache.Get(context.Background(), id) // populate cache
	mr.FastForward(2 * time.Second)            // expire the entry

	_, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, 2, stub.calls, "repo must be called again after TTL expiry")
}
