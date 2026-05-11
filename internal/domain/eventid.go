package domain

import "fmt"

// EventID is the caller-supplied idempotency key. Validating it as a domain
// type keeps the constraint in one place and out of every handler.
type EventID string

// ParseEventID validates the supplied string. Length bounds are deliberately
// generous — callers commonly use UUIDs, ULIDs, or composite keys.
func ParseEventID(s string) (EventID, error) {
	if len(s) == 0 || len(s) > 256 {
		return "", fmt.Errorf("%w: event_id length must be 1..256", ErrInvalidInput)
	}
	return EventID(s), nil
}

func (e EventID) String() string { return string(e) }
