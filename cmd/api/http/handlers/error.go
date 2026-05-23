package handlers

import (
	"errors"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
)

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

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, dto.ErrorBody{Code: code, Message: msg})
}
