package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
)

// RescueStuckConfig parameterises one janitor pass.
type RescueStuckConfig struct {
	// StuckThreshold is how long a row may sit in InFlight before we
	// consider its worker dead.
	StuckThreshold time.Duration
	// BatchSize bounds DB and queue traffic per pass.
	BatchSize int
}

// RescueStuckNotifications finds notifications stuck in InFlight, republishes
// them, and resets status to Enqueued so a healthy worker can pick them up.
// It is idempotent at the row level: re-running while a worker is mid-rescue
// only incurs an extra requeue, not a duplicate row (event_id is unique).
type RescueStuckNotifications struct {
	Notifications port.NotificationRepository
	Publisher     port.EventPublisher
	Clock         port.Clock
	Log           *slog.Logger
	Cfg           RescueStuckConfig
}

// RescueResult is returned from one pass for visibility.
type RescueResult struct {
	Examined int
	Rescued  int
	Errors   int
}

func (u *RescueStuckNotifications) Execute(ctx context.Context) (RescueResult, error) {
	stuck, err := u.Notifications.ListStuckInFlight(ctx, u.Cfg.StuckThreshold, u.Cfg.BatchSize)
	if err != nil {
		return RescueResult{}, fmt.Errorf("list stuck: %w", err)
	}
	res := RescueResult{Examined: len(stuck)}
	now := u.Clock.Now()
	for _, n := range stuck {
		if err := u.Publisher.Publish(ctx, n); err != nil {
			u.Log.Warn("rescue publish failed", "id", n.ID, "err", err)
			res.Errors++
			continue
		}
		// Force the status back to Enqueued; we don't go through the state
		// machine here because we are explicitly recovering an inconsistent
		// state, not following the normal life cycle.
		if err := u.Notifications.UpdateStatus(ctx, n.ID, domain.StatusEnqueued, n.Attempt, "rescued by janitor"); err != nil {
			u.Log.Warn("rescue status update failed", "id", n.ID, "err", err)
			res.Errors++
			continue
		}
		u.Log.Info("rescued stuck notification",
			"id", n.ID, "channel", n.Channel, "attempt", n.Attempt, "stuck_for", now.Sub(n.UpdatedAt))
		res.Rescued++
	}
	return res, nil
}
