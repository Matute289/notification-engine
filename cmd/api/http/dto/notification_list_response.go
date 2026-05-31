package dto

// NotificationListResponse is the JSON envelope for GET /v1/notifications.
type NotificationListResponse struct {
	Items      []NotificationView `json:"items"`
	NextCursor string             `json:"next_cursor"`
	Limit      int                `json:"limit"`
}
