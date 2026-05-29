package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UpdateTemplate handles PUT /v1/templates/{id}.
func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
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
	var req dto.UpdateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	t, err := h.UpdateTemplateSvc.Execute(r.Context(), service.UpdateTemplateInput{
		ID: id, Name: req.Name, Subject: req.Subject, Body: req.Body,
		MediaURLs: req.MediaURLs, OwnerUserID: ownerID,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateToView(t))
}
