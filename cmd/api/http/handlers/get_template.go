package handlers

import (
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GetTemplate handles GET /v1/templates/{id}.
func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	t, err := h.GetTemplateSvc.Execute(r.Context(), id)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.TemplateView{
		ID:          t.ID,
		Name:        t.Name,
		Channel:     string(t.Channel),
		Locale:      t.Locale,
		Subject:     t.Subject,
		Body:        t.Body,
		MediaURLs:   t.MediaURLs,
		Version:     t.Version,
		OwnerUserID: t.OwnerUserID,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	})
}
