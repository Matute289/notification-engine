-- +goose Up
-- +goose StatementBegin
-- Transactional outbox: SubmitNotification writes notification_log + outbox in
-- one DB transaction. The outbox-relay process drains pending rows and
-- publishes them to the channel queue, then marks them published. This makes
-- "row exists / message published" atomic from the API's point of view —
-- crashes after row insert but before publish are recoverable by the relay
-- without losing any notification.

CREATE TABLE notification_outbox (
    id              BIGSERIAL   PRIMARY KEY,
    notification_id UUID        NOT NULL REFERENCES notification_log(id) ON DELETE CASCADE,
    channel         TEXT        NOT NULL,
    payload         BYTEA       NOT NULL,         -- the AMQP message body, ready to ship
    status          TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','published','failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ NULL
);

-- The relay reads pending rows oldest-first and uses SKIP LOCKED so several
-- relays can run in parallel without trampling each other.
CREATE INDEX notification_outbox_pending_idx
    ON notification_outbox (created_at)
    WHERE status = 'pending';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS notification_outbox;
-- +goose StatementEnd
