package domain

import (
	"fmt"
	"net/mail"
	"strings"
)

// Email is a validated email address.
type Email string

// ParseEmail returns a validated Email. Empty input is allowed (callers decide
// whether absence is acceptable for a given channel).
func ParseEmail(s string) (Email, error) {
	if s == "" {
		return "", nil
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return "", fmt.Errorf("%w: invalid email %q: %v", ErrInvalidInput, s, err)
	}
	return Email(s), nil
}

func (e Email) String() string { return string(e) }
func (e Email) Empty() bool    { return e == "" }

// Phone is a validated E.164-ish phone number.
type Phone string

// ParsePhone returns a validated Phone. Empty input is allowed.
func ParsePhone(s string) (Phone, error) {
	if s == "" {
		return "", nil
	}
	s = strings.TrimSpace(s)
	if len(s) < 7 || len(s) > 16 {
		return "", fmt.Errorf("%w: phone length out of range", ErrInvalidInput)
	}
	for i, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c == '+' && i == 0:
		default:
			return "", fmt.Errorf("%w: invalid phone character %q", ErrInvalidInput, c)
		}
	}
	return Phone(s), nil
}

func (p Phone) String() string { return string(p) }
func (p Phone) Empty() bool    { return p == "" }

// DeviceToken is the opaque token issued by APNs/FCM for a single device.
type DeviceToken string

func (d DeviceToken) String() string { return string(d) }
func (d DeviceToken) Empty() bool    { return d == "" }

// UserID identifies a user. Pointer indicates "unknown" — used when callers
// pass raw email/phone/token without a user reference.
type UserID = int64

// Recipient is the destination of a notification. Exactly one of UserID or the
// channel-specific raw fields should be populated initially; use cases hydrate
// the others when UserID is provided.
type Recipient struct {
	UserID      *UserID     `json:"user_id,omitempty"`
	Email       Email       `json:"email,omitempty"`
	Phone       Phone       `json:"phone_number,omitempty"`
	DeviceToken DeviceToken `json:"device_token,omitempty"`
	MessagingID string      `json:"messaging_id,omitempty"`
}

// Validate checks that the recipient carries enough information for the channel.
func (r Recipient) Validate(c Channel) error {
	if r.UserID == nil && r.Email.Empty() && r.Phone.Empty() && r.DeviceToken.Empty() && r.MessagingID == "" {
		return fmt.Errorf("%w: recipient must carry user_id or a raw destination", ErrInvalidInput)
	}
	if r.UserID != nil {
		// We trust use cases to hydrate the raw destination later.
		return nil
	}
	switch c {
	case ChannelEmail:
		if r.Email.Empty() {
			return fmt.Errorf("%w: email channel needs an email", ErrInvalidInput)
		}
	case ChannelSMS:
		if r.Phone.Empty() {
			return fmt.Errorf("%w: sms channel needs a phone", ErrInvalidInput)
		}
	case ChannelPushIOS, ChannelPushAndroid:
		if r.DeviceToken.Empty() {
			return fmt.Errorf("%w: push channel needs a device token", ErrInvalidInput)
		}
	case ChannelWhatsApp:
		if r.Phone.Empty() {
			return fmt.Errorf("%w: whatsapp channel needs a phone", ErrInvalidInput)
		}
	case ChannelTelegram, ChannelLine, ChannelFacebookMessenger:
		if r.MessagingID == "" {
			return fmt.Errorf("%w: %s channel needs a messaging_id", ErrInvalidInput, c)
		}
	}
	return nil
}
