package handlers

import (
	"net/http"
	"strconv"

	"github.com/example/notification-engine/cmd/api/http/dto"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
)

// GetSettings handles GET /v1/users/{id}/settings.
// Returns all 8 channel preferences; channels with no explicit setting
// show opt_in=true and updated_at=null (implicit default).
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := mw.RequireUserOwnership(r.Context(), uid); err != nil {
		mapDomainError(w, err)
		return
	}
	settings, err := h.ListSettingsSvc.Execute(r.Context(), uid)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	views := make([]dto.SettingView, len(settings))
	for i, s := range settings {
		v := dto.SettingView{
			Channel: string(s.Channel),
			OptIn:   s.OptIn,
		}
		if !s.UpdatedAt.IsZero() {
			t := s.UpdatedAt
			v.UpdatedAt = &t
		}
		views[i] = v
	}
	writeJSON(w, http.StatusOK, views)
}
