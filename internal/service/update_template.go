package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
)

// UpdateTemplate updates the mutable fields of an existing template in-place.
type UpdateTemplate struct {
	Templates port.TemplateRepository
	Clock     port.Clock
}

type UpdateTemplateInput struct {
	ID          uuid.UUID
	Name        string
	Subject     string
	Body        string
	MediaURLs   []string
	OwnerUserID int64
}

func (u *UpdateTemplate) Execute(ctx context.Context, in UpdateTemplateInput) (domain.Template, error) {
	t, err := u.Templates.Get(ctx, in.ID)
	if err != nil {
		return domain.Template{}, err
	}
	if t.OwnerUserID != in.OwnerUserID {
		return domain.Template{}, fmt.Errorf("%w: template belongs to a different owner", domain.ErrForbidden)
	}
	updated, err := t.UpdateFields(in.Name, in.Subject, in.Body, in.MediaURLs, u.Clock.Now())
	if err != nil {
		return domain.Template{}, err
	}
	if err := u.Templates.Update(ctx, updated); err != nil {
		return domain.Template{}, err
	}
	return updated, nil
}
