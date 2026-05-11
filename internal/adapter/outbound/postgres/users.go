package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct{ pool *pgxpool.Pool }

func NewUserRepository(pool *pgxpool.Pool) *UserRepository { return &UserRepository{pool: pool} }

var _ port.UserRepository = (*UserRepository)(nil)

func (r *UserRepository) GetUser(ctx context.Context, id int64) (domain.User, error) {
	var (
		u     domain.User
		email string
	)
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, country_code, phone_number, created_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &email, &u.CountryCode, &u.PhoneNumber, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, domain.ErrNotFound
	}
	if err != nil {
		return u, fmt.Errorf("get user: %w", err)
	}
	u.Email = domain.Email(email)
	return u, nil
}

func (r *UserRepository) DevicesForUser(ctx context.Context, userID int64, channel domain.Channel) ([]domain.Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, device_token, channel, last_logged_in_at
		   FROM devices WHERE user_id = $1 AND channel = $2
		   ORDER BY last_logged_in_at DESC`, userID, string(channel))
	if err != nil {
		return nil, fmt.Errorf("devices query: %w", err)
	}
	defer rows.Close()
	var out []domain.Device
	for rows.Next() {
		var (
			d     domain.Device
			token string
			ch    string
		)
		if err := rows.Scan(&d.ID, &d.UserID, &token, &ch, &d.LastLoggedInAt); err != nil {
			return nil, fmt.Errorf("devices scan: %w", err)
		}
		d.DeviceToken = domain.DeviceToken(token)
		d.Channel = domain.Channel(ch)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *UserRepository) UpsertDevice(ctx context.Context, d domain.Device) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO devices (user_id, device_token, channel, last_logged_in_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (channel, device_token) DO UPDATE
		   SET user_id = EXCLUDED.user_id, last_logged_in_at = NOW()`,
		d.UserID, string(d.DeviceToken), string(d.Channel))
	if err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}
	return nil
}

func (r *UserRepository) GetSetting(ctx context.Context, userID int64, channel domain.Channel) (domain.Setting, error) {
	var (
		s  domain.Setting
		ch string
	)
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, channel, opt_in, updated_at FROM notification_settings
		   WHERE user_id = $1 AND channel = $2`, userID, string(channel),
	).Scan(&s.UserID, &ch, &s.OptIn, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DefaultSetting(userID, channel), nil
	}
	if err != nil {
		return s, fmt.Errorf("get setting: %w", err)
	}
	s.Channel = domain.Channel(ch)
	return s, nil
}

func (r *UserRepository) UpsertSetting(ctx context.Context, s domain.Setting) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO notification_settings (user_id, channel, opt_in, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id, channel) DO UPDATE
		   SET opt_in = EXCLUDED.opt_in, updated_at = NOW()`,
		s.UserID, string(s.Channel), s.OptIn)
	if err != nil {
		return fmt.Errorf("upsert setting: %w", err)
	}
	return nil
}
