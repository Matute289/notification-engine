package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestDeleteDevice_HappyPath(t *testing.T) {
	users := newFakeUsers()
	users.devices[42] = map[domain.Channel][]domain.Device{
		domain.ChannelPushIOS: {{UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "tok", LastLoggedInAt: time.Now()}},
	}
	svc := &DeleteDevice{Users: users}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "tok",
	})
	require.NoError(t, err)
	require.Empty(t, users.devices[42][domain.ChannelPushIOS])
}

func TestDeleteDevice_NonPushChannel_Rejected(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelEmail, DeviceToken: "tok",
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeleteDevice_EmptyToken_Rejected(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "",
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeleteDevice_NotFound(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "unknown",
	})
	require.True(t, errors.Is(err, domain.ErrNotFound))
}
