package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDomainError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantCode    string
		wantRetryAfter string
	}{
		{"not found",                domain.ErrNotFound,                http.StatusNotFound,          "not_found",       ""},
		{"invalid input",            domain.ErrInvalidInput,            http.StatusBadRequest,        "invalid_request", ""},
		{"invalid transition",       domain.ErrInvalidStatusTransition, http.StatusBadRequest,        "invalid_request", ""},
		{"forbidden",                domain.ErrForbidden,               http.StatusForbidden,         "forbidden",       ""},
		{"opted out",                domain.ErrOptedOut,                http.StatusForbidden,         "opted_out",       ""},
		{"rate limited",             domain.ErrRateLimited,             http.StatusTooManyRequests,   "rate_limited",    "3600"},
		{"unauthenticated",          domain.ErrUnauthenticated,         http.StatusUnauthorized,      "unauthorized",    ""},
		{"already exists",           domain.ErrAlreadyExists,           http.StatusConflict,          "conflict",        ""},
		{"wrapped not found",        fmt.Errorf("svc: %w", domain.ErrNotFound), http.StatusNotFound, "not_found",       ""},
		{"wrapped invalid input",    fmt.Errorf("svc: %w", domain.ErrInvalidInput), http.StatusBadRequest, "invalid_request", ""},
		{"unknown",                  errors.New("boom"),                http.StatusInternalServerError, "internal_error", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			mapDomainError(w, tc.err)
			assert.Equal(t, tc.wantStatus, w.Code)
			if tc.wantRetryAfter != "" {
				assert.Equal(t, tc.wantRetryAfter, w.Header().Get("Retry-After"))
			}
			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
			assert.Equal(t, tc.wantCode, body.Code)
		})
	}
}

func TestWriteError_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "bad", "something went wrong")
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
