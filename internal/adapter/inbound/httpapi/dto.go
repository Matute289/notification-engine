package httpapi

import (
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// Inbound DTOs — kept separate from domain so HTTP-layer concerns (json tags,
// optional vs required, version-specific shape) don't leak into the core.

type submitRequest struct {
	EventID    string            `json:"event_id"`
	Channel    string            `json:"channel"`
	Recipient  recipientDTO      `json:"recipient"`
	TemplateID *uuid.UUID        `json:"template_id,omitempty"`
	Variables  map[string]string `json:"variables,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body,omitempty"`
}

type recipientDTO struct {
	UserID      *int64 `json:"user_id,omitempty"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	DeviceToken string `json:"device_token,omitempty"`
}

type submitResponse struct {
	NotificationID uuid.UUID     `json:"notification_id"`
	Status         domain.Status `json:"status"`
	Duplicate      bool          `json:"duplicate,omitempty"`
}

type templateRequest struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Version int    `json:"version"`
}

type settingRequest struct {
	Channel string `json:"channel"`
	OptIn   bool   `json:"opt_in"`
}

type deviceRequest struct {
	DeviceToken string `json:"device_token"`
	Channel     string `json:"channel"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type notificationView struct {
	ID         uuid.UUID         `json:"id"`
	EventID    string            `json:"event_id"`
	Channel    string            `json:"channel"`
	Status     string            `json:"status"`
	Attempt    int               `json:"attempt"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body,omitempty"`
	LastError  string            `json:"last_error,omitempty"`
	Recipient  recipientDTO      `json:"recipient"`
	Variables  map[string]string `json:"variables,omitempty"`
	TemplateID *uuid.UUID        `json:"template_id,omitempty"`
}

func toView(n *domain.Notification) notificationView {
	v := notificationView{
		ID:         n.ID,
		EventID:    string(n.EventID),
		Channel:    string(n.Channel),
		Status:     string(n.Status),
		Attempt:    n.Attempt,
		Subject:    n.Subject,
		Body:       n.Body,
		LastError:  n.LastError,
		Variables:  n.Variables,
		TemplateID: n.TemplateID,
		Recipient: recipientDTO{
			UserID:      n.Recipient.UserID,
			Email:       string(n.Recipient.Email),
			PhoneNumber: string(n.Recipient.Phone),
			DeviceToken: string(n.Recipient.DeviceToken),
		},
	}
	return v
}
