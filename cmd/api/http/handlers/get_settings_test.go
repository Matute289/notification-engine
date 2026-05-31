package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSettings_InvalidID_400(t *testing.T) {
	h := &Handler{ListSettingsSvc: &service.ListSettings{Users: &userRepo{}}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/users/bad/settings", nil), "id", "bad")
	h.GetSettings(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestGetSettings_NoIdentity_401(t *testing.T) {
	h := &Handler{ListSettingsSvc: &service.ListSettings{Users: &userRepo{}}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/users/42/settings", nil), "id", "42")
	h.GetSettings(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestGetSettings_CrossUser_403(t *testing.T) {
	h := &Handler{ListSettingsSvc: &service.ListSettings{Users: &userRepo{}}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/users/42/settings", nil), 99),
		"id", "42",
	)
	h.GetSettings(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestGetSettings_HappyPath_AllDefaults_200(t *testing.T) {
	// No explicit settings → 8 channels all opt_in=true.
	h := &Handler{ListSettingsSvc: &service.ListSettings{
		Users: &userRepo{settings: nil},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/users/42/settings", nil), 42),
		"id", "42",
	)
	h.GetSettings(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var views []dto.SettingView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&views))
	assert.Len(t, views, len(domain.AllChannels()))
	for _, v := range views {
		assert.True(t, v.OptIn)
		assert.Nil(t, v.UpdatedAt)
	}
}

func TestGetSettings_HappyPath_WithExplicitRow_200(t *testing.T) {
	// sms opted-out explicitly.
	ts := time.Date(2025, 3, 10, 14, 0, 0, 0, time.UTC)
	explicit := domain.Setting{UserID: 42, Channel: domain.ChannelSMS, OptIn: false, UpdatedAt: ts}
	h := &Handler{ListSettingsSvc: &service.ListSettings{
		Users: &userRepo{settings: []domain.Setting{explicit}},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/users/42/settings", nil), 42),
		"id", "42",
	)
	h.GetSettings(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var views []dto.SettingView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&views))
	assert.Len(t, views, len(domain.AllChannels()))

	byChannel := make(map[string]dto.SettingView)
	for _, v := range views {
		byChannel[v.Channel] = v
	}
	assert.False(t, byChannel["sms"].OptIn)
	require.NotNil(t, byChannel["sms"].UpdatedAt)
	assert.Equal(t, ts.UTC(), byChannel["sms"].UpdatedAt.UTC())
	assert.True(t, byChannel["email"].OptIn)
	assert.Nil(t, byChannel["email"].UpdatedAt)
}
