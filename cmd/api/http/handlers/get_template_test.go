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

func TestGetTemplate_InvalidID_400(t *testing.T) {
	h := &Handler{GetTemplateSvc: &service.GetTemplate{Templates: &templateRepo{}}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/templates/bad-id", nil), "id", "bad-id")
	h.GetTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestGetTemplate_NotFound_404(t *testing.T) {
	h := &Handler{GetTemplateSvc: &service.GetTemplate{
		Templates: &templateRepo{err: domain.ErrNotFound},
	}}
	id := uuid.New()
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/templates/"+id.String(), nil), "id", id.String())
	h.GetTemplate(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}

func TestGetTemplate_HappyPath_200(t *testing.T) {
	id := uuid.New()
	tpl := domain.Template{ID: id, Name: "welcome", Channel: domain.ChannelSMS, Body: "Hello!", OwnerUserID: 42}
	h := &Handler{GetTemplateSvc: &service.GetTemplate{
		Templates: &templateRepo{t: tpl},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/templates/"+id.String(), nil), "id", id.String())
	h.GetTemplate(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "welcome", got.Name)
	assert.Equal(t, int64(42), got.OwnerUserID)
}
