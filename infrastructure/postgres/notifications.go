package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationRepository implements port.NotificationRepository and also the
// transactional outbox flavour port.TxNotificationRepository.
type NotificationRepository struct{ pool *pgxpool.Pool }

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

// Compile-time checks.
var (
	_ port.NotificationRepository   = (*NotificationRepository)(nil)
	_ port.TxNotificationRepository = (*NotificationRepository)(nil)
	_ port.OutboxRepository         = (*NotificationRepository)(nil)
)

// SubmitWithOutbox writes the notification row and an outbox row in one
// transaction. Either both land or neither does, which is exactly what the
// "transactional outbox" pattern is for.
func (r *NotificationRepository) SubmitWithOutbox(ctx context.Context, n *domain.Notification, payload []byte) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	recipientJSON, _ := json.Marshal(n.Recipient) //nolint:errcheck
	varsJSON, _ := json.Marshal(n.Variables)      //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`INSERT INTO notification_log
		   (id, event_id, channel, recipient, template_id, variables, subject, body, status, attempt)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		n.ID, string(n.EventID), string(n.Channel), recipientJSON, n.TemplateID, varsJSON,
		n.Subject, n.Body, string(n.Status), n.Attempt); err != nil {
		return fmt.Errorf("tx insert log: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO notification_outbox (notification_id, channel, payload, status)
		 VALUES ($1, $2, $3, 'pending')`,
		n.ID, string(n.Channel), payload); err != nil {
		return fmt.Errorf("tx insert outbox: %w", err)
	}
	return tx.Commit(ctx)
}

// Claim locks and returns up to limit pending outbox items. The transaction
// stays open; callers must MarkPublished/MarkFailed and Commit.
func (r *NotificationRepository) Claim(ctx context.Context, limit int) ([]port.OutboxItem, port.OutboxTx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	rows, err := tx.Query(ctx,
		`SELECT id, notification_id, channel, payload, attempts
		   FROM notification_outbox
		  WHERE status = 'pending'
		  ORDER BY created_at
		  LIMIT $1
		  FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, fmt.Errorf("claim outbox: %w", err)
	}
	defer rows.Close()

	var items []port.OutboxItem
	for rows.Next() {
		var (
			it      port.OutboxItem
			channel string
			notifID uuid.UUID
		)
		if err := rows.Scan(&it.ID, &notifID, &channel, &it.Payload, &it.Attempts); err != nil {
			_ = tx.Rollback(ctx)
			return nil, nil, fmt.Errorf("scan outbox: %w", err)
		}
		it.NotificationID = notifID.String()
		it.Channel = domain.Channel(channel)
		items = append(items, it)
	}
	return items, &outboxTx{tx: tx}, nil
}

type outboxTx struct {
	tx pgx.Tx
}

func (o *outboxTx) MarkPublished(ctx context.Context, id int64) error {
	_, err := o.tx.Exec(ctx,
		`UPDATE notification_outbox
		    SET status = 'published', published_at = NOW(), last_error = ''
		  WHERE id = $1`, id)
	return err
}

func (o *outboxTx) MarkFailed(ctx context.Context, id int64, attempts int, errStr string) error {
	_, err := o.tx.Exec(ctx,
		`UPDATE notification_outbox
		    SET attempts = $2, last_error = $3
		  WHERE id = $1`, id, attempts, errStr)
	return err
}

func (o *outboxTx) Commit(ctx context.Context) error  { return o.tx.Commit(ctx) }
func (o *outboxTx) Rollback(ctx context.Context) error { return o.tx.Rollback(ctx) }

func (r *NotificationRepository) Create(ctx context.Context, n *domain.Notification) error {
	recipientJSON, _ := json.Marshal(n.Recipient) //nolint:errcheck
	varsJSON, _ := json.Marshal(n.Variables)      //nolint:errcheck
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notification_log
		   (id, event_id, channel, recipient, template_id, variables, subject, body, status, attempt)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		n.ID, string(n.EventID), string(n.Channel), recipientJSON, n.TemplateID, varsJSON,
		n.Subject, n.Body, string(n.Status), n.Attempt)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

