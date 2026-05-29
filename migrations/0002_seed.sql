-- +goose Up
-- +goose StatementBegin
-- Demo users + devices + opt-in defaults.
-- Templates are stored in MongoDB; see seed-mongo service in docker-compose.yml.
-- Social channel settings are not inserted here (constraint is extended in 0006);
-- the domain defaults to opt-in for any (user, channel) with no explicit row.
INSERT INTO users (id, email, country_code, phone_number)
VALUES
  (1,  'demo@example.com',  '+1', '5551234567'),
  (42, 'user42@example.com', '+1', '5559876543'),
  (43, 'user43@example.com', '+1', '5550001111');
SELECT setval(pg_get_serial_sequence('users', 'id'), 43, true);

INSERT INTO devices (user_id, device_token, channel)
VALUES
  (1,  'demo-ios-token',       'push_ios'),
  (1,  'demo-android-token',   'push_android'),
  (42, 'user42-ios-token',     'push_ios'),
  (42, 'user42-android-token', 'push_android');

INSERT INTO notification_settings (user_id, channel, opt_in)
SELECT u.id, c.channel, TRUE
FROM (VALUES (1),(42),(43)) AS u(id)
CROSS JOIN (VALUES ('push_ios'),('push_android'),('sms'),('email')) AS c(channel);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM notification_settings WHERE user_id IN (1, 42, 43);
DELETE FROM devices WHERE user_id IN (1, 42, 43);
DELETE FROM users WHERE id IN (1, 42, 43);
-- +goose StatementEnd
