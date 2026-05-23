package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// CircuitBreaker state-machine tests (no Redis needed)
// ---------------------------------------------------------------------------

func TestCircuitBreaker_InitiallyClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	require.Equal(t, "closed", cb.State())
	require.True(t, cb.Allow(), "new CB must allow requests")
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}
	require.Equal(t, "open", cb.State())
	require.False(t, cb.Allow(), "circuit is open: must reject requests")
}

func TestCircuitBreaker_SuccessResetsFailureCounter(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Minute)
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // reset counter — need 3 more failures to open
	cb.RecordFailure()
	cb.RecordFailure()
	require.Equal(t, "closed", cb.State(), "2 failures after reset should not open (threshold=3)")
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	cb.RecordFailure() // open immediately (threshold=1)
	require.False(t, cb.Allow(), "should be open")

	time.Sleep(30 * time.Millisecond)

	// First Allow after timeout transitions to half-open and returns true.
	require.True(t, cb.Allow(), "after timeout Allow should allow the probe")
}

func TestCircuitBreaker_HalfOpen_SuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(30 * time.Millisecond)

	require.True(t, cb.Allow()) // probe (transitions to half-open)
	cb.RecordSuccess()
	require.Equal(t, "closed", cb.State())
	require.True(t, cb.Allow(), "after recovery the circuit must be closed")
}

func TestCircuitBreaker_HalfOpen_FailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(30 * time.Millisecond)

	require.True(t, cb.Allow()) // probe
	cb.RecordFailure()          // probe fails → re-open immediately
	require.Equal(t, "open", cb.State())
	require.False(t, cb.Allow())
}

// ---------------------------------------------------------------------------
// isRedisError helper tests
// ---------------------------------------------------------------------------

func TestIsRedisError_NilIsNotAnError(t *testing.T) {
	require.False(t, isRedisError(nil))
}

func TestIsRedisError_RedisNilIsNotAnError(t *testing.T) {
	require.False(t, isRedisError(goredis.Nil))
}

func TestIsRedisError_ContextCanceledIsNotAnError(t *testing.T) {
	require.False(t, isRedisError(context.Canceled))
}

func TestIsRedisError_OtherErrorsAreInfraErrors(t *testing.T) {
	require.True(t, isRedisError(errors.New("connection refused")))
}

// ---------------------------------------------------------------------------
// RateLimiter + CircuitBreaker integration tests (miniredis)
// ---------------------------------------------------------------------------

func TestRateLimiter_CB_Open_FailsOpen(t *testing.T) {
	c, _ := newRedis(t)
	cb := NewCircuitBreaker(1, time.Minute)
	rl := NewRateLimiter(c, cb)

	cb.RecordFailure() // open circuit immediately (threshold=1)
	require.Equal(t, "open", cb.State())

	// Even with limit=1, the open circuit must allow all requests.
	for i := 0; i < 5; i++ {
		ok, err := rl.Allow(context.Background(), "k", 1, time.Minute)
		require.NoError(t, err)
		require.True(t, ok, "open circuit → fail-open (call %d)", i)
	}
}

func TestRateLimiter_CB_OpensAfterRedisErrors(t *testing.T) {
	c, mr := newRedis(t)
	cb := NewCircuitBreaker(3, time.Minute)
	rl := NewRateLimiter(c, cb)

	mr.SetError("ERR connection refused")

	// Each call should fail-open and record a failure; no error surfaced.
	for i := 0; i < 3; i++ {
		ok, err := rl.Allow(context.Background(), "k", 5, time.Minute)
		require.NoError(t, err, "Redis errors must not surface with CB configured (call %d)", i)
		require.True(t, ok, "should fail-open on Redis error (call %d)", i)
	}
	require.Equal(t, "open", cb.State(), "circuit must be open after threshold errors")

	// Clear the Redis error — CB is open so Redis is not called at all.
	mr.SetError("")
	ok, err := rl.Allow(context.Background(), "k", 5, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "circuit still open: must fail-open without hitting Redis")
}

func TestRateLimiter_CB_RecoveryRecloses(t *testing.T) {
	c, mr := newRedis(t)
	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	rl := NewRateLimiter(c, cb)

	// Open the circuit via a Redis error.
	mr.SetError("ERR down")
	_, _ = rl.Allow(context.Background(), "k", 5, time.Minute) // opens circuit

	// Wait for timeout, clear Redis error.
	mr.SetError("")
	time.Sleep(30 * time.Millisecond)

	// The probe call reaches Redis (which is healthy) → records success → closes.
	ok, err := rl.Allow(context.Background(), "k", 5, time.Minute)
	require.NoError(t, err)
	require.True(t, ok) // bucket has capacity
	require.Equal(t, "closed", cb.State())
}

