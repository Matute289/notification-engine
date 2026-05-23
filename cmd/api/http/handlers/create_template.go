package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
)

// CreateTemplate handles POST /v1/templates.
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req dto.TemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
		return
	}
	t, err := h.CreateTemplateSvc.Execute(r.Context(), service.CreateTemplateInput{
		Name: req.Name, Channel: channel, Locale: req.Locale,
		Subject: req.Subject, Body: req.Body, MediaURLs: req.MediaURLs, Version: req.Version,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}
