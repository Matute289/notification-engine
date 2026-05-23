// Package handlers is the HTTP inbound adapter. Handlers translate HTTP into
// service input and back; they never perform business logic themselves.
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler holds references to every service the HTTP surface exposes.
// Fields use the Svc suffix to avoid clashing with the identically-named handler methods.
type Handler struct {
	SubmitSvc         *service.SubmitNotification
	GetSvc            *service.GetNotification
	CreateTemplateSvc *service.CreateTemplate
	GetTemplateSvc    *service.GetTemplate
	UpdateSettingSvc  *service.UpdateSetting
	RegisterDeviceSvc *service.RegisterDevice
}

// --- POST /v1/notifications ---

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

// --- GET /v1/notifications/{id} ---

func (h *Handler) GetNotification(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	n, err := h.GetSvc.Execute(r.Context(), id)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ToView(n))
}

// --- POST /v1/templates ---

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

// --- GET /v1/templates/{id} ---

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
	writeJSON(w, http.StatusOK, t)
}

// --- PUT /v1/users/{id}/settings ---

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

// --- POST /v1/users/{id}/devices ---

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

// --- error mapping ---

// mapDomainError translates sentinel domain errors into HTTP status codes.
// Non-sentinel errors fall through to 500.
func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput),
		errors.Is(err, domain.ErrInvalidStatusTransition):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrOptedOut):
		writeError(w, http.StatusForbidden, "opted_out", err.Error())
	case errors.Is(err, domain.ErrRateLimited):
		w.Header().Set("Retry-After", "3600")
		writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
	case errors.Is(err, domain.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, dto.ErrorBody{Code: code, Message: msg})
}
