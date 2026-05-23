package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTemplate_HappyPath_201(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{
		Templates: &templateRepo{},
		Clock:     fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello {{.Name}}!","version":1}`
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusCreated, w.Code)
	var t2 domain.Template
	require.NoError(t, json.NewDecoder(w.Body).Decode(&t2))
	assert.Equal(t, "welcome", t2.Name)
	assert.Equal(t, domain.ChannelSMS, t2.Channel)
}

func TestCreateTemplate_InvalidJSON_400(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(`{bad}`))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestCreateTemplate_InvalidChannel_400(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"fax","body":"Hello!","version":1}`
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestCreateTemplate_AlreadyExists_409(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{
		Templates: &templateRepo{err: domain.ErrAlreadyExists},
		Clock:     fixedClock{},
	}}
	w := httptest.NewRecorder()
	body := `{"name":"welcome","channel":"sms","body":"Hello!","version":1}`
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusConflict, w.Code)
	assertErrorCode(t, w, "conflict")
}

func TestCreateTemplate_MissingBody_400(t *testing.T) {
	h := &Handler{CreateTemplateSvc: &service.CreateTemplate{
		Templates: &templateRepo{},
		Clock:     fixedClock{},
	}}
	w := httptest.NewRecorder()
	// body field is empty → domain.NewTemplate returns ErrInvalidInput
	body := `{"name":"welcome","channel":"sms","body":"","version":1}`
	r := httptest.NewRequest(http.MethodPost, "/v1/templates", bytes.NewBufferString(body))
	h.CreateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_request")
}
