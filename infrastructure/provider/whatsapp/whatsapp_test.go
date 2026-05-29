package whatsapp

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

func mkWhatsAppNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelWhatsApp,
		domain.Recipient{UserID: &uid, Phone: "+15551234567"},
		nil, nil, "", "Hello from WhatsApp!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/PHONE_ID/messages")
		require.Equal(t, "Bearer TOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.1"}]}`))
	}))
	defer srv.Close()

	p, err := New(Config{PhoneNumberID: "PHONE_ID", AccessToken: "TOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkWhatsAppNotif(t)))
	require.Equal(t, "whatsapp", captured["messaging_product"])
	require.Equal(t, "+15551234567", captured["to"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{PhoneNumberID: "P", AccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkWhatsAppNotif(t)), port.ErrTransient))
}
