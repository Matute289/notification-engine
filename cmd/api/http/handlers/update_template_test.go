package handlers

import (
	"bytes"
	"encoding/json"
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

func TestUpdateTemplate_HappyPath_200(t *testing.T) {
	id := uuid.New()
	expected := domain.Template{ID: id, Name: "new name", Channel: domain.ChannelSMS, Body: "New Body", OwnerUserID: 42, UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{
		Templates: &templateRepo{t: expected},
		Clock:     fixedClock{},
	}}
	body := `{"name":"new name","body":"New Body"}`
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(body)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "new name", got.Name)
}

func TestUpdateTemplate_InvalidID_400(t *testing.T) {
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/templates/bad", bytes.NewBufferString(`{}`)), "id", "bad")
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestUpdateTemplate_NoIdentity_401(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(`{}`)), "id", id.String())
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestUpdateTemplate_NotFound_404(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{
		Templates: &templateRepo{err: domain.ErrNotFound},
		Clock:     fixedClock{},
	}}
	body := `{"name":"n","body":"b"}`
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(body)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}

func TestUpdateTemplate_InvalidJSON_400(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(`{bad}`)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}
