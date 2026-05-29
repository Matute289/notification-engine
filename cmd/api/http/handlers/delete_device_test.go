package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestDeleteDevice_HappyPath_204(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{Users: &userRepo{}}}
	w := httptest.NewRecorder()
	body := `{"channel":"push_ios","device_token":"tok"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteDevice_InvalidID_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/users/bad/devices", nil), "id", "bad")
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestDeleteDevice_NoIdentity_401(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", nil), "id", "42")
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestDeleteDevice_InvalidJSON_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(`{bad}`)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestDeleteDevice_InvalidChannel_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	body := `{"channel":"fax","device_token":"tok"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestDeleteDevice_NotFound_404(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{Users: &userRepo{err: domain.ErrNotFound}}}
	w := httptest.NewRecorder()
	body := `{"channel":"push_ios","device_token":"unknown"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}
