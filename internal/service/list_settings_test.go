package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSettings_AllDefaultsForNewUser(t *testing.T) {
	// A user with no explicit rows gets all 8 channels with opt_in=true.
	svc := &ListSettings{Users: newFakeUsers()}
	got, err := svc.Execute(context.Background(), 42)
	require.NoError(t, err)
	assert.Len(t, got, len(domain.AllChannels()))
	for _, s := range got {
		assert.True(t, s.OptIn, "channel %s should default to opt-in", s.Channel)
		assert.True(t, s.UpdatedAt.IsZero(), "default setting should have zero UpdatedAt")
	}
}

func TestListSettings_MergesExplicitRows(t *testing.T) {
	// Explicit opt-out for sms overrides the default; all other channels stay true.
	fake := newFakeUsers()
	fake.settings[42] = map[domain.Channel]domain.Setting{
		domain.ChannelSMS: {UserID: 42, Channel: domain.ChannelSMS, OptIn: false, UpdatedAt: time.Date(2025, 3, 10, 0, 0, 0, 0, time.UTC)},
	}
	svc := &ListSettings{Users: fake}
	got, err := svc.Execute(context.Background(), 42)
	require.NoError(t, err)
	assert.Len(t, got, len(domain.AllChannels()))

	byChannel := make(map[domain.Channel]domain.Setting)
	for _, s := range got {
		byChannel[s.Channel] = s
	}
	assert.False(t, byChannel[domain.ChannelSMS].OptIn)
	assert.False(t, byChannel[domain.ChannelSMS].UpdatedAt.IsZero())
	assert.True(t, byChannel[domain.ChannelEmail].OptIn)
	assert.True(t, byChannel[domain.ChannelEmail].UpdatedAt.IsZero())
}

func TestListSettings_OrderFollowsAllChannels(t *testing.T) {
	svc := &ListSettings{Users: newFakeUsers()}
	got, err := svc.Execute(context.Background(), 1)
	require.NoError(t, err)
	all := domain.AllChannels()
	require.Len(t, got, len(all))
	for i, ch := range all {
		assert.Equal(t, ch, got[i].Channel)
	}
}
