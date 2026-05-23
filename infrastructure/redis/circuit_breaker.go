package redis

import (
	"context"
	"errors"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// cbState encodes the three circuit-breaker states.
type cbState int

const (
	cbClosed   cbState = iota // normal: requests reach Redis
	cbOpen                    // Redis unavailable: skip Redis, use fallback
	cbHalfOpen                // timeout elapsed: one probe call allowed
)

const (
	// DefaultCBThreshold is the number of consecutive Redis errors that open
	// the circuit.
	DefaultCBThreshold = 5
	// DefaultCBOpenTimeout is how long the circuit stays open before a probe
	// call is allowed (transition to half-open).
	DefaultCBOpenTimeout = 30 * time.Second
)

// CircuitBreaker is a thread-safe three-state circuit breaker shared across
// the Redis-backed components (RateLimiter, Deduper, TemplateCache).
// When open, each component falls back to its safe default:
//
//   - RateLimiter  → fail-open (allow all requests through)
//   - Deduper      → fail-open (grant the claim; the DB unique index is backstop)
//   - TemplateCache → bypass Redis and query MongoDB directly
type CircuitBreaker struct {
	mu          sync.Mutex
	state       cbState
	failures    int
	threshold   int
	openTimeout time.Duration
	openedAt    time.Time
}

// NewCircuitBreaker creates a CircuitBreaker. Pass 0 for either parameter to
// use the defaults (threshold = 5, openTimeout = 30 s).
func NewCircuitBreaker(threshold int, openTimeout time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = DefaultCBThreshold
	}
	if openTimeout <= 0 {
		openTimeout = DefaultCBOpenTimeout
	}
	return &CircuitBreaker{threshold: threshold, openTimeout: openTimeout}
}

// Allow returns true if the caller should proceed with a Redis call.
// When the circuit is open it returns false (caller must use its fallback).
// Once the open timeout elapses it transitions to half-open and returns true
// so the next call can probe recovery.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbClosed:
		return true
	case cbOpen:
		if time.Since(cb.openedAt) >= cb.openTimeout {
			cb.state = cbHalfOpen
			return true // this call is the probe
		}
		return false
	default: // cbHalfOpen
		return true
	}
}

// RecordSuccess marks a successful Redis call. Resets the failure counter
// and closes the circuit (safe to call from closed, open, or half-open).
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	cb.state = cbClosed
}

// RecordFailure marks a failed Redis call. The circuit opens once consecutive
// failures reach the threshold, or immediately when in half-open state (a
// failed probe re-opens without waiting for the full threshold again).
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	if cb.state == cbHalfOpen || cb.failures >= cb.threshold {
		cb.state = cbOpen
		cb.openedAt = time.Now()
		cb.failures = 0
	}
}

// State returns a human-readable label for the current circuit state.
// Intended for logging and health endpoints; does not trigger transitions.
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	switch cb.state {
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half_open"
	default:
		return "closed"
	}
}

// isRedisError returns true for errors that indicate a Redis infrastructure
// problem and should count toward the failure threshold.
// redis.Nil (key not found) and context.Canceled (caller abort) are not
// infrastructure signals and are excluded.
func isRedisError(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, goredis.Nil) && !errors.Is(err, context.Canceled)
}
