package twilio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkSMS(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelSMS,
		domain.Recipient{UserID: &uid, Phone: "+15555550100"},
		nil, nil, "", "hello", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/Accounts/SID/Messages.json")
		user, pass, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "SID", user)
		require.Equal(t, "TOKEN", pass)
		body, _ := io.ReadAll(r.Body)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "+15555550100", form.Get("To"))
		require.Equal(t, "+15555550999", form.Get("From"))
		require.Equal(t, "hello", form.Get("Body"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	p, err := New(Config{
		AccountSID: "SID", AuthToken: "TOKEN",
		FromNumber: "+15555550999", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkSMS(t)))
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{AccountSID: "SID", AuthToken: "T", FromNumber: "+1", BaseURL: srv.URL})
	err := p.Send(context.Background(), mkSMS(t))
	require.True(t, errors.Is(err, port.ErrTransient))
}
