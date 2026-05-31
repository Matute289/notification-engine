package middleware

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
)

// RequireUserOwnership returns an error if the authenticated identity is not
// authorized to act on behalf of pathUserID.
//
// For service identities (HMAC), the X-On-Behalf-Of-User header must be present
// and must match pathUserID exactly.
// For user identities (Clerk JWT), the Subject↔internal-user-ID mapping is not
// yet implemented, so authenticated JWT users are allowed through. Replace the
// "user" case below once the mapping is available.
func RequireUserOwnership(ctx context.Context, pathUserID int64) error {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return domain.ErrUnauthenticated
	}
	switch id.Kind {
	case "service":
		if id.OnBehalfOfUserID == nil || *id.OnBehalfOfUserID != pathUserID {
			return domain.ErrForbidden
		}
		return nil
	case "user":
		// JWT user → internal user ID mapping is not yet implemented.
		// Return ErrForbidden to prevent any JWT-authenticated caller from
		// accessing another user's resources. Use HMAC with X-On-Behalf-Of-User
		// header for service-to-service access on behalf of a specific user.
		return domain.ErrForbidden
	default:
		return domain.ErrForbidden
	}
}

// RequireServiceIdentity verifies the caller is an authenticated service with
// an X-On-Behalf-Of-User header, and returns that user ID. Use this for
// endpoints where the owner is taken from the header rather than a URL param.
func RequireServiceIdentity(ctx context.Context) (int64, error) {
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return 0, domain.ErrUnauthenticated
	}
	if id.Kind != "service" || id.OnBehalfOfUserID == nil {
		return 0, domain.ErrForbidden
	}
	return *id.OnBehalfOfUserID, nil
}
