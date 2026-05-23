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

// RegisterDevice handles POST /v1/users/{id}/devices.
func (h *Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	var req dto.DeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
		return
	}
	if err := h.RegisterDeviceSvc.Execute(r.Context(), service.RegisterDeviceInput{
		UserID: uid, Channel: channel, DeviceToken: domain.DeviceToken(req.DeviceToken),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
