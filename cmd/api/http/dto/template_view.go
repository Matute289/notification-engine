package dto

import (
	"time"

	"github.com/google/uuid"
)

// TemplateView is the JSON response body for template endpoints.
type TemplateView struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Channel     string    `json:"channel"`
	Locale      string    `json:"locale"`
	Subject     string    `json:"subject,omitempty"`
	Body        string    `json:"body"`
	MediaURLs   []string  `json:"media_urls,omitempty"`
	Version     int       `json:"version"`
	OwnerUserID int64     `json:"owner_user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
