package domain

import "errors"

// Sentinel errors. Adapters wrap these so callers can distinguish business
// outcomes from infrastructure failures with errors.Is.
var (
	ErrNotFound                 = errors.New("not found")
	ErrAlreadyExists            = errors.New("already exists")
	ErrInvalidInput             = errors.New("invalid input")
	ErrOptedOut                 = errors.New("recipient has opted out of this channel")
	ErrRateLimited              = errors.New("rate limit exceeded")
	ErrDuplicateEvent           = errors.New("duplicate event_id")
	ErrUnauthenticated          = errors.New("unauthenticated")
	ErrInvalidStatusTransition  = errors.New("invalid notification status transition")
)
