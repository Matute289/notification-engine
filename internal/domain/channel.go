package domain

import "fmt"

// Channel identifies a delivery medium. The value-object behaviour (validation,
// equality, parsing) keeps the rest of the codebase from juggling raw strings.
type Channel string

const (
	ChannelPushIOS     Channel = "push_ios"
	ChannelPushAndroid Channel = "push_android"
	ChannelSMS         Channel = "sms"
	ChannelEmail       Channel = "email"
)

// AllChannels returns every supported channel in deterministic order.
func AllChannels() []Channel {
	return []Channel{ChannelPushIOS, ChannelPushAndroid, ChannelSMS, ChannelEmail}
}

// ParseChannel returns the typed channel for s or an error if unknown.
func ParseChannel(s string) (Channel, error) {
	c := Channel(s)
	if !c.Valid() {
		return "", fmt.Errorf("%w: unknown channel %q", ErrInvalidInput, s)
	}
	return c, nil
}

// Valid returns true for the four supported channels.
func (c Channel) Valid() bool {
	switch c {
	case ChannelPushIOS, ChannelPushAndroid, ChannelSMS, ChannelEmail:
		return true
	}
	return false
}

// IsPush is a convenience predicate used by recipient resolution.
func (c Channel) IsPush() bool {
	return c == ChannelPushIOS || c == ChannelPushAndroid
}

func (c Channel) String() string { return string(c) }
