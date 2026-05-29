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

func (r *TemplateRepository) Update(ctx context.Context, t domain.Template) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notification_templates SET name=$1, subject=$2, body=$3, updated_at=NOW() WHERE id=$4`,
		t.Name, t.Subject, t.Body, t.ID)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notification_templates WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if channel != nil {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, channel, locale, subject, body, version, owner_user_id, created_at, updated_at
			   FROM notification_templates WHERE owner_user_id=$1 AND channel=$2 ORDER BY name`,
			ownerUserID, string(*channel))
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, channel, locale, subject, body, version, owner_user_id, created_at, updated_at
			   FROM notification_templates WHERE owner_user_id=$1 ORDER BY channel, name`,
			ownerUserID)
	}
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()
	var out []domain.Template
	for rows.Next() {
		var (
			t  domain.Template
			ch string
		)
		if err := rows.Scan(&t.ID, &t.Name, &ch, &t.Locale, &t.Subject, &t.Body,
			&t.Version, &t.OwnerUserID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list templates scan: %w", err)
		}
		t.Channel = domain.Channel(ch)
		out = append(out, t)
	}
	return out, rows.Err()
}
