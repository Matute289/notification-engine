package sendgrid

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkNotification(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelEmail,
		domain.Recipient{UserID: &uid, Email: "to@example.com"},
		nil, nil, "Subject", "<p>Body</p>", mustNow(t))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/mail/send", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		var got sendRequest
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, "Subject", got.Subject)
		require.Equal(t, "to@example.com", got.Personalizations[0].To[0].Email)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p, err := New(Config{APIKey: "test-key", FromEmail: "from@x.com", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkNotification(t)))
}

func TestSend_5xxIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", FromEmail: "f@x", BaseURL: srv.URL})
	err := p.Send(context.Background(), mkNotification(t))
	require.True(t, errors.Is(err, port.ErrTransient))
}

func TestSend_4xxIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", FromEmail: "f@x", BaseURL: srv.URL})
	err := p.Send(context.Background(), mkNotification(t))
	require.Error(t, err)
	require.False(t, errors.Is(err, port.ErrTransient))
}
