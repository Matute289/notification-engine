package telegram

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

func mkTelegramNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelTelegram,
		domain.Recipient{UserID: &uid, MessagingID: "123456789"},
		nil, nil, "", "Hello from Telegram!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/botTOKEN/sendMessage")
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	p, err := New(Config{BotToken: "TOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkTelegramNotif(t)))
	require.Equal(t, "123456789", captured["chat_id"])
	require.Equal(t, "Hello from Telegram!", captured["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{BotToken: "TOKEN", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkTelegramNotif(t)), port.ErrTransient))
}

func TestSend_EmptyMessagingID_Error(t *testing.T) {
	p, _ := New(Config{BotToken: "TOKEN"})
	uid := int64(1)
	n, _ := domain.NewNotification(uuid.New(), "e", domain.ChannelTelegram,
		domain.Recipient{UserID: &uid, MessagingID: ""}, nil, nil, "", "body", time.Now())
	require.Error(t, p.Send(context.Background(), n))
}
