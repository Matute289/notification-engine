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
// For user identities (Clerk JWT), ownership verification is not yet implemented.
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
	default:
		// Clerk JWT: user ↔ User.ID mapping not implemented yet.
		return domain.ErrForbidden
	}
}
