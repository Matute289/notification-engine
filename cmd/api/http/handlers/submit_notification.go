package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
)

// SubmitNotification handles POST /v1/notifications.
func (h *Handler) SubmitNotification(w http.ResponseWriter, r *http.Request) {
	var req dto.SubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
		return
	}
	eventID, err := domain.ParseEventID(req.EventID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_event_id", err.Error())
		return
	}
	email, err := domain.ParseEmail(req.Recipient.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_email", err.Error())
		return
	}
	phone, err := domain.ParsePhone(req.Recipient.PhoneNumber)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_phone", err.Error())
		return
	}

	if req.Recipient.UserID != nil {
		if err := mw.RequireUserOwnership(r.Context(), *req.Recipient.UserID); err != nil {
			mapDomainError(w, err)
			return
		}
	}

	in := service.SubmitInput{
		EventID:    eventID,
		Channel:    channel,
		TemplateID: req.TemplateID,
		Variables:  req.Variables,
		Subject:    req.Subject,
		Body:       req.Body,
		Recipient: domain.Recipient{
			UserID:      req.Recipient.UserID,
			Email:       email,
			Phone:       phone,
			DeviceToken: domain.DeviceToken(req.Recipient.DeviceToken),
		},
	}

	out, err := h.SubmitSvc.Execute(r.Context(), in)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	resp := dto.SubmitResponse{
		NotificationID: out.Notification.ID,
		Status:         out.Notification.Status,
		Duplicate:      out.Duplicate,
	}
	if out.Duplicate {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusAccepted, resp)
}
