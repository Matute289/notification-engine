-- +goose Up
-- +goose StatementBegin
CREATE TABLE users (
    id           BIGSERIAL PRIMARY KEY,
    email        TEXT        NOT NULL,
    country_code TEXT        NOT NULL DEFAULT '',
    phone_number TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX users_email_idx ON users (LOWER(email)) WHERE email <> '';

CREATE TABLE devices (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_token      TEXT        NOT NULL,
    channel           TEXT        NOT NULL CHECK (channel IN ('push_ios','push_android')),
    last_logged_in_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel, device_token)
);
CREATE INDEX devices_user_idx ON devices (user_id);

CREATE TABLE notification_settings (
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel    TEXT        NOT NULL CHECK (channel IN ('push_ios','push_android','sms','email')),
    opt_in     BOOLEAN     NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, channel)
);

CREATE TABLE notification_templates (
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

CREATE TABLE notification_log (
    id          UUID        PRIMARY KEY,
    event_id    TEXT        NOT NULL,
    channel     TEXT        NOT NULL,
    recipient   JSONB       NOT NULL,
    template_id UUID        NULL REFERENCES notification_templates(id) ON DELETE SET NULL,
    variables   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    subject     TEXT        NOT NULL DEFAULT '',
    body        TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL,
    attempt     INT         NOT NULL DEFAULT 0,
    last_error  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX notification_log_event_idx ON notification_log (event_id);
CREATE INDEX notification_log_status_idx ON notification_log (status, created_at);

CREATE TABLE analytics_events (
    id              BIGSERIAL   PRIMARY KEY,
    notification_id UUID        NOT NULL REFERENCES notification_log(id) ON DELETE CASCADE,
    event_type      TEXT        NOT NULL,
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX analytics_events_notif_idx ON analytics_events (notification_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS analytics_events;
DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS notification_settings;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
