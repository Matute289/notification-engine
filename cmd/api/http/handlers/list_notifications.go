package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
)

// parseStatus validates a raw status string and returns the typed Status.
func parseStatus(s string) (domain.Status, error) {
	st := domain.Status(s)
	switch st {
	case domain.StatusReceived, domain.StatusEnqueued, domain.StatusInFlight,
		domain.StatusSent, domain.StatusFailed, domain.StatusRetrying, domain.StatusDeadLetter:
		return st, nil
	}
	return "", fmt.Errorf("%w: unknown status %q", domain.ErrInvalidInput, s)
}

// ListNotifications handles GET /v1/notifications.
// Requires service identity with X-On-Behalf-Of-User.
// Optional query params: limit, cursor, channel, status, since, until.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "service identity with X-On-Behalf-Of-User required")
		return
	}

	q := r.URL.Query()

	// -- limit --
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if v > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 100")
			return
		}
		limit = v
	}

	// -- cursor --
	cursor := q.Get("cursor")

	// Validate cursor is decodable base64 before hitting the repo.
	if cursor != "" {
		if _, err := base64.URLEncoding.DecodeString(cursor); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor encoding")
			return
		}
	}

	// -- channel --
	var channel *domain.Channel
	if raw := q.Get("channel"); raw != "" {
		ch, err := domain.ParseChannel(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
			return
		}
		channel = &ch
	}

	// -- status --
	var status *domain.Status
	if raw := q.Get("status"); raw != "" {
		st, err := parseStatus(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_status", err.Error())
			return
		}
		status = &st
	}

	// -- since --
	var since *time.Time
	if raw := q.Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_since", "since must be RFC3339")
			return
		}
		since = &t
	}

	// -- until --
	var until *time.Time
	if raw := q.Get("until"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_until", "until must be RFC3339")
			return
		}
		until = &t
	}

	items, nextCursor, err := h.ListNotificationsSvc.Execute(r.Context(), service.ListNotificationsInput{
		UserID:  ownerID,
		Limit:   limit,
		Cursor:  cursor,
		Channel: channel,
		Status:  status,
		Since:   since,
		Until:   until,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}

	views := make([]dto.NotificationView, len(items))
	for i := range items {
		views[i] = dto.ToView(&items[i])
	}
	writeJSON(w, http.StatusOK, dto.NotificationListResponse{
		Items:      views,
		NextCursor: nextCursor,
		Limit:      limit,
	})
}
