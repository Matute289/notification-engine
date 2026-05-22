package fcm

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

type fakeTS struct{ tok string }

func (f fakeTS) Token(_ context.Context) (string, error) { return f.tok, nil }

func mkPush(t *testing.T) *domain.Notification {
	t.Helper()
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelPushAndroid,
		domain.Recipient{DeviceToken: "tok-android"},
		nil, nil, "Title", "Body", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.True(t, strings.HasSuffix(r.URL.Path, "/v1/projects/proj-1/messages:send"))
		require.Equal(t, "Bearer fake-token", r.Header.Get("Authorization"))
		raw, _ := io.ReadAll(r.Body)
		var got sendRequest
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, "tok-android", got.Message.Token)
		require.Equal(t, "Title", got.Message.Notification.Title)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p, err := New(Config{
		ProjectID: "proj-1", BaseURL: srv.URL, TokenSource: fakeTS{tok: "fake-token"},
	})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkPush(t)))
}

func TestSend_4xxIsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	p, _ := New(Config{ProjectID: "p", BaseURL: srv.URL, TokenSource: fakeTS{}})
	err := p.Send(context.Background(), mkPush(t))
	require.Error(t, err)
	require.False(t, errors.Is(err, port.ErrTransient))
}
