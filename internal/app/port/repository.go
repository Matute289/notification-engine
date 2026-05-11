// Package port defines the interfaces that application use cases depend on.
// Outbound adapters (postgres, redis, rabbitmq, ...) implement these so that
// use cases stay free of any concrete infrastructure.
//
// Convention: a port is named after what the *application* needs ("Notification
// Repository", "EventPublisher"), not after the technology that fulfils it.
package port

import (
	"context"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// NotificationRepository persists the Notification aggregate and its
// per-event analytics records.
type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status, attempt int, lastError string) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	GetByEventID(ctx context.Context, eventID domain.EventID) (*domain.Notification, error)
	RecordEvent(ctx context.Context, notificationID uuid.UUID, eventType string, metadata map[string]any) error

	// ListStuckInFlight returns up to limit notifications that have been in
	// the InFlight state longer than threshold. Used by the janitor to rescue
	// rows whose worker died after MarkInFlight but before ack/nack.
	ListStuckInFlight(ctx context.Context, threshold time.Duration, limit int) ([]*domain.Notification, error)
}

// TemplateRepository persists notification templates.
type TemplateRepository interface {
	Create(ctx context.Context, t domain.Template) error
	Get(ctx context.Context, id uuid.UUID) (domain.Template, error)
}

// UserRepository persists users, devices, and per-channel settings.
type UserRepository interface {
	GetUser(ctx context.Context, id int64) (domain.User, error)
	DevicesForUser(ctx context.Context, userID int64, channel domain.Channel) ([]domain.Device, error)
	UpsertDevice(ctx context.Context, d domain.Device) error
	GetSetting(ctx context.Context, userID int64, channel domain.Channel) (domain.Setting, error)
	UpsertSetting(ctx context.Context, s domain.Setting) error
}
