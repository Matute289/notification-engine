package usecase

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkStuckNotification(t *testing.T, ns *fakeNotifications, age time.Duration) *domain.Notification {
	t.Helper()
	uid := int64(1)
	now := time.Now().Add(-age)
	n, err := domain.NewNotification(uuid.New(), domain.EventID(uuid.NewString()), domain.ChannelEmail,
		domain.Recipient{UserID: &uid, Email: "a@b.com"}, nil, nil, "subj", "body", now)
	require.NoError(t, err)
	require.NoError(t, n.MarkEnqueued(now))
	require.NoError(t, n.MarkInFlight(1, now))
	require.NoError(t, ns.Create(context.Background(), n))
	return n
}

func TestRescueStuck_RepublishesAndResetsStatus(t *testing.T) {
	ns := newFakeNotifications()
	pub := &fakePublisher{}
	mkStuckNotification(t, ns, 10*time.Minute)
	mkStuckNotification(t, ns, 10*time.Minute)

	uc := &RescueStuckNotifications{
		Notifications: ns,
		Publisher:     pub,
		Clock:         fixedClock{t: time.Now()},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:           RescueStuckConfig{StuckThreshold: time.Minute, BatchSize: 100},
	}
	res, err := uc.Execute(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, res.Examined)
	require.Equal(t, 2, res.Rescued)
	require.Equal(t, 0, res.Errors)
	require.Len(t, pub.published, 2)

	for _, n := range ns.byID {
		require.Equal(t, domain.StatusEnqueued, n.Status, "rescued notification should be enqueued")
	}
}

func TestRescueStuck_IgnoresFreshInFlight(t *testing.T) {
	ns := newFakeNotifications()
	pub := &fakePublisher{}
	mkStuckNotification(t, ns, 10*time.Second) // recent — should be skipped

	uc := &RescueStuckNotifications{
		Notifications: ns,
		Publisher:     pub,
		Clock:         fixedClock{t: time.Now()},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:           RescueStuckConfig{StuckThreshold: time.Minute, BatchSize: 100},
	}
	res, err := uc.Execute(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, res.Examined)
	require.Empty(t, pub.published)
}
