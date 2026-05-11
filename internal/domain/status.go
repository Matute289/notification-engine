package domain

import "fmt"

// Status is the life-cycle state stored on every notification_log row.
// Transitions between states are restricted to those listed in validTransitions;
// the rich Notification entity uses these rules to enforce the state machine.
type Status string

const (
	StatusReceived   Status = "received"
	StatusEnqueued   Status = "enqueued"
	StatusInFlight   Status = "in_flight"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
	StatusRetrying   Status = "retrying"
	StatusDeadLetter Status = "dead_letter"
)

// validTransitions encodes the allowed state moves. Any move not listed here is
// rejected by Notification.transitionTo.
var validTransitions = map[Status]map[Status]struct{}{
	StatusReceived: {StatusEnqueued: {}, StatusFailed: {}},
	StatusEnqueued: {StatusInFlight: {}, StatusFailed: {}},
	StatusInFlight: {
		StatusSent:       {},
		StatusFailed:     {},
		StatusRetrying:   {},
		StatusDeadLetter: {},
	},
	StatusRetrying:   {StatusInFlight: {}, StatusDeadLetter: {}, StatusEnqueued: {}},
	StatusFailed:     {StatusRetrying: {}, StatusDeadLetter: {}},
	StatusSent:       {}, // terminal
	StatusDeadLetter: {}, // terminal
}

// Terminal reports whether the status admits no further transitions.
func (s Status) Terminal() bool {
	to, ok := validTransitions[s]
	return ok && len(to) == 0
}

func (s Status) canTransitionTo(next Status) bool {
	allowed, ok := validTransitions[s]
	if !ok {
		return false
	}
	_, ok = allowed[next]
	return ok
}

func (s Status) String() string { return string(s) }

// errInvalidTransition is wrapped into ErrInvalidStatusTransition so callers
// can pattern-match without losing the from/to detail.
type errInvalidTransition struct{ From, To Status }

func (e errInvalidTransition) Error() string {
	return fmt.Sprintf("invalid status transition %s -> %s", e.From, e.To)
}
func (errInvalidTransition) Is(target error) bool { return target == ErrInvalidStatusTransition }
