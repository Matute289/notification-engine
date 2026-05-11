-- +goose Up
-- +goose StatementBegin
-- Demo user + device + opt-in defaults + a couple of templates so a tester
-- can issue a meaningful POST /v1/notifications immediately after boot.
INSERT INTO users (id, email, country_code, phone_number)
VALUES (1, 'demo@example.com', '+1', '5551234567');
SELECT setval(pg_get_serial_sequence('users', 'id'), 1, true);

INSERT INTO devices (user_id, device_token, channel)
VALUES
  (1, 'demo-ios-token', 'push_ios'),
  (1, 'demo-android-token', 'push_android');

INSERT INTO notification_settings (user_id, channel, opt_in) VALUES
  (1, 'push_ios', TRUE),
  (1, 'push_android', TRUE),
  (1, 'sms', TRUE),
  (1, 'email', TRUE);

INSERT INTO notification_templates (id, name, channel, locale, subject, body, version) VALUES
  ('11111111-1111-1111-1111-111111111111', 'welcome', 'email', 'en',
   'Welcome, {{.Name}}!',
   'Hi {{.Name}}, thanks for joining {{.Product}}.', 1),
  ('22222222-2222-2222-2222-222222222222', 'order_shipped', 'sms', 'en', '',
   'Your order #{{.OrderID}} has shipped.', 1),
  ('33333333-3333-3333-3333-333333333333', 'game_request', 'push_ios', 'en', 'Game Request',
   '{{.From}} wants to play chess', 1);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM notification_templates WHERE id IN (
  '11111111-1111-1111-1111-111111111111',
  '22222222-2222-2222-2222-222222222222',
  '33333333-3333-3333-3333-333333333333'
);
DELETE FROM notification_settings WHERE user_id = 1;
DELETE FROM devices WHERE user_id = 1;
DELETE FROM users WHERE id = 1;
-- +goose StatementEnd
