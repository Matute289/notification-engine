package dto

import "github.com/google/uuid"

type SubmitRequest struct {
	EventID    string            `json:"event_id"`
	Channel    string            `json:"channel"`
	Recipient  Recipient         `json:"recipient"`
	TemplateID *uuid.UUID        `json:"template_id,omitempty"`
	Variables  map[string]string `json:"variables,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	Body       string            `json:"body,omitempty"`
}
