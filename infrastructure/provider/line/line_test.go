package line

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

func mkLineNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelLine,
		domain.Recipient{UserID: &uid, MessagingID: "Uf1234567890"},
		nil, nil, "", "Hello from Line!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/bot/message/push", r.URL.Path)
		require.Equal(t, "Bearer LINETOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p, err := New(Config{ChannelAccessToken: "LINETOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkLineNotif(t)))
	require.Equal(t, "Uf1234567890", captured["to"])
	msgs := captured["messages"].([]any)
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello from Line!", msgs[0].(map[string]any)["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{ChannelAccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkLineNotif(t)), port.ErrTransient))
}
