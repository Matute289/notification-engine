package service

import (
	"context"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// CreateTemplate persists a new template version. Validation lives on the
// domain constructor so the use case stays thin.
type CreateTemplate struct {
	Templates port.TemplateRepository
	Clock     port.Clock
}

type CreateTemplateInput struct {
	Name        string
	Channel     domain.Channel
	Locale      string
	Subject     string
	Body        string
	MediaURLs   []string
	Version     int
	OwnerUserID int64
}

func (u *CreateTemplate) Execute(ctx context.Context, in CreateTemplateInput) (domain.Template, error) {
	t, err := domain.NewTemplate(uuid.New(), in.Name, in.Channel, in.Locale, in.Subject, in.Body, in.MediaURLs, in.Version, in.OwnerUserID, u.Clock.Now())
	if err != nil {
		return domain.Template{}, err
	}
	if err := u.Templates.Create(ctx, t); err != nil {
		return domain.Template{}, err
	}
	return t, nil
}

// GetTemplate reads one template by id.
type GetTemplate struct {
	Templates port.TemplateRepository
}

func (u *GetTemplate) Execute(ctx context.Context, id uuid.UUID) (domain.Template, error) {
	return u.Templates.Get(ctx, id)
}
