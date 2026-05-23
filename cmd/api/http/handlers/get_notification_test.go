package handlers

import (
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

func TestGetNotification_InvalidID_400(t *testing.T) {
	h := &Handler{GetSvc: &service.GetNotification{Notifications: &notifRepo{}}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/notifications/bad-id", nil), "id", "bad-id")
	h.GetNotification(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestGetNotification_NotFound_404(t *testing.T) {
	h := &Handler{GetSvc: &service.GetNotification{
		Notifications: &notifRepo{getErr: domain.ErrNotFound},
	}}
	id := uuid.New()
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/notifications/"+id.String(), nil), "id", id.String())
	h.GetNotification(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}

func TestGetNotification_HappyPath_200(t *testing.T) {
	id := uuid.New()
	n := &domain.Notification{
		ID:      id,
		EventID: "evt-1",
		Channel: domain.ChannelSMS,
		Status:  domain.StatusSent,
	}
	h := &Handler{GetSvc: &service.GetNotification{
		Notifications: &notifRepo{getResult: n},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/notifications/"+id.String(), nil), "id", id.String())
	h.GetNotification(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var view dto.NotificationView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&view))
	assert.Equal(t, id, view.ID)
	assert.Equal(t, "evt-1", view.EventID)
	assert.Equal(t, "sms", view.Channel)
}
