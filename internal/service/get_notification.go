package service

import (
	"context"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// GetNotification reads the current state of a notification by id.
type GetNotification struct {
	Notifications port.NotificationRepository
}

func (u *GetNotification) Execute(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	return u.Notifications.Get(ctx, id)
}
