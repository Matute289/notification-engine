package apns

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeAuth struct{ tok string }

func (f fakeAuth) Authorization(_ context.Context) (string, error) {
	return "bearer " + f.tok, nil
}

func mkPush(t *testing.T) *domain.Notification {
	t.Helper()
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelPushIOS,
		domain.Recipient{DeviceToken: "tok-123"},
		nil, nil, "Title", "Body", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasPrefix(r.URL.Path, "/3/device/"))
		require.Equal(t, "com.example.app", r.Header.Get("apns-topic"))
		require.Equal(t, "alert", r.Header.Get("apns-push-type"))
		require.Equal(t, "bearer T", r.Header.Get("Authorization"))
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &body))
		aps := body["aps"].(map[string]any)
		alert := aps["alert"].(map[string]any)
		require.Equal(t, "Title", alert["title"])
		require.Equal(t, "Body", alert["body"])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := New(Config{
		BundleID: "com.example.app", BaseURL: srv.URL, Auth: fakeAuth{tok: "T"},
	})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkPush(t)))
}

func TestSend_5xxIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	p, _ := New(Config{BundleID: "com.example", BaseURL: srv.URL, Auth: fakeAuth{}})
	err := p.Send(context.Background(), mkPush(t))
	require.True(t, errors.Is(err, port.ErrTransient))
}
