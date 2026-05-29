# Design: Template CRUD, Device Delete, Social Channels

**Date:** 2026-05-29  
**Status:** Approved

---

## Overview

Three independent feature areas added to the notification engine:

1. **Template CRUD**: complete the template lifecycle — update (in-place), delete (hard), and list (grouped by channel).
2. **Device delete**: allow a user to unregister a push device by channel + token.
3. **Social channels**: add Telegram, WhatsApp, Line, and Facebook Messenger as delivery channels.

All additions follow the existing layered architecture: domain → port → service → infrastructure/handler → cmd composition root.

---

## 1. Template CRUD

### 1.1 Port

`internal/port/repository.go` — three new methods on `TemplateRepository`:

```go
Update(ctx context.Context, t domain.Template) error
Delete(ctx context.Context, id uuid.UUID) error
List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error)
// channel nil = all channels
```

### 1.2 Domain

`internal/domain/template.go` — new method on `Template`:

```go
func (t Template) UpdateFields(name, subject, body string, mediaURLs []string, now time.Time) (Template, error)
```

Validates that `name` and `body` are non-empty. Returns a new `Template` value with `UpdatedAt = now`. `Channel`, `Locale`, `Version`, and `OwnerUserID` are immutable after creation.

### 1.3 Services

Three new use-case structs in `internal/service/`:

| Struct | Input | Output |
|--------|-------|--------|
| `UpdateTemplate` | `UpdateTemplateInput{ID, Name, Subject, Body, MediaURLs}` | `domain.Template, error` |
| `DeleteTemplate` | `uuid.UUID` | `error` |
| `ListTemplates` | `ListTemplatesInput{OwnerUserID int64, Channel *domain.Channel}` | `[]domain.Template, error` |

`UpdateTemplate.Execute` calls `Get` → `UpdateFields` → `Update` (fetch-modify-save).

### 1.4 HTTP Handlers

| Method | Path | Handler file | Auth |
|--------|------|-------------|------|
| `PUT` | `/v1/templates/{id}` | `update_template.go` | `RequireServiceIdentity` |
| `DELETE` | `/v1/templates/{id}` | `delete_template.go` | `RequireServiceIdentity` |
| `GET` | `/v1/templates` | `list_templates.go` | service or user identity |

`GET /v1/templates` accepts optional `?channel=push_ios`. Response shape:

```json
{
  "push_ios": [ { "id": "...", "name": "...", ... } ],
  "email":    [ { "id": "...", "name": "...", ... } ]
}
```

`PUT` request body uses a new `UpdateTemplateRequest` DTO with only the mutable fields: `name`, `subject`, `body`, `media_urls`. (`channel`, `locale`, and `version` are immutable after creation and are not accepted in the PUT body.) Returns `200` with the updated `TemplateView`. `DELETE` returns `204 No Content`.

`GET /v1/templates` reads the `Identity` from context (set by the `Authenticate` middleware) regardless of kind (service or user). The `Identity.Subject` becomes `ownerUserID`, so both HMAC service clients and JWT users can only list templates they own.

### 1.5 Infrastructure Adapters

Both `infrastructure/postgres/templates.go` and `infrastructure/mongodb/templates.go` implement the three new port methods.

- **Postgres `Update`**: `UPDATE notification_templates SET name=$1, subject=$2, body=$3, updated_at=NOW() WHERE id=$4`.
- **Postgres `Delete`**: `DELETE FROM notification_templates WHERE id=$1`. If `RowsAffected == 0` → `domain.ErrNotFound`.
- **Postgres `List`**: `SELECT ... FROM notification_templates WHERE owner_user_id=$1 [AND channel=$2] ORDER BY name`.
- **MongoDB**: equivalent `UpdateOne`, `DeleteOne`, `Find` operations. Delete checks `DeletedCount == 0` → `domain.ErrNotFound`.

Delete is **hard-delete** — no `DeletedAt` field added to the domain.

---

## 2. Device Delete

### 2.1 Port

`internal/port/repository.go` — one new method on `UserRepository`:

```go
DeleteDevice(ctx context.Context, userID int64, channel domain.Channel, token domain.DeviceToken) error
```

### 2.2 Service

`internal/service/delete_device.go`:

```go
type DeleteDevice struct {
    Users port.UserRepository
}

type DeleteDeviceInput struct {
    UserID      int64
    Channel     domain.Channel
    DeviceToken domain.DeviceToken
}
```

Validates: channel must be push, token must not be empty. Delegates to `Users.DeleteDevice`.

### 2.3 HTTP Handler

`cmd/api/http/handlers/delete_device.go`:

```
DELETE /v1/users/{id}/devices
Body: { "channel": "push_ios", "device_token": "abc..." }
```

- Requires `RequireUserOwnership`.
- `204 No Content` on success.
- `404` if device not found (`domain.ErrNotFound`).

### 2.4 Infrastructure Adapter

`infrastructure/postgres/users.go`:

```sql
DELETE FROM devices WHERE user_id=$1 AND channel=$2 AND device_token=$3
```

