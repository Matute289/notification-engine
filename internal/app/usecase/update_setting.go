package usecase

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
)

// UpdateSetting flips opt-in for one (user, channel) tuple.
type UpdateSetting struct {
	Users port.UserRepository
	Clock port.Clock
}

type UpdateSettingInput struct {
	UserID  int64
	Channel domain.Channel
	OptIn   bool
}

func (u *UpdateSetting) Execute(ctx context.Context, in UpdateSettingInput) error {
	if !in.Channel.Valid() {
		return fmt.Errorf("%w: invalid channel", domain.ErrInvalidInput)
	}
	return u.Users.UpsertSetting(ctx, domain.Setting{
		UserID:    in.UserID,
		Channel:   in.Channel,
		OptIn:     in.OptIn,
		UpdatedAt: u.Clock.Now(),
	})
}
