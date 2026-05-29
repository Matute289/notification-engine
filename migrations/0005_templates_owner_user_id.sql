-- +goose Up
-- +goose StatementBegin
-- No-op: notification_templates was moved to MongoDB and the Postgres table
-- was dropped in 0004_template_to_mongodb.sql. The owner_user_id column lives
-- on the MongoDB document instead. This migration intentionally does nothing.
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
