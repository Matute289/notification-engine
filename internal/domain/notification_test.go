package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestNotification(t *testing.T) *Notification {
	t.Helper()
	uid := int64(7)
	n, err := NewNotification(
		uuid.New(),
		EventID("evt-1"),
		ChannelEmail,
		Recipient{UserID: &uid, Email: "a@b.com"},
		nil, nil,
		"hi", "body",
		time.Unix(1700000000, 0),
	)
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}
	return n
}

func TestNewNotification_Defaults(t *testing.T) {
	n := newTestNotification(t)
	if n.Status != StatusReceived {
		t.Errorf("status: got %q, want %q", n.Status, StatusReceived)
	}
	if n.Attempt != 0 {
		t.Errorf("attempt: got %d, want 0", n.Attempt)
	}
}

func TestNewNotification_RejectsBadChannel(t *testing.T) {
	uid := int64(1)
	if _, err := NewNotification(uuid.New(), "evt", "fax",
		Recipient{UserID: &uid}, nil, nil, "", "x", time.Now()); err == nil {
		t.Fatal("expected error for invalid channel")
	}
}

func TestNotification_HappyPathTransitions(t *testing.T) {
	n := newTestNotification(t)
	now := time.Unix(1700000001, 0)
	if err := n.MarkEnqueued(now); err != nil {
		t.Fatalf("MarkEnqueued: %v", err)
	}
	if err := n.MarkInFlight(1, now); err != nil {
		t.Fatalf("MarkInFlight: %v", err)
	}
	if err := n.MarkSent(now); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if !n.Status.Terminal() {
		t.Fatal("Sent should be terminal")
	}
}

func TestNotification_RetryPath(t *testing.T) {
	n := newTestNotification(t)
	now := time.Unix(1700000001, 0)
	_ = n.MarkEnqueued(now)
	_ = n.MarkInFlight(1, now)
	if err := n.MarkRetrying("boom", now); err != nil {
		t.Fatal(err)
	}
	if err := n.MarkInFlight(2, now); err != nil {
		t.Fatalf("Retrying -> InFlight should be allowed: %v", err)
	}
	if err := n.MarkDeadLetter("perma", now); err != nil {
		t.Fatal(err)
	}
}

func TestNotification_RejectsIllegalTransitions(t *testing.T) {
	n := newTestNotification(t) // Received
	if err := n.MarkSent(time.Now()); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("Received -> Sent should be rejected; got %v", err)
	}
	if err := n.MarkRetrying("nope", time.Now()); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("Received -> Retrying should be rejected; got %v", err)
	}
}

func TestNotification_CannotMutateAfterEnqueued(t *testing.T) {
	n := newTestNotification(t)
	_ = n.MarkEnqueued(time.Now())
	if err := n.SetRendered("subj", "body"); err == nil {
		t.Fatal("SetRendered should fail after enqueue")
	}
}
