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

// ListNotificationsParams carries all optional filter and pagination state for
// the user-scoped notification list query. Limit must always be set (service
// clamps it); the rest are optional.
type ListNotificationsParams struct {
	UserID  int64
	Limit   int             // must be set; service enforces 1–100
	Cursor  string          // empty = first page; opaque base64 from previous response
	Channel *domain.Channel // nil = no filter
	Status  *domain.Status  // nil = no filter
	Since   *time.Time      // nil = no filter
	Until   *time.Time      // nil = no filter
}

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

	// List returns up to params.Limit notifications owned by params.UserID,
	// ordered by (created_at DESC, id DESC). nextCursor is empty when no further
	// page exists. The cursor is opaque to callers.
	List(ctx context.Context, params ListNotificationsParams) (items []domain.Notification, nextCursor string, err error)
}

// TemplateRepository persists notification templates.
type TemplateRepository interface {
	Create(ctx context.Context, t domain.Template) error
	Get(ctx context.Context, id uuid.UUID) (domain.Template, error)
	Update(ctx context.Context, t domain.Template) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error)
}

// UserRepository persists users, devices, and per-channel settings.
type UserRepository interface {
	GetUser(ctx context.Context, id int64) (domain.User, error)
	DevicesForUser(ctx context.Context, userID int64, channel domain.Channel) ([]domain.Device, error)
	UpsertDevice(ctx context.Context, d domain.Device) error
	DeleteDevice(ctx context.Context, userID int64, channel domain.Channel, token domain.DeviceToken) error
	GetSetting(ctx context.Context, userID int64, channel domain.Channel) (domain.Setting, error)
	UpsertSetting(ctx context.Context, s domain.Setting) error

	// ListSettings returns the explicit opt-in rows for userID. Channels with no
	// row use the DefaultSetting (opt-in=true). The service layer fills in those
	// defaults so callers always see every channel.
	ListSettings(ctx context.Context, userID int64) ([]domain.Setting, error)
}
