-- +goose Up
-- +goose StatementBegin
-- Templates are now stored in MongoDB. Drop the FK on notification_log first,
-- then drop the notification_templates table.
ALTER TABLE notification_log
    DROP CONSTRAINT IF EXISTS notification_log_template_id_fkey;

DROP TABLE IF EXISTS notification_templates;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS notification_templates (
    id         UUID        PRIMARY KEY,
    name       TEXT        NOT NULL,
    channel    TEXT        NOT NULL CHECK (channel IN ('push_ios','push_android','sms','email')),
    locale     TEXT        NOT NULL DEFAULT 'en',
    subject    TEXT        NOT NULL DEFAULT '',
    body       TEXT        NOT NULL,
    version    INT         NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name, channel, locale, version)
);

ALTER TABLE notification_log
    ADD CONSTRAINT notification_log_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES notification_templates(id) ON DELETE SET NULL;
-- +goose StatementEnd
