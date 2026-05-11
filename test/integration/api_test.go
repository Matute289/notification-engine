//go:build integration

// Integration tests assume a running compose stack (`make up`). They exercise
// the public API end-to-end: signed POST → DB row → queue consumed by worker
// → status transitions to `sent`.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/platform/auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func host() string {
	if h := os.Getenv("API_HOST"); h != "" {
		return h
	}
	return "http://localhost:8080"
}

func appKey() string {
	if k := os.Getenv("APP_KEY"); k != "" {
		return k
	}
	return "demo-app"
}

func appSecret() string {
	if s := os.Getenv("APP_SECRET"); s != "" {
		return s
	}
	return "demo-secret-please-change"
}

func signedRequest(t *testing.T, method, path string, body []byte) *http.Request {
	t.Helper()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := auth.Sign(appSecret(), ts, method, path, body)
	req, err := http.NewRequest(method, host()+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(auth.HeaderAppKey, appKey())
	req.Header.Set(auth.HeaderTimestamp, ts)
	req.Header.Set(auth.HeaderSignature, sig)
	return req
}

func TestSubmitNotificationEndToEnd(t *testing.T) {
	body := []byte(fmt.Sprintf(`{
        "event_id": "it-%s",
        "channel": "email",
        "recipient": {"user_id": 1},
        "template_id": "11111111-1111-1111-1111-111111111111",
        "variables": {"Name":"It","Product":"NotifEngine"}
    }`, uuid.NewString()))

	resp, err := http.DefaultClient.Do(signedRequest(t, "POST", "/v1/notifications", body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var sub struct {
		NotificationID uuid.UUID `json:"notification_id"`
		Status         string    `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sub))
	require.NotEqual(t, uuid.Nil, sub.NotificationID)

	// Worker should pick this up within a few seconds.
	require.Eventually(t, func() bool {
		path := "/v1/notifications/" + sub.NotificationID.String()
		req := signedRequest(t, "GET", path, nil)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		var out struct{ Status string `json:"status"` }
		_ = json.NewDecoder(r.Body).Decode(&out)
		return out.Status == "sent"
	}, 10*time.Second, 200*time.Millisecond)
}

func TestDuplicateEventCollapses(t *testing.T) {
	eid := "it-dup-" + uuid.NewString()
	body := []byte(fmt.Sprintf(`{
        "event_id": "%s",
        "channel": "email",
        "recipient": {"user_id": 1},
        "template_id": "11111111-1111-1111-1111-111111111111",
        "variables": {"Name":"Dup","Product":"NotifEngine"}
    }`, eid))

	r1, err := http.DefaultClient.Do(signedRequest(t, "POST", "/v1/notifications", body))
	require.NoError(t, err)
	r1.Body.Close()
	require.Equal(t, http.StatusAccepted, r1.StatusCode)

	r2, err := http.DefaultClient.Do(signedRequest(t, "POST", "/v1/notifications", body))
	require.NoError(t, err)
	defer r2.Body.Close()
	require.Equal(t, http.StatusOK, r2.StatusCode) // duplicate -> 200 with same id

	var dup struct {
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.NewDecoder(r2.Body).Decode(&dup))
	require.True(t, dup.Duplicate)
}
