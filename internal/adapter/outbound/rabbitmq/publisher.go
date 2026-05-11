package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher implements port.EventPublisher with broker-confirmed publishes.
// It owns a dedicated channel (separate from any consumer channel), opens it
// lazily, and re-opens it transparently after a reconnect. Each Publish waits
// for a server-side ack before returning.
type Publisher struct {
	conn *Conn

	mu sync.Mutex
	ch *amqp.Channel
}

func NewPublisher(c *Conn) *Publisher { return &Publisher{conn: c} }

var _ port.EventPublisher = (*Publisher)(nil)

// publishedNotification is the wire format. We translate domain types to/from
// this so adapters control serialisation, not domain.
type publishedNotification struct {
	ID         string            `json:"id"`
	EventID    string            `json:"event_id"`
	Channel    string            `json:"channel"`
	Recipient  domain.Recipient  `json:"recipient"`
	TemplateID *string           `json:"template_id,omitempty"`
	Variables  map[string]string `json:"variables,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body,omitempty"`
	Attempt    int               `json:"attempt"`
}

func toWire(n *domain.Notification) publishedNotification {
	w := publishedNotification{
		ID:        n.ID.String(),
		EventID:   string(n.EventID),
		Channel:   string(n.Channel),
		Recipient: n.Recipient,
		Variables: n.Variables,
		Subject:   n.Subject,
		Body:      n.Body,
		Attempt:   n.Attempt,
	}
	if n.TemplateID != nil {
		s := n.TemplateID.String()
		w.TemplateID = &s
	}
	return w
}

// ensureChannel returns the publisher's confirm-mode channel, opening it if
// missing or replacing it if the previous one is closed (e.g. after reconnect).
func (p *Publisher) ensureChannel() (*amqp.Channel, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	ch, err := p.conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("confirm mode: %w", err)
	}
	p.ch = ch
	return ch, nil
}

func (p *Publisher) Publish(ctx context.Context, n *domain.Notification) error {
	body, err := json.Marshal(toWire(n))
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return p.publishRaw(ctx, RoutingKey(n.Channel), Exchange, body, n.Attempt, "", n.ID.String())
}

// PublishRaw ships a pre-built payload (e.g. the outbox row body) to the
// channel's work queue without touching the bytes.
func (p *Publisher) PublishRaw(ctx context.Context, channel domain.Channel, body []byte, attempt int) error {
	return p.publishRaw(ctx, RoutingKey(channel), Exchange, body, attempt, "", "")
}

// Encode serialises a notification into the wire format. Exposed via the
// EventPublisher port so the SubmitNotification use case can put the bytes
// in the transactional outbox.
func (p *Publisher) Encode(n *domain.Notification) ([]byte, error) {
	return json.Marshal(toWire(n))
}

func (p *Publisher) Retry(ctx context.Context, channel domain.Channel, body []byte, attempt, maxAttempts int) (sentToDead bool, err error) {
	if attempt >= maxAttempts || attempt >= len(BackoffSchedule) {
		err := p.publishRaw(ctx, DeadQueue(channel), "", body, attempt, "", "")
		return true, err
	}
	ttl := BackoffSchedule[attempt]
	err = p.publishRaw(ctx, RetryQueue(channel), "", body, attempt+1,
		strconv.FormatInt(ttl.Milliseconds(), 10), "")
	return false, err
}

// publishRaw publishes one message and waits for the broker's confirm. exchange
// may be empty (default exchange) for direct-to-queue publishes used by Retry.
func (p *Publisher) publishRaw(ctx context.Context, routingKey, exchange string, body []byte, attempt int, expiration, messageID string) error {
	ch, err := p.ensureChannel()
	if err != nil {
		return err
	}
	pub := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
		Headers:      amqp.Table{HeaderAttempt: int32(attempt)},
		Expiration:   expiration,
		MessageId:    messageID,
	}
	confirm, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, pub)
	if err != nil {
		return fmt.Errorf("amqp publish: %w", err)
	}
	if confirm == nil {
		// Channel not in confirm mode — should never happen with ensureChannel,
		// but fall back to a non-confirmed publish rather than blocking forever.
		return nil
	}
	ack, err := confirm.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("amqp confirm wait: %w", err)
	}
	if !ack {
		return errors.New("amqp publish nack")
	}
	return nil
}
