package dto

import "time"

// SettingView is the JSON shape for a single channel preference entry.
// UpdatedAt is nil when the setting was never explicitly changed (implicit default).
type SettingView struct {
	Channel   string     `json:"channel"`
	OptIn     bool       `json:"opt_in"`
	UpdatedAt *time.Time `json:"updated_at"`
}
