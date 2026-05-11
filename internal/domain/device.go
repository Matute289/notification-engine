package domain

import "time"

// Device is one of a user's push-notification destinations.
type Device struct {
	ID             int64
	UserID         int64
	DeviceToken    DeviceToken
	Channel        Channel
	LastLoggedInAt time.Time
}
