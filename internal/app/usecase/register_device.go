package usecase

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
)

// RegisterDevice persists/refreshes a user's push-notification device token.
type RegisterDevice struct {
	Users port.UserRepository
	Clock port.Clock
}

type RegisterDeviceInput struct {
	UserID      int64
	Channel     domain.Channel
	DeviceToken domain.DeviceToken
}

func (u *RegisterDevice) Execute(ctx context.Context, in RegisterDeviceInput) error {
	if !in.Channel.IsPush() {
		return fmt.Errorf("%w: device registration requires a push channel", domain.ErrInvalidInput)
	}
	if in.DeviceToken.Empty() {
		return fmt.Errorf("%w: device_token required", domain.ErrInvalidInput)
	}
	return u.Users.UpsertDevice(ctx, domain.Device{
		UserID:         in.UserID,
		Channel:        in.Channel,
		DeviceToken:    in.DeviceToken,
		LastLoggedInAt: u.Clock.Now(),
	})
}
