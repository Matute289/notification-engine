-- DEFAULT 0 satisfies the NOT NULL constraint for existing rows during the ALTER.
-- The default is dropped immediately so future INSERTs that omit owner_user_id
-- fail at the DB level rather than silently storing the invalid sentinel 0.
ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS owner_user_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE notification_templates ALTER COLUMN owner_user_id DROP DEFAULT;
