package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/go-chi/chi/v5"
)

// UpdateSetting handles PUT /v1/users/{id}/settings.
func (h *Handler) UpdateSetting(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req dto.SettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
		return
	}
	if err := h.UpdateSettingSvc.Execute(r.Context(), service.UpdateSettingInput{
		UserID: uid, Channel: channel, OptIn: req.OptIn,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
