package service

import (
	"context"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// ListNotifications fetches a cursor-paginated page of notifications for a user.
type ListNotifications struct {
	Notifications port.NotificationRepository
}

// ListNotificationsInput carries caller-supplied parameters. Limit is clamped
// to [1, 100]; zero is treated as 20.
type ListNotificationsInput struct {
	UserID  int64
	Limit   int
	Cursor  string
	Channel *domain.Channel
	Status  *domain.Status
	Since   *time.Time
	Until   *time.Time
}

func (u *ListNotifications) Execute(ctx context.Context, in ListNotificationsInput) ([]domain.Notification, string, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return u.Notifications.List(ctx, port.ListNotificationsParams{
		UserID:  in.UserID,
		Limit:   limit,
		Cursor:  in.Cursor,
		Channel: in.Channel,
		Status:  in.Status,
		Since:   in.Since,
		Until:   in.Until,
	})
}
