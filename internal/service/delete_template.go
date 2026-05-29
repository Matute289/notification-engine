package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
)

// DeleteTemplate hard-deletes a template the caller owns.
type DeleteTemplate struct {
	Templates port.TemplateRepository
}

type DeleteTemplateInput struct {
	ID          uuid.UUID
	OwnerUserID int64
}

func (u *DeleteTemplate) Execute(ctx context.Context, in DeleteTemplateInput) error {
	t, err := u.Templates.Get(ctx, in.ID)
	if err != nil {
		return err
	}
	if t.OwnerUserID != in.OwnerUserID {
		return fmt.Errorf("%w: template belongs to a different owner", domain.ErrForbidden)
	}
	return u.Templates.Delete(ctx, in.ID)
}
