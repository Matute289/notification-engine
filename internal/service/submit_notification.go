// Package usecase contains the application's orchestration logic. Each use
// case is a struct with a single Execute method, depending only on domain
// types and ports — never on concrete adapters.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// SubmitNotificationConfig collects the tunables the SubmitNotification use
// case needs. Composition root populates these from environment configuration.
type SubmitNotificationConfig struct {
	DedupeTTL       time.Duration
	RateLimits      map[domain.Channel]int
	RateLimitWindow time.Duration
}

// SubmitNotification is the API-side use case: validate, dedupe, opt-out,
// rate-limit, hydrate, render, persist (notification + outbox in one TX),
// then return. The outbox-relay process handles the actual publish to the
// queue, so we never have a "row exists / message lost" inconsistency.
type SubmitNotification struct {
	Notifications port.TxNotificationRepository
	Users         port.UserRepository
	Templates     port.TemplateRepository
	Renderer      port.TemplateRenderer
	Publisher     port.EventPublisher // used only for Encode; the relay does the actual publish
	Limiter       port.RateLimiter
	Deduper       port.Deduper
	Metrics       port.MetricsRecorder
	Clock         port.Clock
	Log           *slog.Logger
	Cfg           SubmitNotificationConfig
}

// SubmitInput is the use-case-shaped request. Inbound adapters translate
// their transport-specific request type into this.
type SubmitInput struct {
	EventID    domain.EventID
	Channel    domain.Channel
	Recipient  domain.Recipient
	TemplateID *uuid.UUID
	Variables  map[string]string
	Subject    string
	Body       string
}

// SubmitOutput tells the caller whether the request was a duplicate (so the
// adapter can choose 200 vs 202) and exposes the resulting notification.
type SubmitOutput struct {
	Notification *domain.Notification
	Duplicate    bool
}

// Execute runs the use case. Return values follow the convention "domain
// error wrapped with a sentinel" so adapters can map to status codes via
// errors.Is.
func (u *SubmitNotification) Execute(ctx context.Context, in SubmitInput) (SubmitOutput, error) {
	now := u.Clock.Now()

	// 1) Cheap idempotency claim — keeps the hot path off Postgres for replays.
	claimed, err := u.Deduper.Claim(ctx, in.EventID.String(), u.Cfg.DedupeTTL)
	if err != nil {
		return SubmitOutput{}, fmt.Errorf("dedupe claim: %w", err)
	}
	if !claimed {
		existing, err := u.Notifications.GetByEventID(ctx, in.EventID)
		switch {
		case err == nil:
			return SubmitOutput{Notification: existing, Duplicate: true}, nil
		case errors.Is(err, domain.ErrNotFound):
			// Cache hit, DB miss. Treat as fresh; the unique index on event_id
			// ultimately backstops correctness if a second writer races us.
		default:
			return SubmitOutput{}, fmt.Errorf("lookup duplicate: %w", err)
		}
	}

	// 2) Per-user policies (opt-out + rate limit) when a user is identified.
	if in.Recipient.UserID != nil {
		uid := *in.Recipient.UserID
		setting, err := u.Users.GetSetting(ctx, uid, in.Channel)
		if err != nil {
			return SubmitOutput{}, fmt.Errorf("opt-in lookup: %w", err)
		}
		if !setting.OptIn {
			return SubmitOutput{}, domain.ErrOptedOut
		}
		if limit, ok := u.Cfg.RateLimits[in.Channel]; ok && limit > 0 {
			key := fmt.Sprintf("notif:rl:%d:%s", uid, in.Channel)
			allowed, err := u.Limiter.Allow(ctx, key, limit, u.Cfg.RateLimitWindow)
			if err != nil {
				return SubmitOutput{}, fmt.Errorf("rate limit: %w", err)
			}
			if !allowed {
				return SubmitOutput{}, domain.ErrRateLimited
			}
		}
		if err := u.hydrateRecipient(ctx, in.Channel, &in.Recipient); err != nil {
			return SubmitOutput{}, err
		}
	}

	// 3) Render template (if supplied) and decide on the final body.
	subject, body := in.Subject, in.Body
	if in.TemplateID != nil {
		subj, b, err := u.Renderer.Render(ctx, *in.TemplateID, in.Variables)
		if err != nil {
			return SubmitOutput{}, fmt.Errorf("render: %w", err)
		}
		subject, body = subj, b
	}
	if body == "" {
		return SubmitOutput{}, fmt.Errorf("%w: body or template_id required", domain.ErrInvalidInput)
	}

	// 4) Domain construction — invariants checked here.
	n, err := domain.NewNotification(uuid.New(), in.EventID, in.Channel, in.Recipient, in.TemplateID, in.Variables, subject, body, now)
	if err != nil {
		return SubmitOutput{}, err
	}

	// 5) Atomically persist the notification + outbox row. The relay drains
	// the outbox into RabbitMQ, so by the time we return we already
	// guarantee at-least-once delivery — the bytes are durable in Postgres.
	if err := n.MarkEnqueued(now); err != nil {
		// Should never happen: Received -> Enqueued is always valid.
		u.Log.Error("invariant violated marking enqueued", "err", err, "id", n.ID)
	}
	payload, err := u.Publisher.Encode(n)
	if err != nil {
		return SubmitOutput{}, fmt.Errorf("encode: %w", err)
	}
	if err := u.Notifications.SubmitWithOutbox(ctx, n, payload); err != nil {
		return SubmitOutput{}, fmt.Errorf("persist+outbox: %w", err)
	}
	u.Metrics.NotificationAccepted(in.Channel.String())
	return SubmitOutput{Notification: n, Duplicate: false}, nil
}

// hydrateRecipient fills in raw email/phone/device-token fields from the user's
// record so workers don't have to re-query.
func (u *SubmitNotification) hydrateRecipient(ctx context.Context, ch domain.Channel, r *domain.Recipient) error {
	uid := *r.UserID
	switch ch {
	case domain.ChannelEmail:
		if !r.Email.Empty() {
			return nil
		}
		usr, err := u.Users.GetUser(ctx, uid)
		if err != nil {
			return fmt.Errorf("hydrate email: %w", err)
		}
		r.Email = usr.Email
	case domain.ChannelSMS:
		if !r.Phone.Empty() {
			return nil
		}
		usr, err := u.Users.GetUser(ctx, uid)
		if err != nil {
			return fmt.Errorf("hydrate sms: %w", err)
		}
		r.Phone = usr.FullPhone()
	case domain.ChannelPushIOS, domain.ChannelPushAndroid:
		if !r.DeviceToken.Empty() {
			return nil
		}
		devices, err := u.Users.DevicesForUser(ctx, uid, ch)
		if err != nil {
			return fmt.Errorf("hydrate push: %w", err)
		}
		if len(devices) == 0 {
			return fmt.Errorf("%w: no registered device for user %d on %s", domain.ErrInvalidInput, uid, ch)
		}
		r.DeviceToken = devices[0].DeviceToken
	}
	return nil
}
