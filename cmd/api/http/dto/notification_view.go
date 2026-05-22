package dto

import (
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

type NotificationView struct {
	ID         uuid.UUID         `json:"id"`
	EventID    string            `json:"event_id"`
	Channel    string            `json:"channel"`
	Status     string            `json:"status"`
	Attempt    int               `json:"attempt"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body,omitempty"`
	LastError  string            `json:"last_error,omitempty"`
	Recipient  Recipient         `json:"recipient"`
	Variables  map[string]string `json:"variables,omitempty"`
	TemplateID *uuid.UUID        `json:"template_id,omitempty"`
}

func ToView(n *domain.Notification) NotificationView {
	return NotificationView{
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
		Recipient: Recipient{
			UserID:      n.Recipient.UserID,
			Email:       string(n.Recipient.Email),
			PhoneNumber: string(n.Recipient.Phone),
			DeviceToken: string(n.Recipient.DeviceToken),
		},
	}
}
