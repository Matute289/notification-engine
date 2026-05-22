package port

import (
	"context"

	"github.com/google/uuid"
)

// TemplateRenderer renders a stored template against caller-supplied vars and
// returns the resolved subject + body.
type TemplateRenderer interface {
	Render(ctx context.Context, templateID uuid.UUID, vars map[string]string) (subject, body string, err error)
}
