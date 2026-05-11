package usecase

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/stretchr/testify/require"
)

type submitFixture struct {
	notifications *fakeNotifications
	users         *fakeUsers
	templates     *fakeTemplates
	renderer      *fakeRenderer
	publisher     *fakePublisher
	limiter       *fakeLimiter
	deduper       *fakeDeduper
	metrics       *fakeMetrics
	uc            *SubmitNotification
}

func newSubmitFixture() *submitFixture {
	notifications := newFakeNotifications()
	users := newFakeUsers()
	templates := newFakeTemplates()
	renderer := &fakeRenderer{subject: "Hi", body: "Body"}
	publisher := &fakePublisher{}
	limiter := &fakeLimiter{allowed: true}
	deduper := newFakeDeduper()
	metrics := newFakeMetrics()
	return &submitFixture{
		notifications: notifications,
		users:         users,
		templates:     templates,
		renderer:      renderer,
		publisher:     publisher,
		limiter:       limiter,
		deduper:       deduper,
		metrics:       metrics,
		uc: &SubmitNotification{
			Notifications: notifications,
			Users:         users,
			Templates:     templates,
			Renderer:      renderer,
			Publisher:     publisher,
			Limiter:       limiter,
			Deduper:       deduper,
			Metrics:       metrics,
			Clock:         fixedClock{t: time.Unix(1700000000, 0)},
			Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
			Cfg: SubmitNotificationConfig{
				DedupeTTL:       time.Hour,
				RateLimits:      map[domain.Channel]int{domain.ChannelEmail: 100, domain.ChannelSMS: 5, domain.ChannelPushIOS: 100, domain.ChannelPushAndroid: 100},
				RateLimitWindow: time.Hour,
			},
		},
	}
}

func TestSubmit_HappyPath(t *testing.T) {
	f := newSubmitFixture()
	uid := int64(1)
	f.users.users[uid] = domain.User{ID: uid, Email: "demo@example.com", CountryCode: "+1", PhoneNumber: "5551234567"}

	out, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-1",
		Channel:   domain.ChannelEmail,
		Recipient: domain.Recipient{UserID: &uid},
		Body:      "hi",
	})
	require.NoError(t, err)
	require.False(t, out.Duplicate)
	require.Equal(t, domain.StatusEnqueued, out.Notification.Status)
	require.Len(t, f.notifications.outbox, 1, "outbox row should have been written")
	require.Empty(t, f.publisher.published, "publisher.Publish is not called by SubmitNotification anymore — the relay does it")
	require.Equal(t, 1, f.metrics.accepted[string(domain.ChannelEmail)])
	require.Equal(t, domain.Email("demo@example.com"), out.Notification.Recipient.Email)
}

func TestSubmit_Duplicate_ReturnsExistingWithoutOutboxRow(t *testing.T) {
	f := newSubmitFixture()
	in := SubmitInput{
		EventID:   "evt-dup",
		Channel:   domain.ChannelEmail,
		Recipient: domain.Recipient{Email: "a@b.com"},
		Body:      "hi",
	}
	first, err := f.uc.Execute(context.Background(), in)
	require.NoError(t, err)

	dup, err := f.uc.Execute(context.Background(), in)
	require.NoError(t, err)
	require.True(t, dup.Duplicate)
	require.Equal(t, first.Notification.ID, dup.Notification.ID)
	require.Len(t, f.notifications.outbox, 1, "duplicate request must not write a second outbox row")
}

func TestSubmit_OptedOut(t *testing.T) {
	f := newSubmitFixture()
	uid := int64(2)
	f.users.users[uid] = domain.User{ID: uid, Email: "x@y.com"}
	f.users.settings[uid] = map[domain.Channel]domain.Setting{
		domain.ChannelEmail: {UserID: uid, Channel: domain.ChannelEmail, OptIn: false},
	}
	_, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-2",
		Channel:   domain.ChannelEmail,
		Recipient: domain.Recipient{UserID: &uid},
		Body:      "hi",
	})
	require.True(t, errors.Is(err, domain.ErrOptedOut))
	require.Empty(t, f.notifications.outbox, "opted-out request must not write to the outbox")
}

func TestSubmit_RateLimited(t *testing.T) {
	f := newSubmitFixture()
	f.limiter.allowed = false
	uid := int64(3)
	f.users.users[uid] = domain.User{ID: uid, Email: "x@y.com"}
	_, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-3",
		Channel:   domain.ChannelEmail,
		Recipient: domain.Recipient{UserID: &uid},
		Body:      "hi",
	})
	require.True(t, errors.Is(err, domain.ErrRateLimited))
}

func TestSubmit_HydratesPushDeviceToken(t *testing.T) {
	f := newSubmitFixture()
	uid := int64(4)
	f.users.users[uid] = domain.User{ID: uid}
	f.users.devices[uid] = map[domain.Channel][]domain.Device{
		domain.ChannelPushIOS: {{UserID: uid, Channel: domain.ChannelPushIOS, DeviceToken: "tok-1", LastLoggedInAt: time.Now()}},
	}
	out, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-4",
		Channel:   domain.ChannelPushIOS,
		Recipient: domain.Recipient{UserID: &uid},
		Body:      "hi",
	})
	require.NoError(t, err)
	require.Equal(t, domain.DeviceToken("tok-1"), out.Notification.Recipient.DeviceToken)
}

func TestSubmit_PushWithoutRegisteredDevice(t *testing.T) {
	f := newSubmitFixture()
	uid := int64(5)
	f.users.users[uid] = domain.User{ID: uid}
	_, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-5",
		Channel:   domain.ChannelPushIOS,
		Recipient: domain.Recipient{UserID: &uid},
		Body:      "hi",
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestSubmit_EmptyBodyAndNoTemplate(t *testing.T) {
	f := newSubmitFixture()
	_, err := f.uc.Execute(context.Background(), SubmitInput{
		EventID:   "evt-empty",
		Channel:   domain.ChannelEmail,
		Recipient: domain.Recipient{Email: "a@b.com"},
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}
