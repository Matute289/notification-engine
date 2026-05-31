package handlers

import (
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GetNotification handles GET /v1/notifications/{id}.
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
	if n.Recipient.UserID != nil {
		if err := mw.RequireUserOwnership(r.Context(), *n.Recipient.UserID); err != nil {
			mapDomainError(w, err)
			return
		}
	} else {
		// Notifications addressed to a raw email/phone/token (no UserID) are
		// accessible only to authenticated service callers — not JWT users.
		id, ok := mw.IdentityFromContext(r.Context())
		if !ok {
			mapDomainError(w, domain.ErrUnauthenticated)
			return
		}
		if id.Kind != "service" {
			mapDomainError(w, domain.ErrForbidden)
			return
		}
	}
	writeJSON(w, http.StatusOK, dto.ToView(n))
}
