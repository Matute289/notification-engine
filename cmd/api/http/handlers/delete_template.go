package handlers

import (
	"net/http"

	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DeleteTemplate handles DELETE /v1/templates/{id}.
func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if err := h.DeleteTemplateSvc.Execute(r.Context(), service.DeleteTemplateInput{
		ID: id, OwnerUserID: ownerID,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
