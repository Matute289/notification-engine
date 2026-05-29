package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// DeleteDevice unregisters a push device identified by channel + token.
type DeleteDevice struct {
	Users port.UserRepository
}

type DeleteDeviceInput struct {
	UserID      int64
	Channel     domain.Channel
	DeviceToken domain.DeviceToken
}

func (u *DeleteDevice) Execute(ctx context.Context, in DeleteDeviceInput) error {
	if !in.Channel.IsPush() {
		return fmt.Errorf("%w: device deletion requires a push channel", domain.ErrInvalidInput)
	}
	if in.DeviceToken.Empty() {
		return fmt.Errorf("%w: device_token required", domain.ErrInvalidInput)
	}
	return u.Users.DeleteDevice(ctx, in.UserID, in.Channel, in.DeviceToken)
}
