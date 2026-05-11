package domain

import (
	"time"

	"github.com/google/uuid"
)

// Notification is the central aggregate. The struct is exported so adapters can
// hydrate it from rows / messages, but state-changing methods enforce the
// invariants of the life-cycle state machine.
type Notification struct {
	ID         uuid.UUID
	EventID    EventID
	Channel    Channel
	Recipient  Recipient
	TemplateID *uuid.UUID
	Variables  map[string]string
	Subject    string
	Body       string
	Attempt    int
	Status     Status
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewNotification constructs a notification in the Received state. It runs
// the cross-field invariants (recipient/channel match) so use cases get a
// validated entity or an error before they ever talk to a port.
func NewNotification(
	id uuid.UUID,
	eventID EventID,
	channel Channel,
	recipient Recipient,
	templateID *uuid.UUID,
	variables map[string]string,
	subject, body string,
	now time.Time,
) (*Notification, error) {
	if !channel.Valid() {
		return nil, ErrInvalidInput
	}
	if err := recipient.Validate(channel); err != nil {
		return nil, err
	}
	return &Notification{
		ID:         id,
		EventID:    eventID,
		Channel:    channel,
		Recipient:  recipient,
		TemplateID: templateID,
		Variables:  variables,
		Subject:    subject,
		Body:       body,
		Status:     StatusReceived,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// SetRendered records the rendered subject/body. Cannot be called after the
// notification has left the Received state — once enqueued, the body is frozen.
func (n *Notification) SetRendered(subject, body string) error {
	if n.Status != StatusReceived {
		return errInvalidTransition{From: n.Status, To: n.Status}
	}
	n.Subject, n.Body = subject, body
	return nil
}

// HydrateRecipient overlays a hydrated recipient (use case responsibility) on
// top of the original. Same restriction as SetRendered.
func (n *Notification) HydrateRecipient(r Recipient) error {
	if n.Status != StatusReceived {
		return errInvalidTransition{From: n.Status, To: n.Status}
	}
	n.Recipient = r
	return nil
}

func (n *Notification) MarkEnqueued(now time.Time) error {
	return n.transitionTo(StatusEnqueued, "", now)
}

func (n *Notification) MarkInFlight(attempt int, now time.Time) error {
	if err := n.transitionTo(StatusInFlight, "", now); err != nil {
		return err
	}
	n.Attempt = attempt
	return nil
}

func (n *Notification) MarkSent(now time.Time) error {
	return n.transitionTo(StatusSent, "", now)
}

func (n *Notification) MarkRetrying(reason string, now time.Time) error {
	return n.transitionTo(StatusRetrying, reason, now)
}

func (n *Notification) MarkDeadLetter(reason string, now time.Time) error {
	return n.transitionTo(StatusDeadLetter, reason, now)
}

func (n *Notification) transitionTo(next Status, reason string, now time.Time) error {
	if !n.Status.canTransitionTo(next) {
		return errInvalidTransition{From: n.Status, To: next}
	}
	n.Status = next
	n.LastError = reason
	n.UpdatedAt = now
	return nil
}
