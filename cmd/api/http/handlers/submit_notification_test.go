package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildSubmitHandler wires a SubmitNotification service with the given fakes.
func buildSubmitHandler(repo *notifRepo, users *userRepo, ded *deduper, lim *limiter) *Handler {
	return &Handler{
		SubmitSvc: &service.SubmitNotification{
			Notifications: repo,
			Users:         users,
			Templates:     &templateRepo{},
			Renderer:      nopRenderer{},
			Publisher:     nopPublisher{},
			Limiter:       lim,
			Deduper:       ded,
			Metrics:       nopMetrics{},
			Clock:         fixedClock{},
			Log:           slog.Default(),
			Cfg: service.SubmitNotificationConfig{
				DedupeTTL:       24 * time.Hour,
				RateLimits:      map[domain.Channel]int{domain.ChannelSMS: 100},
				RateLimitWindow: time.Hour,
			},
		},
	}
}

func postNotification(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewBufferString(body))
}

func TestSubmitNotification_InvalidJSON(t *testing.T) {
	h := &Handler{SubmitSvc: &service.SubmitNotification{}}
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{bad}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestSubmitNotification_InvalidChannel(t *testing.T) {
	h := &Handler{SubmitSvc: &service.SubmitNotification{}}
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{"event_id":"e1","channel":"fax"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestSubmitNotification_InvalidEventID(t *testing.T) {
	h := &Handler{SubmitSvc: &service.SubmitNotification{}}
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{"event_id":"","channel":"sms"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_event_id")
}

func TestSubmitNotification_InvalidEmail(t *testing.T) {
	h := &Handler{SubmitSvc: &service.SubmitNotification{}}
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{"event_id":"e1","channel":"email","recipient":{"email":"not-an-email"}}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_email")
}

func TestSubmitNotification_InvalidPhone(t *testing.T) {
	h := &Handler{SubmitSvc: &service.SubmitNotification{}}
	w := httptest.NewRecorder()
	// Phone "123" is too short (< 7 chars)
	h.SubmitNotification(w, postNotification(`{"event_id":"e1","channel":"sms","recipient":{"phone_number":"123"}}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_phone")
}

func TestSubmitNotification_HappyPath_202(t *testing.T) {
	h := buildSubmitHandler(
		&notifRepo{},
		&userRepo{setting: domain.DefaultSetting(0, domain.ChannelSMS)},
		&deduper{claimed: true},
		&limiter{allowed: true},
	)
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{
		"event_id":  "evt-001",
		"channel":   "sms",
		"recipient": {"phone_number": "+15551234567"},
		"body":      "Hello!"
	}`))
	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp dto.SubmitResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEqual(t, uuid.Nil, resp.NotificationID)
	assert.False(t, resp.Duplicate)
}

func TestSubmitNotification_Duplicate_200(t *testing.T) {
	existing := &domain.Notification{
		ID:      uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		EventID: "evt-dup",
	}
	h := buildSubmitHandler(
		&notifRepo{getByEvtResult: existing},
		&userRepo{},
		&deduper{claimed: false}, // false = already claimed → look up existing
		&limiter{allowed: true},
	)
	w := httptest.NewRecorder()
	h.SubmitNotification(w, postNotification(`{
		"event_id":  "evt-dup",
		"channel":   "sms",
		"recipient": {"phone_number": "+15551234567"},
		"body":      "Hello!"
	}`))
	assert.Equal(t, http.StatusOK, w.Code)
	var resp dto.SubmitResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Duplicate)
	assert.Equal(t, existing.ID, resp.NotificationID)
}

func TestSubmitNotification_CrossUser_403(t *testing.T) {
	// HMAC service acting on behalf of user 42 tries to submit for user 99 → 403.
	uid := int64(42)
	h := buildSubmitHandler(
		&notifRepo{},
		&userRepo{},
		&deduper{claimed: true},
		&limiter{allowed: true},
	)
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"event_id":  "evt-cross",
		"channel":   "sms",
		"recipient": map[string]any{"user_id": int64(99)},
		"body":      "Hello!",
	})
	req := withServiceIdentity(httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader(body)), uid)
	h.SubmitNotification(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestSubmitNotification_OptedOut_403(t *testing.T) {
	uid := int64(42)
	h := buildSubmitHandler(
		&notifRepo{},
		&userRepo{setting: domain.Setting{UserID: uid, Channel: domain.ChannelSMS, OptIn: false}},
		&deduper{claimed: true},
		&limiter{allowed: true},
	)
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"event_id":  "evt-opt",
		"channel":   "sms",
		"recipient": map[string]any{"user_id": uid},
		"body":      "Hello!",
	})
	req := withServiceIdentity(httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader(body)), uid)
	h.SubmitNotification(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "opted_out")
}

func TestSubmitNotification_RateLimited_429(t *testing.T) {
	uid := int64(42)
	h := buildSubmitHandler(
		&notifRepo{},
		&userRepo{setting: domain.DefaultSetting(uid, domain.ChannelSMS)},
		&deduper{claimed: true},
		&limiter{allowed: false}, // rate limited
	)
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{
		"event_id":  "evt-rl",
		"channel":   "sms",
		"recipient": map[string]any{"user_id": uid},
		"body":      "Hello!",
	})
	req := withServiceIdentity(httptest.NewRequest(http.MethodPost, "/v1/notifications", bytes.NewReader(body)), uid)
	h.SubmitNotification(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3600", w.Header().Get("Retry-After"))
	assertErrorCode(t, w, "rate_limited")
}

// assertErrorCode decodes the response body and asserts the error code field.
func assertErrorCode(t *testing.T, w *httptest.ResponseRecorder, code string) {
	t.Helper()
	var body dto.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, code, body.Code)
}
