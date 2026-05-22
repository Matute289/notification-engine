package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
)

// ProcessNotificationConfig holds the retry policy.
type ProcessNotificationConfig struct {
	MaxRetries int
}

// ProcessOutcome is the high-level result of one ProcessNotification execution.
// Inbound worker adapter uses it to decide whether to ack the message.
type ProcessOutcome string

const (
	OutcomeSent       ProcessOutcome = "sent"
	OutcomeRetry      ProcessOutcome = "retry"
	OutcomeDeadLetter ProcessOutcome = "dead_letter"
)

// ProcessNotification is the worker-side use case: send via provider, decide
// retry vs dead-letter, update status + metrics. Inbound adapter is responsible
// for ack/nack and message body marshalling.
type ProcessNotification struct {
	Notifications port.NotificationRepository
	Provider      port.NotificationProvider
	Publisher     port.EventPublisher
	Metrics       port.MetricsRecorder
	Clock         port.Clock
	Log           *slog.Logger
	Cfg           ProcessNotificationConfig
}

// ProcessInput carries the domain entity (already unmarshalled by the adapter)
// plus the raw bytes (for republishing into the retry queue).
type ProcessInput struct {
	Notification *domain.Notification
	RawBody      []byte
	Attempt      int
	Channel      domain.Channel
}

// Execute runs one delivery attempt. The returned outcome tells the inbound
// adapter how to ack / republish the underlying message.
func (u *ProcessNotification) Execute(ctx context.Context, in ProcessInput) (ProcessOutcome, error) {
	start := u.Clock.Now()
	n := in.Notification

	if err := n.MarkInFlight(in.Attempt, start); err == nil {
		_ = u.Notifications.UpdateStatus(ctx, n.ID, n.Status, n.Attempt, "")
	}

	err := u.Provider.Send(ctx, n)
	switch {
	case err == nil:
		now := u.Clock.Now()
		if err := n.MarkSent(now); err != nil {
			u.Log.Error("invariant violated marking sent", "err", err, "id", n.ID)
		}
		_ = u.Notifications.UpdateStatus(ctx, n.ID, n.Status, n.Attempt, "")
		_ = u.Notifications.RecordEvent(ctx, n.ID, "sent", nil)
		u.Metrics.NotificationSent(in.Channel.String())
		u.observe(in.Channel, OutcomeSent, start)
		return OutcomeSent, nil

	case errors.Is(err, port.ErrTransient):
		dead, retryErr := u.Publisher.Retry(ctx, in.Channel, in.RawBody, in.Attempt, u.Cfg.MaxRetries)
		if retryErr != nil {
			return "", fmt.Errorf("publish retry: %w", retryErr)
		}
		now := u.Clock.Now()
		if dead {
			_ = n.MarkDeadLetter(err.Error(), now)
			_ = u.Notifications.UpdateStatus(ctx, n.ID, n.Status, n.Attempt, err.Error())
			_ = u.Notifications.RecordEvent(ctx, n.ID, "dead_letter", map[string]any{"error": err.Error()})
			u.Metrics.NotificationDeadLettered(in.Channel.String())
			u.observe(in.Channel, OutcomeDeadLetter, start)
			return OutcomeDeadLetter, nil
		}
		_ = n.MarkRetrying(err.Error(), now)
		_ = u.Notifications.UpdateStatus(ctx, n.ID, n.Status, n.Attempt, err.Error())
		u.Metrics.NotificationFailed(in.Channel.String())
		u.observe(in.Channel, OutcomeRetry, start)
		return OutcomeRetry, nil

	default:
		// Terminal failure — dead-letter immediately.
		now := u.Clock.Now()
		_ = n.MarkDeadLetter(err.Error(), now)
		_ = u.Notifications.UpdateStatus(ctx, n.ID, n.Status, n.Attempt, err.Error())
		_ = u.Notifications.RecordEvent(ctx, n.ID, "dead_letter", map[string]any{"error": err.Error()})
		u.Metrics.NotificationDeadLettered(in.Channel.String())
		u.observe(in.Channel, OutcomeDeadLetter, start)
		// Forward to the terminal queue too — adapter will ack the original.
		_, _ = u.Publisher.Retry(ctx, in.Channel, in.RawBody, u.Cfg.MaxRetries, u.Cfg.MaxRetries)
		return OutcomeDeadLetter, nil
	}
}

func (u *ProcessNotification) observe(ch domain.Channel, out ProcessOutcome, start time.Time) {
	u.Metrics.ObserveWorkerDuration(ch.String(), string(out), u.Clock.Now().Sub(start).Seconds())
}
