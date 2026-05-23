package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestRegisterDevice_InvalidID_400(t *testing.T) {
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/bad/devices", nil), "id", "bad")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestRegisterDevice_InvalidJSON_400(t *testing.T) {
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/42/devices", bytes.NewBufferString(`{bad}`)), "id", "42")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestRegisterDevice_InvalidChannel_400(t *testing.T) {
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{}}
	w := httptest.NewRecorder()
	body := `{"device_token":"tok","channel":"fax"}`
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/42/devices", bytes.NewBufferString(body)), "id", "42")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestRegisterDevice_NonPushChannel_400(t *testing.T) {
	// "email" is a valid channel but not a push channel; service rejects it.
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{
		Users: &userRepo{},
		Clock: fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"device_token":"tok","channel":"email"}`
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/42/devices", bytes.NewBufferString(body)), "id", "42")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_request")
}

func TestRegisterDevice_HappyPath_204(t *testing.T) {
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{
		Users: &userRepo{},
		Clock: fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"device_token":"device-token-abc","channel":"push_ios"}`
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/42/devices", bytes.NewBufferString(body)), "id", "42")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestRegisterDevice_EmptyToken_400(t *testing.T) {
	// Empty device_token → service returns ErrInvalidInput.
	h := &Handler{RegisterDeviceSvc: &service.RegisterDevice{
		Users: &userRepo{},
		Clock: fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"device_token":"","channel":"push_ios"}`
	r := withURLParam(httptest.NewRequest(http.MethodPost, "/v1/users/42/devices", bytes.NewBufferString(body)), "id", "42")
	h.RegisterDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_request")
}
