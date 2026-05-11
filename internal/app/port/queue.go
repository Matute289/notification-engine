package port

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
)

// EventPublisher hands a notification off to the channel-specific work queue
// for asynchronous processing by a worker.
type EventPublisher interface {
	Publish(ctx context.Context, n *domain.Notification) error

	// Retry republishes the raw message body with an incremented attempt
	// counter. Implementations decide the backoff strategy (e.g. dead-letter
	// TTL hop). When attempts exceed maxAttempts the implementation should
	// route the message to a terminal dead-letter queue and report
	// sentToDead=true so the use case can mark the notification accordingly.
	Retry(ctx context.Context, channel domain.Channel, body []byte, attempt, maxAttempts int) (sentToDead bool, err error)

	// PublishRaw publishes a pre-serialised payload to a channel's work
	// queue. The outbox relay uses this so the wire format stored in
	// notification_outbox.payload is shipped byte-for-byte rather than
	// re-marshalled (and at risk of drift).
	PublishRaw(ctx context.Context, channel domain.Channel, body []byte, attempt int) error

	// Encode serialises a notification into the wire-format bytes that the
	// publisher would send. Used by SubmitNotification to stash a payload
	// in the transactional outbox, then by RelayOutbox via PublishRaw.
	Encode(n *domain.Notification) ([]byte, error)
}
