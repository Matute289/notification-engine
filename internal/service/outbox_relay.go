package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/example/notification-engine/internal/port"
)

// RelayConfig parameterises one outbox-relay pass.
type RelayConfig struct {
	BatchSize int
}

// RelayOutbox drains pending outbox rows: publish each one, then mark
// published — all inside the same DB transaction so a crash never causes a
// "row marked published, message never sent" inconsistency.
type RelayOutbox struct {
	Outbox    port.OutboxRepository
	Publisher port.EventPublisher
	Log       *slog.Logger
	Cfg       RelayConfig
}

// RelayResult is the outcome of one pass; the relay binary logs it.
type RelayResult struct {
	Examined  int
	Published int
	Failed    int
}

func (u *RelayOutbox) Execute(ctx context.Context) (RelayResult, error) {
	items, tx, err := u.Outbox.Claim(ctx, u.Cfg.BatchSize)
	if err != nil {
		return RelayResult{}, fmt.Errorf("claim outbox: %w", err)
	}
	if len(items) == 0 {
		_ = tx.Rollback(ctx)
		return RelayResult{}, nil
	}
	res := RelayResult{Examined: len(items)}

	for _, it := range items {
		if err := u.publish(ctx, it); err != nil {
			u.Log.Warn("outbox publish failed",
				"id", it.ID, "notification_id", it.NotificationID, "err", err)
			if mErr := tx.MarkFailed(ctx, it.ID, it.Attempts+1, err.Error()); mErr != nil {
				u.Log.Error("outbox mark failed: %v", "err", mErr)
			}
			res.Failed++
			continue
		}
		if err := tx.MarkPublished(ctx, it.ID); err != nil {
			u.Log.Error("outbox mark published failed", "id", it.ID, "err", err)
			res.Failed++
			continue
		}
		res.Published++
	}

	if err := tx.Commit(ctx); err != nil {
		return res, fmt.Errorf("commit outbox tx: %w", err)
	}
	return res, nil
}

func (u *RelayOutbox) publish(ctx context.Context, it port.OutboxItem) error {
	return u.Publisher.PublishRaw(ctx, it.Channel, it.Payload, it.Attempts)
}
