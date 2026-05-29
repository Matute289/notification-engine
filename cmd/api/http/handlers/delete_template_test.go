package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDeleteTemplate_HappyPath_204(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{
		Templates: &templateRepo{t: domain.Template{ID: id, OwnerUserID: 42}},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), 42),
		"id", id.String(),
	)
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteTemplate_InvalidID_400(t *testing.T) {
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/templates/bad", nil), "id", "bad")
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestDeleteTemplate_NoIdentity_401(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), "id", id.String())
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestDeleteTemplate_NotFound_404(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{
		Templates: &templateRepo{err: domain.ErrNotFound},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), 42),
		"id", id.String(),
	)
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}
