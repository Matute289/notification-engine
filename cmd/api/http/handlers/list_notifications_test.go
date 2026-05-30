package handlers

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListNotifications_NoIdentity_403(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/notifications", nil)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestListNotifications_MissingOnBehalfOf_403(t *testing.T) {
	// Identity present but no OnBehalfOfUserID → forbidden.
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications", nil), 0) // 0 = nil OnBehalfOfUserID
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestListNotifications_InvalidLimit_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=abc", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_limit")
}

func TestListNotifications_LimitZeroOrNegative_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=0", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_limit")
}

func TestListNotifications_InvalidChannel_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?channel=fax", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestListNotifications_InvalidCursor_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?cursor=!!!notbase64", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_cursor")
}

func TestListNotifications_HappyPath_EmptyList_200(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{
		Notifications: &notifRepo{listResult: nil, listNextCursor: ""},
	}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp dto.NotificationListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.Items)
	assert.Equal(t, "", resp.NextCursor)
}

func TestListNotifications_HappyPath_WithItems_200(t *testing.T) {
	uid := int64(42)
	n := domain.Notification{
		ID: uuid.New(), EventID: "e1", Channel: domain.ChannelEmail,
		Status: domain.StatusSent, Recipient: domain.Recipient{UserID: &uid},
	}
	cursor := base64.URLEncoding.EncodeToString([]byte("2025-01-01T00:00:00Z," + n.ID.String()))
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{
		Notifications: &notifRepo{listResult: []domain.Notification{n}, listNextCursor: cursor},
	}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?limit=10", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp dto.NotificationListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Items, 1)
	assert.Equal(t, n.ID, resp.Items[0].ID)
	assert.Equal(t, cursor, resp.NextCursor)
	assert.Equal(t, 10, resp.Limit)
}

func TestListNotifications_InvalidSince_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?since=not-a-date", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_since")
}

func TestListNotifications_InvalidStatus_400(t *testing.T) {
	h := &Handler{ListNotificationsSvc: &service.ListNotifications{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/notifications?status=flying", nil), 42)
	h.ListNotifications(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_status")
}
