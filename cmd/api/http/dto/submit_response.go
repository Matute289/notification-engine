package dto

import (
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

type SubmitResponse struct {
	NotificationID uuid.UUID     `json:"notification_id"`
	Status         domain.Status `json:"status"`
	Duplicate      bool          `json:"duplicate,omitempty"`
}
