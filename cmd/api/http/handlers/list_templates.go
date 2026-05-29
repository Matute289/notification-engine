package handlers

import (
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
)

// ListTemplates handles GET /v1/templates.
// Optional query parameter: channel=<channel>
// Response: map[channel][]TemplateView
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	var channel *domain.Channel
	if raw := r.URL.Query().Get("channel"); raw != "" {
		ch, err := domain.ParseChannel(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
			return
		}
		channel = &ch
	}
	templates, err := h.ListTemplatesSvc.Execute(r.Context(), service.ListTemplatesInput{
		OwnerUserID: ownerID, Channel: channel,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	grouped := make(map[string][]dto.TemplateView)
	for _, t := range templates {
		grouped[string(t.Channel)] = append(grouped[string(t.Channel)], templateToView(t))
	}
	writeJSON(w, http.StatusOK, grouped)
}
