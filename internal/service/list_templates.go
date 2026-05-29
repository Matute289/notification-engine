package service

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// ListTemplates returns all templates owned by a user, optionally filtered by channel.
type ListTemplates struct {
	Templates port.TemplateRepository
}

type ListTemplatesInput struct {
	OwnerUserID int64
	Channel     *domain.Channel
}

func (u *ListTemplates) Execute(ctx context.Context, in ListTemplatesInput) ([]domain.Template, error) {
	return u.Templates.List(ctx, in.OwnerUserID, in.Channel)
}
