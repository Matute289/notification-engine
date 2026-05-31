package service

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// ListSettings returns the notification preferences for a user across all
// supported channels. Channels with no explicit row in the database are filled
// in with the default (opt-in=true, zero UpdatedAt).
type ListSettings struct {
	Users port.UserRepository
}

func (u *ListSettings) Execute(ctx context.Context, userID int64) ([]domain.Setting, error) {
	stored, err := u.Users.ListSettings(ctx, userID)
	if err != nil {
		return nil, err
	}

	byChannel := make(map[domain.Channel]domain.Setting, len(stored))
	for _, s := range stored {
		byChannel[s.Channel] = s
	}

	all := domain.AllChannels()
	out := make([]domain.Setting, 0, len(all))
	for _, ch := range all {
		if s, ok := byChannel[ch]; ok {
			out = append(out, s)
		} else {
			out = append(out, domain.DefaultSetting(userID, ch))
		}
	}
	return out, nil
}
