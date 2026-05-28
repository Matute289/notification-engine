package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TemplateRepository struct{ pool *pgxpool.Pool }

func NewTemplateRepository(pool *pgxpool.Pool) *TemplateRepository {
	return &TemplateRepository{pool: pool}
}

var _ port.TemplateRepository = (*TemplateRepository)(nil)

func (r *TemplateRepository) Create(ctx context.Context, t domain.Template) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notification_templates (id, name, channel, locale, subject, body, version, owner_user_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.Name, string(t.Channel), t.Locale, t.Subject, t.Body, t.Version, t.OwnerUserID)
	if err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) Get(ctx context.Context, id uuid.UUID) (domain.Template, error) {
	var (
		t       domain.Template
		channel string
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, channel, locale, subject, body, version, owner_user_id, created_at, updated_at
		   FROM notification_templates WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &channel, &t.Locale, &t.Subject, &t.Body, &t.Version, &t.OwnerUserID, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("get template: %w", err)
	}
	t.Channel = domain.Channel(channel)
	return t, nil
}
