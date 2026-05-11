package port

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
)

// OutboxItem is what an outbox-aware repository hands to the relay.
type OutboxItem struct {
	ID             int64
	NotificationID string
	Channel        domain.Channel
	Payload        []byte
	Attempts       int
}

// OutboxRepository is the port the outbox relay drives. It is implemented by
// the Postgres adapter alongside NotificationRepository so that
// `Create + Enqueue` can run in one DB transaction.
type OutboxRepository interface {
	// Claim atomically locks and returns up to limit pending outbox rows
	// using SELECT ... FOR UPDATE SKIP LOCKED. The returned items are owned
	// by the calling transaction; callers must MarkPublished or
	// MarkFailed before the transaction commits.
	Claim(ctx context.Context, limit int) ([]OutboxItem, OutboxTx, error)
}

// OutboxTx encapsulates the transaction returned by Claim. The relay calls
// MarkPublished/MarkFailed on each item and then Commit (or Rollback on
// catastrophic error).
type OutboxTx interface {
	MarkPublished(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, attempt int, err string) error
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TxNotificationRepository is the transactional flavour of
// NotificationRepository used by SubmitNotification when writing the
// notification_log + outbox in one go. The composition root binds this to
// the Postgres adapter; tests can substitute a fake.
type TxNotificationRepository interface {
	NotificationRepository

	// SubmitWithOutbox inserts the notification and a matching outbox row in
	// one DB transaction. The supplied payload is the wire-format JSON body
	// the relay will publish.
	SubmitWithOutbox(ctx context.Context, n *domain.Notification, payload []byte) error
}
