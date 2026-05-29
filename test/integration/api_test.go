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
	return signedRequestOnBehalf(t, method, path, "", body)
}

func signedRequestOnBehalf(t *testing.T, method, path, onBehalfOf string, body []byte) *http.Request {
	t.Helper()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := auth.Sign(appSecret(), ts, method, path, onBehalfOf, body)
	req, err := http.NewRequest(method, host()+path, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(auth.HeaderAppKey, appKey())
	req.Header.Set(auth.HeaderTimestamp, ts)
	req.Header.Set(auth.HeaderSignature, sig)
	if onBehalfOf != "" {
		req.Header.Set("X-On-Behalf-Of-User", onBehalfOf)
	}
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

	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/notifications", "1", body))
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

	r1, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/notifications", "1", body))
	require.NoError(t, err)
	r1.Body.Close()
	require.Equal(t, http.StatusAccepted, r1.StatusCode)

	r2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/notifications", "1", body))
	require.NoError(t, err)
	defer r2.Body.Close()
	require.Equal(t, http.StatusOK, r2.StatusCode) // duplicate -> 200 with same id

	var dup struct {
		Duplicate bool `json:"duplicate"`
	}
	require.NoError(t, json.NewDecoder(r2.Body).Decode(&dup))
	require.True(t, dup.Duplicate)
}

func TestRegisterDevice_OnBehalfOf_HappyPath_204(t *testing.T) {
	body := []byte(`{"device_token":"integration-test-tok","channel":"push_ios"}`)
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/users/42/devices", "42", body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestRegisterDevice_CrossUser_403(t *testing.T) {
	// Signed on behalf of user 42 but path targets user 99 → 403.
	body := []byte(`{"device_token":"integration-test-tok","channel":"push_ios"}`)
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/users/99/devices", "42", body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	var errBody struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	require.Equal(t, "forbidden", errBody.Code)
}

func TestCreateTemplate_OwnerAssignedAutomatically_201(t *testing.T) {
	body := []byte(fmt.Sprintf(`{
		"name":    "it-tpl-%s",
		"channel": "sms",
		"body":    "Hello integration!",
		"version": 1
	}`, uuid.NewString()))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/templates", "42", body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var tpl struct {
		OwnerUserID int64 `json:"owner_user_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tpl))
	require.Equal(t, int64(42), tpl.OwnerUserID)
}

func TestSubmitNotification_CrossUser_403(t *testing.T) {
	// Signed on behalf of user 42 but recipient.user_id is 99 → 403.
	body := []byte(fmt.Sprintf(`{
		"event_id":  "it-cross-%s",
		"channel":   "email",
		"recipient": {"user_id": 99},
		"template_id": "11111111-1111-1111-1111-111111111111",
		"variables": {"Name":"It","Product":"NotifEngine"}
	}`, uuid.NewString()))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/notifications", "42", body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	var errBody struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	require.Equal(t, "forbidden", errBody.Code)
}

func TestUpdateTemplate_HappyPath_200(t *testing.T) {
	// Create a template first.
	name := "it-upd-" + uuid.NewString()
	createBody := []byte(fmt.Sprintf(`{"name":"%s","channel":"sms","body":"Original body.","version":1}`, name))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/templates", "42", createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))

	// Update it.
	updateBody := []byte(`{"name":"updated-name","body":"Updated body."}`)
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "PUT", "/v1/templates/"+created.ID, "42", updateBody))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var updated struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&updated))
	require.Equal(t, "updated-name", updated.Name)
	require.Equal(t, "Updated body.", updated.Body)
}

func TestListTemplates_GroupedByChannel_200(t *testing.T) {
	ownerID := "43"
	suffix := uuid.NewString()

	// Create 2 SMS and 1 email template under the same owner.
	for i, ch := range []string{"sms", "sms", "email"} {
		body := []byte(fmt.Sprintf(`{"name":"it-list-%d-%s","channel":"%s","body":"Body.","version":1}`, i, suffix, ch))
		resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/templates", ownerID, body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	// List all.
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "GET", "/v1/templates", ownerID, nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var grouped map[string][]struct{ Name string `json:"name"` }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&grouped))
	require.GreaterOrEqual(t, len(grouped["sms"]), 2)
	require.GreaterOrEqual(t, len(grouped["email"]), 1)

	// Filter by channel.
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "GET", "/v1/templates?channel=sms", ownerID, nil))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var smsOnly map[string][]struct{ Name string `json:"name"` }
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&smsOnly))
	require.NotContains(t, smsOnly, "email")
}

func TestDeleteDevice_HappyPath_204(t *testing.T) {
	const token = "integration-delete-test-token"

	// Register the device.
	regBody := []byte(fmt.Sprintf(`{"device_token":"%s","channel":"push_ios"}`, token))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/users/42/devices", "42", regBody))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Delete it.
	delBody := []byte(fmt.Sprintf(`{"device_token":"%s","channel":"push_ios"}`, token))
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "DELETE", "/v1/users/42/devices", "42", delBody))
	require.NoError(t, err)
	resp2.Body.Close()
	require.Equal(t, http.StatusNoContent, resp2.StatusCode)

	// Second delete must return 404.
	resp3, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "DELETE", "/v1/users/42/devices", "42", delBody))
	require.NoError(t, err)
	defer resp3.Body.Close()
	require.Equal(t, http.StatusNotFound, resp3.StatusCode)
}
