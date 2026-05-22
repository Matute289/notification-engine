// Package consumer is the AMQP inbound adapter. It consumes messages from a
// channel-specific work queue and delegates the actual processing to the
// ProcessNotification service.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/example/notification-engine/infrastructure/rabbitmq"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Consumer drives one channel's queue.
type Consumer struct {
	Channel     domain.Channel
	Concurrency int
	Conn        *rabbitmq.Conn
	UseCase     *service.ProcessNotification
	Log         *slog.Logger
}

// wireFormat must mirror rabbitmq.Publisher's JSON shape.
type wireFormat struct {
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

// Run keeps a consume loop alive across broker restarts. Each iteration of
// the outer loop opens a fresh consume channel; if the broker drops or the
// consume channel closes, we wait briefly for the managed Conn to reconnect
// then re-subscribe. The loop only returns when ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		if err := c.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.Log.Warn("consume loop ended; will retry after reconnect",
				"channel", c.Channel, "err", err)
			c.Conn.AfterReconnect(ctx, 5*time.Second)
			continue
		}
		// runOnce returned nil only on ctx.Done.
		return nil
	}
}

func (c *Consumer) runOnce(ctx context.Context) error {
	ch, err := c.Conn.ConsumeChannel(c.Concurrency)
	if err != nil {
		return err
	}
	defer ch.Close()

	deliveries, err := ch.Consume(rabbitmq.WorkQueue(c.Channel), "worker-"+string(c.Channel),
		false, false, false, false, nil)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, c.Concurrency)

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case d, ok := <-deliveries:
			if !ok {
				wg.Wait()
				return errors.New("delivery channel closed")
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(d amqp.Delivery) {
				defer func() { <-sem; wg.Done() }()
				c.handle(ctx, d)
			}(d)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, d amqp.Delivery) {
	n, err := decode(d.Body)
	if err != nil {
		c.Log.Error("malformed message; discarding",
			"err", err, "channel", c.Channel, "message_id", d.MessageId)
		_ = d.Reject(false)
		return
	}
	attempt := readAttempt(d.Headers)

	_, ucErr := c.UseCase.Execute(ctx, service.ProcessInput{
		Notification: n,
		RawBody:      d.Body,
		Attempt:      attempt,
		Channel:      c.Channel,
	})
	if ucErr != nil {
		c.Log.Error("process failed; requeuing",
			"err", ucErr, "id", n.ID, "attempt", attempt)
		_ = d.Nack(false, true)
		return
	}
	_ = d.Ack(false)
}

func decode(body []byte) (*domain.Notification, error) {
	var w wireFormat
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(w.ID)
	if err != nil {
		return nil, err
	}
	n := &domain.Notification{
		ID:        id,
		EventID:   domain.EventID(w.EventID),
		Channel:   domain.Channel(w.Channel),
		Recipient: w.Recipient,
		Variables: w.Variables,
		Subject:   w.Subject,
		Body:      w.Body,
		Attempt:   w.Attempt,
		Status:    domain.StatusEnqueued,
	}
	if w.TemplateID != nil {
		tid, err := uuid.Parse(*w.TemplateID)
		if err == nil {
			n.TemplateID = &tid
		}
	}
	return n, nil
}

func readAttempt(h amqp.Table) int {
	if h == nil {
		return 0
	}
	v, ok := h[rabbitmq.HeaderAttempt]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case int32:
		return int(x)
	case int64:
		return int(x)
	case int:
		return x
	}
	return 0
}