func (r *NotificationRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status, attempt int, lastErr string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE notification_log
		    SET status = $2, attempt = $3, last_error = $4, updated_at = NOW()
		  WHERE id = $1`, id, string(status), attempt, lastErr)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

func (r *NotificationRepository) Get(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	return r.scan(ctx,
		`SELECT id, event_id, channel, recipient, template_id, variables, subject, body, status, attempt, last_error, created_at, updated_at
		   FROM notification_log WHERE id = $1`, id)
}

func (r *NotificationRepository) GetByEventID(ctx context.Context, eventID domain.EventID) (*domain.Notification, error) {
	return r.scan(ctx,
		`SELECT id, event_id, channel, recipient, template_id, variables, subject, body, status, attempt, last_error, created_at, updated_at
		   FROM notification_log WHERE event_id = $1`, string(eventID))
}

func (r *NotificationRepository) RecordEvent(ctx context.Context, notifID uuid.UUID, eventType string, metadata map[string]any) error {
	meta, _ := json.Marshal(metadata) //nolint:errcheck
	_, err := r.pool.Exec(ctx,
		`INSERT INTO analytics_events (notification_id, event_type, metadata)
		 VALUES ($1, $2, $3)`, notifID, eventType, meta)
	if err != nil {
		return fmt.Errorf("record event: %w", err)
	}
	return nil
}

// ListStuckInFlight finds rows still in_flight after threshold has elapsed
// since updated_at. The janitor calls this periodically to rescue rows whose
// worker crashed mid-send.
func (r *NotificationRepository) ListStuckInFlight(ctx context.Context, threshold time.Duration, limit int) ([]*domain.Notification, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, event_id, channel, recipient, template_id, variables, subject, body, status, attempt, last_error, created_at, updated_at
		   FROM notification_log
		  WHERE status = 'in_flight'
		    AND updated_at < NOW() - $1::interval
		  ORDER BY updated_at ASC
		  LIMIT $2`, fmt.Sprintf("%d milliseconds", threshold.Milliseconds()), limit)
	if err != nil {
		return nil, fmt.Errorf("list stuck: %w", err)
	}
	defer rows.Close()

	var out []*domain.Notification
	for rows.Next() {
		n, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// scanRow maps the standard 13-column SELECT (above) into a domain.Notification.
func scanRow(rows pgx.Rows) (*domain.Notification, error) {
	var (
		n            domain.Notification
		eventID      string
		channel      string
		recipientRaw []byte
		varsRaw      []byte
		status       string
	)
	if err := rows.Scan(&n.ID, &eventID, &channel, &recipientRaw, &n.TemplateID, &varsRaw,
		&n.Subject, &n.Body, &status, &n.Attempt, &n.LastError, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan stuck row: %w", err)
	}
	n.EventID = domain.EventID(eventID)
	n.Channel = domain.Channel(channel)
	n.Status = domain.Status(status)
	if err := json.Unmarshal(recipientRaw, &n.Recipient); err != nil {
		return nil, fmt.Errorf("decode recipient: %w", err)
	}
	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &n.Variables); err != nil {
			return nil, fmt.Errorf("decode variables: %w", err)
		}
	}
	return &n, nil
}

func (r *NotificationRepository) scan(ctx context.Context, query string, arg any) (*domain.Notification, error) {
	var (
		n            domain.Notification
		eventID      string
		channel      string
		recipientRaw []byte
		varsRaw      []byte
		status       string
	)
	err := r.pool.QueryRow(ctx, query, arg).Scan(
		&n.ID, &eventID, &channel, &recipientRaw, &n.TemplateID, &varsRaw,
		&n.Subject, &n.Body, &status, &n.Attempt, &n.LastError, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan notification: %w", err)
	}
	n.EventID = domain.EventID(eventID)
	n.Channel = domain.Channel(channel)
	n.Status = domain.Status(status)
	if err := json.Unmarshal(recipientRaw, &n.Recipient); err != nil {
		return nil, fmt.Errorf("decode recipient json: %w", err)
	}
	if len(varsRaw) > 0 {
		if err := json.Unmarshal(varsRaw, &n.Variables); err != nil {
			return nil, fmt.Errorf("decode variables json: %w", err)
		}
	}
	return &n, nil
}
