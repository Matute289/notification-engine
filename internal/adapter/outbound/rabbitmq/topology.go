// Package rabbitmq implements port.EventPublisher on top of RabbitMQ.
//
// Two production-grade properties beyond the basic publisher:
//
//   1. Auto-reconnect. Conn watches the underlying *amqp.Connection's
//      NotifyClose channel; on a non-graceful close it redials with
//      exponential backoff and re-declares topology. Callers see transient
//      ErrNotReady until reconnect succeeds.
//
//   2. Publisher confirms (mandatory). The publisher opens a dedicated
//      channel in confirm mode; every Publish blocks until the broker acks
//      (or returns) the message, so a "successful" publish really did make
//      it past the broker.
//
// Topology — provisioned by Setup, idempotent:
//
//	exchange  notifications (topic, durable)
//	   ├── queue notifications.<channel>          (work queue, dead-letters to retry)
//	   ├── queue notifications.<channel>.retry    (TTL backoff; dead-letters back to work)
//	   └── queue notifications.<channel>.dead     (terminal)
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/example/notification-engine/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	Exchange      = "notifications"
	HeaderAttempt = "x-attempt"
)

// ErrNotReady is returned when the connection is currently down (during
// reconnect). Callers should treat it as transient.
var ErrNotReady = errors.New("amqp connection not ready")

// BackoffSchedule defines the dead-letter TTL applied to retries.
var BackoffSchedule = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// reconnectBackoff bounds how often we redial. Exponential up to the cap.
var (
	reconnectInitial = 500 * time.Millisecond
	reconnectMax     = 30 * time.Second
)

// Conn is a managed AMQP connection that survives broker restarts. After Dial,
// callers should treat Conn as long-lived; channels are obtained per-call via
// Channel() so a closed channel never makes its way into a publish or consume.
type Conn struct {
	url string
	log *slog.Logger

	mu       sync.RWMutex
	conn     *amqp.Connection
	channels []domain.Channel // remembered so reconnect can redeclare topology

	closed chan struct{} // closed by Close()
}

// Dial establishes the initial connection and starts the reconnect supervisor.
// Even if the broker drops afterwards, Conn will redial without caller action.
func Dial(url string, log *slog.Logger) (*Conn, error) {
	c := &Conn{url: url, log: log, closed: make(chan struct{})}
	if err := c.connect(); err != nil {
		return nil, err
	}
	go c.supervise()
	return c, nil
}

func (c *Conn) connect() error {
	conn, err := amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	return nil
}

// supervise watches the current connection's close-notify and triggers a
// reconnect cycle whenever it fires unexpectedly.
func (c *Conn) supervise() {
	for {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()
		if conn == nil {
			return
		}
		notify := conn.NotifyClose(make(chan *amqp.Error, 1))
		select {
		case <-c.closed:
			return
		case err, ok := <-notify:
			if !ok && err == nil {
				return // graceful close
			}
			c.log.Warn("amqp connection lost; reconnecting", "err", err)
			if !c.reconnectWithBackoff() {
				return
			}
		}
	}
}

func (c *Conn) reconnectWithBackoff() bool {
	backoff := reconnectInitial
	for {
		select {
		case <-c.closed:
			return false
		default:
		}
		if err := c.connect(); err == nil {
			c.mu.RLock()
			channels := c.channels
			c.mu.RUnlock()
			if len(channels) > 0 {
				if err := c.declareTopology(channels); err != nil {
					c.log.Warn("topology redeclare failed; retrying", "err", err)
					time.Sleep(backoff)
					continue
				}
			}
			c.log.Info("amqp reconnected")
			return true
		} else {
			c.log.Warn("amqp reconnect failed", "err", err)
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, reconnectMax)
	}
}

// Close stops the supervisor and closes the underlying connection.
func (c *Conn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Channel returns a fresh AMQP channel on the current connection. Callers own
// the lifecycle and must Close it when done. Returns ErrNotReady if the
// connection is currently down.
func (c *Conn) Channel() (*amqp.Channel, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil || conn.IsClosed() {
		return nil, ErrNotReady
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("amqp channel: %w", err)
	}
	return ch, nil
}

// IsConnected reports whether the underlying connection is currently alive.
// Health probes use this; it is not a hard guarantee given the inherent race.
func (c *Conn) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && !c.conn.IsClosed()
}

// Setup remembers the channel list (so reconnect can redeclare) and
// provisions the topology now.
func (c *Conn) Setup(channels []domain.Channel) error {
	c.mu.Lock()
	c.channels = append([]domain.Channel(nil), channels...)
	c.mu.Unlock()
	return c.declareTopology(channels)
}

func (c *Conn) declareTopology(channels []domain.Channel) error {
	ch, err := c.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	for _, c := range channels {
		work, retry, dead := WorkQueue(c), RetryQueue(c), DeadQueue(c)

		if _, err := ch.QueueDeclare(dead, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dead queue: %w", err)
		}
		if _, err := ch.QueueDeclare(work, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": retry,
		}); err != nil {
			return fmt.Errorf("declare work queue: %w", err)
		}
		if _, err := ch.QueueDeclare(retry, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": work,
		}); err != nil {
			return fmt.Errorf("declare retry queue: %w", err)
		}
		if err := ch.QueueBind(work, RoutingKey(c), Exchange, false, nil); err != nil {
			return fmt.Errorf("bind work queue: %w", err)
		}
	}
	return nil
}

// QueueDepth returns the message count for the named channel's work queue.
func (c *Conn) QueueDepth(channel domain.Channel) (int, error) {
	ch, err := c.Channel()
	if err != nil {
		return 0, err
	}
	defer ch.Close()
	q, err := ch.QueueDeclarePassive(WorkQueue(channel), true, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": RetryQueue(channel),
	})
	if err != nil {
		return 0, err
	}
	return q.Messages, nil
}

// ConsumeChannel returns a fresh channel ready for Consume. The caller is
// responsible for closing it (typically when their consume loop exits or the
// connection is torn down).
func (c *Conn) ConsumeChannel(prefetch int) (*amqp.Channel, error) {
	ch, err := c.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("qos: %w", err)
	}
	return ch, nil
}

// AfterReconnect blocks until a reconnect cycle completes or ctx is cancelled.
// The worker uses it to back off briefly between consume attempts during a
// broker outage.
func (c *Conn) AfterReconnect(ctx context.Context, max time.Duration) {
	t := time.NewTimer(max)
	defer t.Stop()
	for {
		if c.IsConnected() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Naming helpers — exported so the inbound worker adapter can construct the
// same queue names without re-encoding the convention.
func RoutingKey(c domain.Channel) string { return "notification." + string(c) }
func WorkQueue(c domain.Channel) string  { return "notifications." + string(c) }
func RetryQueue(c domain.Channel) string { return "notifications." + string(c) + ".retry" }
func DeadQueue(c domain.Channel) string  { return "notifications." + string(c) + ".dead" }
