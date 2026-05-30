-- +goose Up
-- +goose StatementBegin
-- Composite index for user-scoped keyset pagination on notification_log.
-- The partial WHERE clause filters to rows with a user_id in the recipient JSONB,
-- which are the only rows returned by GET /v1/notifications.
CREATE INDEX IF NOT EXISTS notification_log_user_ts_idx
    ON notification_log (
        ((recipient->>'user_id')::bigint),
        created_at DESC,
        id DESC
    )
    WHERE recipient->>'user_id' IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS notification_log_user_ts_idx;
-- +goose StatementEnd
