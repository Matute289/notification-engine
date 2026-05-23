package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/redis/go-redis/v9"
)

// RateLimiter is a token-bucket rate limiter implemented as a single Redis
// Lua script so refill+consume is atomic. We chose token bucket over the
// previous fixed-window counter because fixed windows allow up to 2*limit
// across a window boundary; with a token bucket the effective rate is
// rate=limit/window, which is what callers actually want.
//
// The bucket capacity equals the limit (so an idle key can absorb a burst up
// to limit), and tokens refill at rate = limit/window per second. Each Allow
// consumes 1 token. Returns false if there isn't a full token available.
//
// When a CircuitBreaker is configured and Redis is unhealthy, Allow fails open
// (returns true) so notifications are never blocked by a Redis outage.
type RateLimiter struct {
	c      *redis.Client
	script *redis.Script
	cb     *CircuitBreaker
}

// rateLimitScript implements the bucket math:
//   KEYS[1] = bucket key
//   ARGV[1] = capacity (max tokens; integer)
//   ARGV[2] = refill rate in tokens-per-second (float string)
//   ARGV[3] = bucket TTL in milliseconds (integer)
//
// Returns 1 (allowed) or 0 (denied).
const rateLimitScript = `
local key      = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate     = tonumber(ARGV[2])
local ttlMs    = tonumber(ARGV[3])

local time   = redis.call('TIME')
local nowMs  = tonumber(time[1]) * 1000 + math.floor(tonumber(time[2]) / 1000)

local data   = redis.call('HMGET', key, 't', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil then
  tokens = capacity
  ts     = nowMs
end

local elapsed = math.max(0, (nowMs - ts) / 1000.0)
tokens = math.min(capacity, tokens + elapsed * rate)

local allowed = 0
if tokens >= 1 then
  tokens  = tokens - 1
  allowed = 1
end

redis.call('HMSET', key, 't', tokens, 'ts', nowMs)
redis.call('PEXPIRE', key, ttlMs)
return allowed
`

// NewRateLimiter creates a RateLimiter. Pass a non-nil cb to enable circuit
// breaker protection: on Redis errors the limiter fails open rather than
// surfacing the error to callers.
func NewRateLimiter(c *redis.Client, cb *CircuitBreaker) *RateLimiter {
	return &RateLimiter{
		c:      c,
		script: redis.NewScript(rateLimitScript),
		cb:     cb,
	}
}

var _ port.RateLimiter = (*RateLimiter)(nil)

// Allow consumes one token. limit is interpreted as the bucket capacity; the
// refill rate is limit/window per second. window also doubles as the lower
// bound for the bucket key TTL (we keep it for 2x window so brief idle
// periods don't lose accumulated tokens).
//
// When the circuit breaker is open, or when a Redis error occurs with a
// circuit breaker configured, Allow fails open (returns true, nil) so a
// Redis outage never blocks notification submission.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if limit <= 0 || window <= 0 {
		return true, nil
	}
	if r.cb != nil && !r.cb.Allow() {
		return true, nil // circuit open → fail-open
	}
	rate := float64(limit) / window.Seconds()
	ttlMs := (2 * window).Milliseconds()
	res, err := r.script.Run(ctx, r.c, []string{key},
		limit, fmt.Sprintf("%f", rate), ttlMs,
	).Int()
	if err != nil {
		if r.cb != nil && isRedisError(err) {
			r.cb.RecordFailure()
			return true, nil // fail-open; don't surface the infrastructure error
		}
		return false, fmt.Errorf("ratelimit script: %w", err)
	}
	if r.cb != nil {
		r.cb.RecordSuccess()
	}
	return res == 1, nil
}
