package domain

import "time"

// User mirrors the contact info captured at signup (PDF Figure 10-8).
type User struct {
	ID          int64
	Email       Email
	CountryCode string
	PhoneNumber string
	CreatedAt   time.Time
}

// FullPhone returns the dialable form (country code + national number).
func (u User) FullPhone() Phone {
	if u.PhoneNumber == "" {
		return ""
	}
	return Phone(u.CountryCode + u.PhoneNumber)
}
