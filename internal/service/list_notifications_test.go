package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListNotifications_DefaultLimit(t *testing.T) {
	// When Limit is 0, service must clamp to 20 before calling repo.
	fake := newFakeNotifications()
	svc := &ListNotifications{Notifications: fake}
	uid := int64(42)
	n := &domain.Notification{ID: uuid.New(), EventID: "e1", Status: domain.StatusSent, Recipient: domain.Recipient{UserID: &uid}}
	fake.byID[n.ID] = n
	fake.byEvent[string(n.EventID)] = n

	out, cursor, err := svc.Execute(context.Background(), ListNotificationsInput{UserID: 42, Limit: 0})
	require.NoError(t, err)
	assert.Equal(t, "", cursor)
	assert.Len(t, out, 1)
}

func TestListNotifications_LimitClamped(t *testing.T) {
	// Limit > 100 must be silently clamped to 100.
	fake := newFakeNotifications()
	svc := &ListNotifications{Notifications: fake}

	out, _, err := svc.Execute(context.Background(), ListNotificationsInput{UserID: 99, Limit: 9999})
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestListNotifications_FiltersForwarded(t *testing.T) {
	// Verify that Channel and Status filters are forwarded to the repo.
	fake := newFakeNotifications()
	svc := &ListNotifications{Notifications: fake}
	uid := int64(7)
	n := &domain.Notification{
		ID: uuid.New(), EventID: "e2", Status: domain.StatusSent,
		Channel: domain.ChannelEmail, Recipient: domain.Recipient{UserID: &uid},
	}
	fake.byID[n.ID] = n
	fake.byEvent[string(n.EventID)] = n

	ch := domain.ChannelEmail
	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	out, _, err := svc.Execute(context.Background(), ListNotificationsInput{
		UserID:  7,
		Limit:   10,
		Channel: &ch,
		Since:   &since,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

func TestListNotifications_Empty(t *testing.T) {
	svc := &ListNotifications{Notifications: newFakeNotifications()}
	out, cursor, err := svc.Execute(context.Background(), ListNotificationsInput{UserID: 1, Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, out)
	assert.Equal(t, "", cursor)
}