If `RowsAffected == 0` → `domain.ErrNotFound`.

---

## 3. Social Channels

### 3.1 Domain — Channel

`internal/domain/channel.go` — four new constants:

```go
ChannelTelegram          Channel = "telegram"
ChannelWhatsApp          Channel = "whatsapp"
ChannelLine              Channel = "line"
ChannelFacebookMessenger Channel = "facebook_messenger"
```

`AllChannels()` and `Valid()` updated to include all four. `IsPush()` unchanged.

### 3.2 Domain — Recipient

`internal/domain/recipient.go` — new field:

```go
MessagingID string `json:"messaging_id,omitempty"`
```

`Recipient.Validate` extended:

| Channel | Required field |
|---------|---------------|
| `whatsapp` | `Phone` (existing) |
| `telegram` | `MessagingID` |
| `line` | `MessagingID` |
| `facebook_messenger` | `MessagingID` |

### 3.3 Providers (skeletons)

Four new packages in `infrastructure/provider/`, each following the same structure as existing providers:

| Package | API | Config fields |
|---------|-----|--------------|
| `telegram/` | Telegram Bot API (`https://api.telegram.org/bot{token}/sendMessage`) | `BotToken` |
| `whatsapp/` | Meta Cloud API (`https://graph.facebook.com/v18.0/{phone_number_id}/messages`) | `PhoneNumberID`, `AccessToken` |
| `line/` | LINE Messaging API (`https://api.line.me/v2/bot/message/push`) | `ChannelAccessToken` |
| `fbmessenger/` | Meta Graph API (`https://graph.facebook.com/v18.0/me/messages`) | `PageAccessToken` |

Each has:
- `Provider` struct + `Config` struct
- `New(cfg Config) *Provider`
- `Send(ctx, n *domain.Notification) error` — builds the platform-specific JSON payload and POSTs it
- `var _ port.NotificationProvider = (*Provider)(nil)`

Real credential wiring is not implemented (same pattern as APNs/FCM stubs today).

### 3.4 Worker Composition Root

`cmd/worker/main.go` — `buildProvider` switch gains four new cases for real mode. `internal/platform/config/` gains the corresponding env vars:

| Env var | Channel |
|---------|---------|
| `TELEGRAM_BOT_TOKEN` | telegram |
| `WHATSAPP_PHONE_NUMBER_ID` + `WHATSAPP_ACCESS_TOKEN` | whatsapp |
| `LINE_CHANNEL_ACCESS_TOKEN` | line |
| `FB_PAGE_ACCESS_TOKEN` | facebook_messenger |

No DB migration needed: channel is stored as a string in Postgres/MongoDB.

---

## 4. Tests

### 4.1 Unit Tests

**Domain:**
- `internal/domain/template_test.go`: cases for `UpdateFields` (valid, empty name, empty body).
- `internal/domain/channel_test.go`: extend `Valid()` and `ParseChannel()` cases for the 4 new channels.
- `internal/domain/recipient_test.go`: extend `Validate` cases for WhatsApp (needs Phone), Telegram/Line/Messenger (need MessagingID).

**Services** (`internal/service/`):
- `update_template_test.go`: happy path, not found, invalid input, repo error.
- `delete_template_test.go`: happy path, not found, repo error.
- `list_templates_test.go`: all channels, filter by channel, empty result.
- `delete_device_test.go`: happy path, non-push channel rejected, empty token rejected, not found.

**Handlers** (`cmd/api/http/handlers/`):
- `update_template_test.go`: 200, 400 (bad JSON, bad channel), 404, 500.
- `delete_template_test.go`: 204, 404, 500.
- `list_templates_test.go`: 200 grouped, 200 filtered by channel, 500.
- `delete_device_test.go`: 204, 400 (bad JSON, bad channel), 404, 500.

**Providers** (`infrastructure/provider/`):
- One `*_test.go` per new provider using `httptest.NewServer` to verify the HTTP payload shape — same pattern as `sendgrid_test.go` and `twilio_test.go`.

All service tests use the port fakes in `internal/service/fakes_test.go` (extended with the new methods). Handler tests use the shared fakes in `cmd/api/http/handlers/fakes_test.go`.

### 4.2 Integration Tests

`test/integration/` — three new scenarios added to the existing suite:

1. **Template update + get**: create → update (name, body) → get by ID → assert `UpdatedAt` changed and new body persists.
2. **Template list by channel**: create 2 email + 1 push_ios template → list all (assert grouped) → list `?channel=email` (assert only email group).
3. **Device delete**: register device → delete by token → delete again → assert `404`.

---

## API Summary

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/v1/templates/{id}` | Update template (in-place) |
| `DELETE` | `/v1/templates/{id}` | Delete template (hard) |
| `GET` | `/v1/templates` | List templates grouped by channel |
| `DELETE` | `/v1/users/{id}/devices` | Delete device by channel + token |

New channel values accepted everywhere a `channel` field appears: `telegram`, `whatsapp`, `line`, `facebook_messenger`.
