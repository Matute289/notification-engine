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

func TestListTemplates_HappyPath_200(t *testing.T) {
	tpl := domain.Template{ID: uuid.New(), Name: "welcome", Channel: domain.ChannelSMS, Body: "Body", OwnerUserID: 42}
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{t: tpl}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/templates", nil), 42)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string][]dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Contains(t, got, "sms")
	assert.Len(t, got["sms"], 1)
	assert.Equal(t, "welcome", got["sms"][0].Name)
}

func TestListTemplates_FilterByChannel_200(t *testing.T) {
	tpl := domain.Template{ID: uuid.New(), Name: "welcome", Channel: domain.ChannelSMS, Body: "Body", OwnerUserID: 42}
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{t: tpl}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodGet, "/v1/templates?channel=sms", nil), 42,
	)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListTemplates_InvalidChannel_400(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodGet, "/v1/templates?channel=fax", nil), 42,
	)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestListTemplates_NoIdentity_401(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestListTemplates_Empty_200(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/templates", nil), 42)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string][]dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Empty(t, got)
}
