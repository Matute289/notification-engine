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
// Returns an empty Phone if the concatenated value fails E.164 validation
// (which causes the downstream channel validation to reject it cleanly).
func (u User) FullPhone() Phone {
	if u.PhoneNumber == "" {
		return ""
	}
	p, err := ParsePhone(u.CountryCode + u.PhoneNumber)
	if err != nil {
		return ""
	}
	return p
}
