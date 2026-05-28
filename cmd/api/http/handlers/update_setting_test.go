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

func TestUpdateSetting_NoIdentity_401(t *testing.T) {
	// No identity in context → RequireUserOwnership returns ErrUnauthenticated → 401.
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", nil), "id", "42")
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestUpdateSetting_CrossUser_403(t *testing.T) {
	// Identity is for user 99 but path user is 42 → ErrForbidden → 403.
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", nil), 99),
		"id", "42",
	)
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "forbidden")
}

func TestUpdateSetting_InvalidJSON_400(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(`{bad}`)), 42),
		"id", "42",
	)
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestUpdateSetting_InvalidChannel_400(t *testing.T) {
	h := &Handler{UpdateSettingSvc: &service.UpdateSetting{}}
	w := httptest.NewRecorder()
	body := `{"channel":"fax","opt_in":true}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
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
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/users/42/settings", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
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
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/users/7/settings", bytes.NewBufferString(body)), 7),
		"id", "7",
	)
	h.UpdateSetting(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}
