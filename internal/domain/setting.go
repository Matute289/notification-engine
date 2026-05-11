package domain

import "time"

// Setting captures a user's opt-in state per channel. Default (no row) is opt-in.
type Setting struct {
	UserID    int64
	Channel   Channel
	OptIn     bool
	UpdatedAt time.Time
}

// DefaultSetting returns the implicit setting used when no row exists.
func DefaultSetting(userID int64, channel Channel) Setting {
	return Setting{UserID: userID, Channel: channel, OptIn: true}
}
