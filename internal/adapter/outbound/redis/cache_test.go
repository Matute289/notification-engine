package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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
