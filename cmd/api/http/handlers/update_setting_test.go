package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSetting_InvalidID_400(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/bad/settings", nil), "id", "bad")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestUpdateSetting_InvalidJSON_400(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(`{bad}`)), "id", "42")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestUpdateSetting_InvalidChannel_400(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	body := `{"channel":"fax","opt_in":true}`
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(body)), "id", "42")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestUpdateSetting_HappyPath_204(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{
		Users: &userRepo{},
		Clock: fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"channel":"sms","opt_in":true}`
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(body)), "id", "42")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestUpdateSetting_OptOut_204(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{
		Users: &userRepo{},
		Clock: fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"channel":"email","opt_in":false}`
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/7/settings", bytes.NewBufferString(body)), "id", "7")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}
