package fbmessenger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkFBNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelFacebookMessenger,
		domain.Recipient{UserID: &uid, MessagingID: "987654321"},
		nil, nil, "", "Hello from Messenger!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/me/messages", r.URL.Path)
		require.Equal(t, "Bearer FBTOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message_id":"mid.1"}`))
	}))
	defer srv.Close()

	p, err := New(Config{PageAccessToken: "FBTOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkFBNotif(t)))
	recipient := captured["recipient"].(map[string]any)
	require.Equal(t, "987654321", recipient["id"])
	message := captured["message"].(map[string]any)
	require.Equal(t, "Hello from Messenger!", message["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{PageAccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkFBNotif(t)), port.ErrTransient))
}