func TestRateLimiter_CB_Nil_BehavesAsOriginal(t *testing.T) {
	c, _ := newRedis(t)
	rl := NewRateLimiter(c, nil) // no circuit breaker

	for i := 0; i < 3; i++ {
		ok, err := rl.Allow(context.Background(), "k", 3, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := rl.Allow(context.Background(), "k", 3, time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "bucket exhausted")
}

// ---------------------------------------------------------------------------
// Deduper + CircuitBreaker integration tests (miniredis)
// ---------------------------------------------------------------------------

func TestDeduper_CB_Open_AllowsThrough(t *testing.T) {
	c, _ := newRedis(t)
	cb := NewCircuitBreaker(1, time.Minute)
	d := NewDeduper(c, cb)

	cb.RecordFailure() // open the circuit

	// Both claims must return (true, nil) — the CB is the authority, not Redis.
	first, err := d.Claim(context.Background(), "evt-cb-1", time.Minute)
	require.NoError(t, err)
	require.True(t, first)

	// Same event_id — would normally be rejected by SETNX, but CB is open.
	second, err := d.Claim(context.Background(), "evt-cb-1", time.Minute)
	require.NoError(t, err)
	require.True(t, second, "open circuit: duplicate claim must also return true (fail-open)")
}

func TestDeduper_CB_OpensAfterRedisErrors(t *testing.T) {
	c, mr := newRedis(t)
	cb := NewCircuitBreaker(2, time.Minute)
	d := NewDeduper(c, cb)

	mr.SetError("ERR connection refused")

	for i := 0; i < 2; i++ {
		ok, err := d.Claim(context.Background(), "evt-err", time.Minute)
		require.NoError(t, err, "Redis errors must not surface with CB (call %d)", i)
		require.True(t, ok, "should fail-open (call %d)", i)
	}
	require.Equal(t, "open", cb.State())
}

func TestDeduper_CB_Nil_BehavesAsOriginal(t *testing.T) {
	c, _ := newRedis(t)
	d := NewDeduper(c, nil)

	first, _ := d.Claim(context.Background(), "evt-nil-cb", time.Minute)
	require.True(t, first)
	second, _ := d.Claim(context.Background(), "evt-nil-cb", time.Minute)
	require.False(t, second, "without CB, SETNX deduplication must work normally")
}

// ---------------------------------------------------------------------------
// TemplateCache + CircuitBreaker integration tests (miniredis)
// ---------------------------------------------------------------------------

func TestTemplateCache_CB_Open_BypassesRedis(t *testing.T) {
	c, _ := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cb := NewCircuitBreaker(1, time.Minute)
	cache := NewTemplateCache(stub, c, time.Minute, cb)

	// Warm Redis cache with a normal Get.
	_, _ = cache.Get(context.Background(), id)
	require.Equal(t, 1, stub.calls)

	// Open the circuit.
	cb.RecordFailure()

	// Get must bypass Redis and go directly to the stub.
	got, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, 2, stub.calls, "CB open → must query the repo, not Redis")
}

func TestTemplateCache_CB_OpensAfterRedisErrors(t *testing.T) {
	c, mr := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cb := NewCircuitBreaker(3, time.Minute)
	cache := NewTemplateCache(stub, c, time.Minute, cb)

	// Break Redis.
	mr.SetError("ERR connection refused")

	for i := 0; i < 3; i++ {
		got, err := cache.Get(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, id, got.ID, "must fall back to repo on Redis error (call %d)", i)
	}
	require.Equal(t, "open", cb.State())

	// CB is open: Redis is not called even after mr.SetError is cleared.
	mr.SetError("")
	stubCallsBefore := stub.calls
	got, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, stubCallsBefore+1, stub.calls, "CB open → repo called, not Redis")
}

func TestTemplateCache_CB_RecoveryRecloses(t *testing.T) {
	c, mr := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cb := NewCircuitBreaker(1, 20*time.Millisecond)
	cache := NewTemplateCache(stub, c, time.Minute, cb)

	// Break Redis to open the circuit.
	mr.SetError("ERR down")
	_, _ = cache.Get(context.Background(), id)
	require.Equal(t, "open", cb.State())

	// Restore Redis and wait for half-open timeout.
	mr.SetError("")
	time.Sleep(30 * time.Millisecond)

	// Probe call: Redis is healthy again → RecordSuccess → closed.
	_, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "closed", cb.State())
}

func TestTemplateCache_CB_CreateSkipsRedisWhenOpen(t *testing.T) {
	c, _ := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}

	cb := NewCircuitBreaker(1, time.Minute)
	cache := NewTemplateCache(stub, c, time.Minute, cb)
	cb.RecordFailure() // open circuit

	id := uuid.New()
	tmpl := seedTemplate(id)
	require.NoError(t, cache.Create(context.Background(), tmpl))

	// Get must go to the repo (Redis was not populated due to open circuit).
	cb.RecordSuccess() // close circuit so we can verify Redis is empty
	got, err := cache.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	// stub.calls: 1 from Create + 1 from Get (Redis miss because Create skipped set)
	require.Equal(t, 2, stub.calls, "Create with open CB must not write to Redis")
}

func TestTemplateCache_CB_Nil_BehavesAsOriginal(t *testing.T) {
	c, _ := newRedis(t)
	stub := &stubTemplateRepo{tpls: map[uuid.UUID]domain.Template{}}
	id := uuid.New()
	stub.tpls[id] = seedTemplate(id)

	cache := NewTemplateCache(stub, c, time.Minute, nil)

	_, _ = cache.Get(context.Background(), id) // miss → populate
	_, _ = cache.Get(context.Background(), id) // hit from Redis
	require.Equal(t, 1, stub.calls, "without CB, Redis cache should still work normally")
}
