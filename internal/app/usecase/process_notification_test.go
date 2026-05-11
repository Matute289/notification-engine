package usecase

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newProcessFixture(provErr error, max int) (*ProcessNotification, *fakeNotifications, *fakePublisher, *fakeMetrics) {
	notifications := newFakeNotifications()
	publisher := &fakePublisher{}
	metrics := newFakeMetrics()
	uc := &ProcessNotification{
		Notifications: notifications,
		Provider:      &fakeProvider{err: provErr},
		Publisher:     publisher,
		Metrics:       metrics,
		Clock:         fixedClock{t: time.Unix(1700000000, 0)},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:           ProcessNotificationConfig{MaxRetries: max},
	}
	return uc, notifications, publisher, metrics
}

func mkInflightNotification(t *testing.T, ns *fakeNotifications) *domain.Notification {
	t.Helper()
	uid := int64(1)
	now := time.Unix(1700000000, 0)
	n, err := domain.NewNotification(uuid.New(), domain.EventID("evt"), domain.ChannelEmail,
		domain.Recipient{UserID: &uid, Email: "a@b.com"}, nil, nil, "subj", "body", now)
	require.NoError(t, err)
	require.NoError(t, n.MarkEnqueued(now))
	require.NoError(t, ns.Create(context.Background(), n))
	return n
}

func TestProcess_Sent(t *testing.T) {
	uc, ns, _, metrics := newProcessFixture(nil, 5)
	n := mkInflightNotification(t, ns)
	out, err := uc.Execute(context.Background(), ProcessInput{
		Notification: n, RawBody: []byte("{}"), Attempt: 0, Channel: domain.ChannelEmail,
	})
	require.NoError(t, err)
	require.Equal(t, OutcomeSent, out)
	stored, _ := ns.Get(context.Background(), n.ID)
	require.Equal(t, domain.StatusSent, stored.Status)
	require.Equal(t, 1, metrics.sent[string(domain.ChannelEmail)])
}

func TestProcess_TransientThenRetry(t *testing.T) {
	uc, ns, pub, metrics := newProcessFixture(port.ErrTransient, 5)
	n := mkInflightNotification(t, ns)
	out, err := uc.Execute(context.Background(), ProcessInput{
		Notification: n, RawBody: []byte("{}"), Attempt: 1, Channel: domain.ChannelEmail,
	})
	require.NoError(t, err)
	require.Equal(t, OutcomeRetry, out)
	require.Len(t, pub.retries, 1)
	require.Equal(t, 1, metrics.failed[string(domain.ChannelEmail)])
	stored, _ := ns.Get(context.Background(), n.ID)
	require.Equal(t, domain.StatusRetrying, stored.Status)
}

func TestProcess_TransientExhaustedDeadLetters(t *testing.T) {
	uc, ns, _, metrics := newProcessFixture(port.ErrTransient, 5)
	n := mkInflightNotification(t, ns)
	out, err := uc.Execute(context.Background(), ProcessInput{
		Notification: n, RawBody: []byte("{}"), Attempt: 5, Channel: domain.ChannelEmail,
	})
	require.NoError(t, err)
	require.Equal(t, OutcomeDeadLetter, out)
	require.Equal(t, 1, metrics.dead[string(domain.ChannelEmail)])
	stored, _ := ns.Get(context.Background(), n.ID)
	require.Equal(t, domain.StatusDeadLetter, stored.Status)
}

func TestProcess_TerminalErrorDeadLetters(t *testing.T) {
	uc, ns, _, metrics := newProcessFixture(errBoom, 5)
	n := mkInflightNotification(t, ns)
	out, err := uc.Execute(context.Background(), ProcessInput{
		Notification: n, RawBody: []byte("{}"), Attempt: 0, Channel: domain.ChannelEmail,
	})
	require.NoError(t, err)
	require.Equal(t, OutcomeDeadLetter, out)
	require.Equal(t, 1, metrics.dead[string(domain.ChannelEmail)])
}
