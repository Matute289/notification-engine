-- +goose Up
-- +goose StatementBegin
ALTER TABLE notification_settings
    DROP CONSTRAINT IF EXISTS notification_settings_channel_check;

ALTER TABLE notification_settings
    ADD CONSTRAINT notification_settings_channel_check
        CHECK (channel IN (
            'push_ios','push_android','sms','email',
            'telegram','whatsapp','line','facebook_messenger'
        ));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE notification_settings
    DROP CONSTRAINT IF EXISTS notification_settings_channel_check;

ALTER TABLE notification_settings
    ADD CONSTRAINT notification_settings_channel_check
        CHECK (channel IN ('push_ios','push_android','sms','email'));
-- +goose StatementEnd
