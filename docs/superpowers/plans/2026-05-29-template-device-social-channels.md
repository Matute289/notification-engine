# Template CRUD, Device Delete & Social Channels — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add template update/delete/list, device delete, and 4 social channel providers (Telegram, WhatsApp, Line, Facebook Messenger) following the existing layered architecture.

**Architecture:** Domain changes first (channel constants, Recipient.MessagingID, Template.UpdateFields), then port interface extensions, then service fakes, then services, then infrastructure adapters, then HTTP handlers, then composition root wiring. Social channels follow the existing twilio/sendgrid provider skeleton pattern.

**Tech Stack:** Go 1.22+, chi router, pgx v5, MongoDB go-driver v2, go-redis v9, testify, httptest.

---

## File Map

**New files:**
- `internal/service/update_template.go` + `update_template_test.go`
- `internal/service/delete_template.go` + `delete_template_test.go`
- `internal/service/list_templates.go` + `list_templates_test.go`
- `internal/service/delete_device.go` + `delete_device_test.go`
- `migrations/0006_social_channels.sql`
- `infrastructure/provider/telegram/telegram.go` + `telegram_test.go`
- `infrastructure/provider/whatsapp/whatsapp.go` + `whatsapp_test.go`
- `infrastructure/provider/line/line.go` + `line_test.go`
- `infrastructure/provider/fbmessenger/fbmessenger.go` + `fbmessenger_test.go`
- `cmd/api/http/dto/update_template_request.go`
- `cmd/api/http/handlers/update_template.go` + `update_template_test.go`
- `cmd/api/http/handlers/delete_template.go` + `delete_template_test.go`
- `cmd/api/http/handlers/list_templates.go` + `list_templates_test.go`
- `cmd/api/http/handlers/delete_device.go` + `delete_device_test.go`

**Modified files:**
- `internal/domain/channel.go` + `channel_test.go`
- `internal/domain/recipient.go` + `recipient_test.go`
- `internal/domain/template.go` + `template_test.go`
- `internal/port/repository.go`
- `internal/service/fakes_test.go`
- `internal/service/submit_notification.go` (hydrateRecipient)
- `infrastructure/mongodb/templates.go`
- `infrastructure/postgres/templates.go`
- `infrastructure/postgres/users.go`
- `infrastructure/redis/template_cache.go`
- `internal/platform/config/config.go`
- `cmd/worker/main.go`
- `cmd/api/http/handlers/handler.go`
- `cmd/api/http/handlers/fakes_test.go`
- `cmd/api/http/router.go`
- `cmd/api/main.go`
- `test/integration/api_test.go`

---

## Task 1: Domain — 4 new channel constants

**Files:**
- Modify: `internal/domain/channel.go`
- Modify: `internal/domain/channel_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/domain/channel_test.go`:

```go
package domain

import "testing"

func TestChannel_Valid_SocialChannels(t *testing.T) {
	cases := []struct {
		ch   Channel
		want bool
	}{
		{ChannelTelegram, true},
		{ChannelWhatsApp, true},
		{ChannelLine, true},
		{ChannelFacebookMessenger, true},
		{Channel("discord"), false},
	}
	for _, c := range cases {
		if got := c.ch.Valid(); got != c.want {
			t.Errorf("Channel(%q).Valid() = %v, want %v", c.ch, got, c.want)
		}
	}
}

func TestParseChannel_SocialChannels(t *testing.T) {
	for _, s := range []string{"telegram", "whatsapp", "line", "facebook_messenger"} {
		ch, err := ParseChannel(s)
		if err != nil {
			t.Errorf("ParseChannel(%q) unexpected error: %v", s, err)
		}
		if string(ch) != s {
			t.Errorf("ParseChannel(%q) = %q, want %q", s, ch, s)
		}
	}
}

func TestAllChannels_IncludesSocialChannels(t *testing.T) {
	all := AllChannels()
	want := map[Channel]bool{
		ChannelTelegram: true, ChannelWhatsApp: true,
		ChannelLine: true, ChannelFacebookMessenger: true,
	}
	found := map[Channel]bool{}
	for _, ch := range all {
		found[ch] = true
	}
	for ch := range want {
		if !found[ch] {
			t.Errorf("AllChannels() missing %q", ch)
		}
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test -race -run "TestChannel_Valid_Social|TestParseChannel_Social|TestAllChannels_Includes" ./internal/domain/...
```
Expected: compile error (ChannelTelegram etc. undefined)

- [ ] **Step 3: Add the 4 new channel constants**

Replace the constants block and update `AllChannels` and `Valid` in `internal/domain/channel.go`:

```go
const (
	ChannelPushIOS          Channel = "push_ios"
	ChannelPushAndroid      Channel = "push_android"
	ChannelSMS              Channel = "sms"
	ChannelEmail            Channel = "email"
	ChannelTelegram         Channel = "telegram"
	ChannelWhatsApp         Channel = "whatsapp"
	ChannelLine             Channel = "line"
	ChannelFacebookMessenger Channel = "facebook_messenger"
)

func AllChannels() []Channel {
	return []Channel{
		ChannelPushIOS, ChannelPushAndroid, ChannelSMS, ChannelEmail,
		ChannelTelegram, ChannelWhatsApp, ChannelLine, ChannelFacebookMessenger,
	}
}

func (c Channel) Valid() bool {
	switch c {
	case ChannelPushIOS, ChannelPushAndroid, ChannelSMS, ChannelEmail,
		ChannelTelegram, ChannelWhatsApp, ChannelLine, ChannelFacebookMessenger:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test -race -run "TestChannel_Valid_Social|TestParseChannel_Social|TestAllChannels_Includes" ./internal/domain/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/channel.go internal/domain/channel_test.go
git commit -m "feat: add telegram/whatsapp/line/facebook_messenger channel constants"
```

---

## Task 2: Domain — Recipient.MessagingID + social channel validation

**Files:**
- Modify: `internal/domain/recipient.go`
- Modify: `internal/domain/recipient_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `TestRecipient_Validate` cases in `internal/domain/recipient_test.go`:

```go
{"whatsapp needs phone", ChannelWhatsApp, Recipient{}, true},
{"whatsapp happy", ChannelWhatsApp, Recipient{Phone: "+15551234567"}, false},
{"telegram needs messaging_id", ChannelTelegram, Recipient{}, true},
{"telegram happy", ChannelTelegram, Recipient{MessagingID: "123456789"}, false},
{"line needs messaging_id", ChannelLine, Recipient{}, true},
{"line happy", ChannelLine, Recipient{MessagingID: "U1234567890"}, false},
{"fbmessenger needs messaging_id", ChannelFacebookMessenger, Recipient{}, true},
{"fbmessenger happy", ChannelFacebookMessenger, Recipient{MessagingID: "987654321"}, false},
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestRecipient_Validate ./internal/domain/...
```
Expected: compile error (MessagingID field undefined)

- [ ] **Step 3: Add MessagingID field and update Validate**

In `internal/domain/recipient.go`, add the field to `Recipient` and update `Validate`:

```go
type Recipient struct {
	UserID      *UserID     `json:"user_id,omitempty"`
	Email       Email       `json:"email,omitempty"`
	Phone       Phone       `json:"phone_number,omitempty"`
	DeviceToken DeviceToken `json:"device_token,omitempty"`
	MessagingID string      `json:"messaging_id,omitempty"`
}

func (r Recipient) Validate(c Channel) error {
	if r.UserID == nil && r.Email.Empty() && r.Phone.Empty() && r.DeviceToken.Empty() && r.MessagingID == "" {
		return fmt.Errorf("%w: recipient must carry user_id or a raw destination", ErrInvalidInput)
	}
	if r.UserID != nil {
		return nil
	}
	switch c {
	case ChannelEmail:
		if r.Email.Empty() {
			return fmt.Errorf("%w: email channel needs an email", ErrInvalidInput)
		}
	case ChannelSMS:
		if r.Phone.Empty() {
			return fmt.Errorf("%w: sms channel needs a phone", ErrInvalidInput)
		}
	case ChannelPushIOS, ChannelPushAndroid:
		if r.DeviceToken.Empty() {
			return fmt.Errorf("%w: push channel needs a device token", ErrInvalidInput)
		}
	case ChannelWhatsApp:
		if r.Phone.Empty() {
			return fmt.Errorf("%w: whatsapp channel needs a phone", ErrInvalidInput)
		}
	case ChannelTelegram, ChannelLine, ChannelFacebookMessenger:
		if r.MessagingID == "" {
			return fmt.Errorf("%w: %s channel needs a messaging_id", ErrInvalidInput, c)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestRecipient_Validate ./internal/domain/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/recipient.go internal/domain/recipient_test.go
git commit -m "feat: add MessagingID to Recipient for social channel support"
```

---

## Task 3: Domain — Template.UpdateFields

**Files:**
- Modify: `internal/domain/template.go`
- Modify: `internal/domain/template_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/domain/template_test.go`:

```go
func TestTemplate_UpdateFields_HappyPath(t *testing.T) {
	now := time.Now()
	tpl, err := NewTemplate(uuid.New(), "orig", ChannelSMS, "en", "Old Subject", "Old Body", nil, 1, 1, now)
	require.NoError(t, err)

	later := now.Add(time.Hour)
	updated, err := tpl.UpdateFields("new name", "New Subject", "New Body", []string{"http://img.example.com/a.png"}, later)
	require.NoError(t, err)

	assert.Equal(t, "new name", updated.Name)
	assert.Equal(t, "New Subject", updated.Subject)
	assert.Equal(t, "New Body", updated.Body)
	assert.Equal(t, []string{"http://img.example.com/a.png"}, updated.MediaURLs)
	assert.Equal(t, later, updated.UpdatedAt)
	// Immutable fields must not change.
	assert.Equal(t, tpl.ID, updated.ID)
	assert.Equal(t, tpl.Channel, updated.Channel)
	assert.Equal(t, tpl.Locale, updated.Locale)
	assert.Equal(t, tpl.Version, updated.Version)
	assert.Equal(t, tpl.OwnerUserID, updated.OwnerUserID)
	assert.Equal(t, tpl.CreatedAt, updated.CreatedAt)
}

func TestTemplate_UpdateFields_EmptyName(t *testing.T) {
	tpl, _ := NewTemplate(uuid.New(), "orig", ChannelSMS, "en", "", "Body", nil, 1, 1, time.Now())
	_, err := tpl.UpdateFields("", "Subject", "Body", nil, time.Now())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidInput))
}

func TestTemplate_UpdateFields_EmptyBody(t *testing.T) {
	tpl, _ := NewTemplate(uuid.New(), "orig", ChannelSMS, "en", "", "Body", nil, 1, 1, time.Now())
	_, err := tpl.UpdateFields("name", "Subject", "", nil, time.Now())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidInput))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestTemplate_UpdateFields ./internal/domain/...
```
Expected: compile error (UpdateFields undefined)

- [ ] **Step 3: Add UpdateFields to template.go**

Append to `internal/domain/template.go`:

```go
// UpdateFields returns a new Template with the mutable fields replaced.
// Channel, Locale, Version, and OwnerUserID are immutable.
func (t Template) UpdateFields(name, subject, body string, mediaURLs []string, now time.Time) (Template, error) {
	if name == "" {
		return Template{}, fmt.Errorf("%w: template name required", ErrInvalidInput)
	}
	if body == "" {
		return Template{}, fmt.Errorf("%w: template body required", ErrInvalidInput)
	}
	updated := t
	updated.Name = name
	updated.Subject = subject
	updated.Body = body
	updated.MediaURLs = mediaURLs
	updated.UpdatedAt = now
	return updated, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestTemplate_UpdateFields ./internal/domain/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/template.go internal/domain/template_test.go
git commit -m "feat: add Template.UpdateFields for in-place mutation"
```

---

## Task 4: Port — extend TemplateRepository and UserRepository

**Files:**
- Modify: `internal/port/repository.go`

- [ ] **Step 1: Add three methods to TemplateRepository and one to UserRepository**

Replace the two interfaces in `internal/port/repository.go`:

```go
// TemplateRepository persists notification templates.
type TemplateRepository interface {
	Create(ctx context.Context, t domain.Template) error
	Get(ctx context.Context, id uuid.UUID) (domain.Template, error)
	Update(ctx context.Context, t domain.Template) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error)
}

// UserRepository persists users, devices, and per-channel settings.
type UserRepository interface {
	GetUser(ctx context.Context, id int64) (domain.User, error)
	DevicesForUser(ctx context.Context, userID int64, channel domain.Channel) ([]domain.Device, error)
	UpsertDevice(ctx context.Context, d domain.Device) error
	DeleteDevice(ctx context.Context, userID int64, channel domain.Channel, token domain.DeviceToken) error
	GetSetting(ctx context.Context, userID int64, channel domain.Channel) (domain.Setting, error)
	UpsertSetting(ctx context.Context, s domain.Setting) error
}
```

- [ ] **Step 2: Verify the project still builds (it won't — fakes and adapters need updating)**

```bash
go build ./... 2>&1 | head -30
```
Expected: compile errors listing every type that claims to implement TemplateRepository or UserRepository. This is expected — Tasks 5–14 will fix them.

- [ ] **Step 3: Commit**

```bash
git add internal/port/repository.go
git commit -m "feat: extend TemplateRepository and UserRepository ports"
```

---

## Task 5: Extend service fakes and handler fakes

**Files:**
- Modify: `internal/service/fakes_test.go`
- Modify: `cmd/api/http/handlers/fakes_test.go`

- [ ] **Step 1: Add missing methods to fakeTemplates in service fakes**

In `internal/service/fakes_test.go`, append the following methods to `fakeTemplates`:

```go
func (f *fakeTemplates) Update(_ context.Context, t domain.Template) error {
	if _, ok := f.tpls[t.ID]; !ok {
		return domain.ErrNotFound
	}
	f.tpls[t.ID] = t
	return nil
}

func (f *fakeTemplates) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := f.tpls[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.tpls, id)
	return nil
}

func (f *fakeTemplates) List(_ context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	var out []domain.Template
	for _, t := range f.tpls {
		if t.OwnerUserID != ownerUserID {
			continue
		}
		if channel != nil && t.Channel != *channel {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
```

Also add `DeleteDevice` to `fakeUsers` in the same file:

```go
func (f *fakeUsers) DeleteDevice(_ context.Context, userID int64, ch domain.Channel, token domain.DeviceToken) error {
	devices := f.devices[userID][ch]
	for i, d := range devices {
		if d.DeviceToken == token {
			f.devices[userID][ch] = append(devices[:i], devices[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}
```

- [ ] **Step 2: Add missing methods to templateRepo and userRepo in handler fakes**

In `cmd/api/http/handlers/fakes_test.go`, add to `templateRepo`:

```go
func (r *templateRepo) Update(_ context.Context, _ domain.Template) error { return r.err }
func (r *templateRepo) Delete(_ context.Context, _ uuid.UUID) error       { return r.err }
func (r *templateRepo) List(_ context.Context, _ int64, _ *domain.Channel) ([]domain.Template, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.t.ID != (uuid.UUID{}) {
		return []domain.Template{r.t}, nil
	}
	return nil, nil
}
```

Add to `userRepo`:

```go
func (r *userRepo) DeleteDevice(_ context.Context, _ int64, _ domain.Channel, _ domain.DeviceToken) error {
	return r.err
}
```

- [ ] **Step 3: Verify tests still compile**

```bash
go test -race -count=1 ./internal/service/... ./cmd/api/http/...
```
Expected: compile errors will remain in infrastructure adapters but service and handler packages should compile.

- [ ] **Step 4: Commit**

```bash
git add internal/service/fakes_test.go cmd/api/http/handlers/fakes_test.go
git commit -m "test: extend fakes for new port methods"
```

---

## Task 6: UpdateTemplate service

**Files:**
- Create: `internal/service/update_template.go`
- Create: `internal/service/update_template_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/update_template_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTemplate_HappyPath(t *testing.T) {
	repo := newFakeTemplates()
	clock := fixedClock{t: time.Unix(1700000000, 0)}
	id := uuid.New()
	orig, _ := domain.NewTemplate(id, "orig", domain.ChannelSMS, "en", "", "Old Body", nil, 1, 42, clock.Now().Add(-time.Hour))
	repo.tpls[id] = orig

	svc := &UpdateTemplate{Templates: repo, Clock: clock}
	got, err := svc.Execute(context.Background(), UpdateTemplateInput{
		ID: id, Name: "new name", Subject: "New Subject", Body: "New Body",
		MediaURLs: []string{"http://x.com/img.png"}, OwnerUserID: 42,
	})
	require.NoError(t, err)
	assert.Equal(t, "new name", got.Name)
	assert.Equal(t, "New Body", got.Body)
	assert.Equal(t, clock.Now(), got.UpdatedAt)
	assert.Equal(t, int64(42), got.OwnerUserID)
}

func TestUpdateTemplate_NotFound(t *testing.T) {
	svc := &UpdateTemplate{Templates: newFakeTemplates(), Clock: fixedClock{}}
	_, err := svc.Execute(context.Background(), UpdateTemplateInput{ID: uuid.New(), Name: "n", Body: "b", OwnerUserID: 1})
	require.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestUpdateTemplate_Forbidden_WrongOwner(t *testing.T) {
	repo := newFakeTemplates()
	id := uuid.New()
	orig, _ := domain.NewTemplate(id, "orig", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[id] = orig

	svc := &UpdateTemplate{Templates: repo, Clock: fixedClock{}}
	_, err := svc.Execute(context.Background(), UpdateTemplateInput{ID: id, Name: "n", Body: "b", OwnerUserID: 99})
	require.True(t, errors.Is(err, domain.ErrForbidden))
}

func TestUpdateTemplate_InvalidInput_EmptyBody(t *testing.T) {
	repo := newFakeTemplates()
	id := uuid.New()
	orig, _ := domain.NewTemplate(id, "orig", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[id] = orig

	svc := &UpdateTemplate{Templates: repo, Clock: fixedClock{}}
	_, err := svc.Execute(context.Background(), UpdateTemplateInput{ID: id, Name: "n", Body: "", OwnerUserID: 42})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestUpdateTemplate ./internal/service/...
```
Expected: compile error (UpdateTemplate type undefined)

- [ ] **Step 3: Create the service**

Create `internal/service/update_template.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
)

// UpdateTemplate updates the mutable fields of an existing template in-place.
type UpdateTemplate struct {
	Templates port.TemplateRepository
	Clock     port.Clock
}

type UpdateTemplateInput struct {
	ID          uuid.UUID
	Name        string
	Subject     string
	Body        string
	MediaURLs   []string
	OwnerUserID int64
}

func (u *UpdateTemplate) Execute(ctx context.Context, in UpdateTemplateInput) (domain.Template, error) {
	t, err := u.Templates.Get(ctx, in.ID)
	if err != nil {
		return domain.Template{}, err
	}
	if t.OwnerUserID != in.OwnerUserID {
		return domain.Template{}, fmt.Errorf("%w: template belongs to a different owner", domain.ErrForbidden)
	}
	updated, err := t.UpdateFields(in.Name, in.Subject, in.Body, in.MediaURLs, u.Clock.Now())
	if err != nil {
		return domain.Template{}, err
	}
	if err := u.Templates.Update(ctx, updated); err != nil {
		return domain.Template{}, err
	}
	return updated, nil
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestUpdateTemplate ./internal/service/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/update_template.go internal/service/update_template_test.go
git commit -m "feat: add UpdateTemplate service"
```

---

## Task 7: DeleteTemplate service

**Files:**
- Create: `internal/service/delete_template.go`
- Create: `internal/service/delete_template_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/delete_template_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDeleteTemplate_HappyPath(t *testing.T) {
	repo := newFakeTemplates()
	id := uuid.New()
	tpl, _ := domain.NewTemplate(id, "x", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[id] = tpl

	svc := &DeleteTemplate{Templates: repo}
	err := svc.Execute(context.Background(), DeleteTemplateInput{ID: id, OwnerUserID: 42})
	require.NoError(t, err)
	_, exists := repo.tpls[id]
	require.False(t, exists)
}

func TestDeleteTemplate_NotFound(t *testing.T) {
	svc := &DeleteTemplate{Templates: newFakeTemplates()}
	err := svc.Execute(context.Background(), DeleteTemplateInput{ID: uuid.New(), OwnerUserID: 1})
	require.True(t, errors.Is(err, domain.ErrNotFound))
}

func TestDeleteTemplate_Forbidden_WrongOwner(t *testing.T) {
	repo := newFakeTemplates()
	id := uuid.New()
	tpl, _ := domain.NewTemplate(id, "x", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[id] = tpl

	svc := &DeleteTemplate{Templates: repo}
	err := svc.Execute(context.Background(), DeleteTemplateInput{ID: id, OwnerUserID: 99})
	require.True(t, errors.Is(err, domain.ErrForbidden))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestDeleteTemplate ./internal/service/...
```
Expected: compile error

- [ ] **Step 3: Create the service**

Create `internal/service/delete_template.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
)

// DeleteTemplate hard-deletes a template the caller owns.
type DeleteTemplate struct {
	Templates port.TemplateRepository
}

type DeleteTemplateInput struct {
	ID          uuid.UUID
	OwnerUserID int64
}

func (u *DeleteTemplate) Execute(ctx context.Context, in DeleteTemplateInput) error {
	t, err := u.Templates.Get(ctx, in.ID)
	if err != nil {
		return err
	}
	if t.OwnerUserID != in.OwnerUserID {
		return fmt.Errorf("%w: template belongs to a different owner", domain.ErrForbidden)
	}
	return u.Templates.Delete(ctx, in.ID)
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestDeleteTemplate ./internal/service/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/delete_template.go internal/service/delete_template_test.go
git commit -m "feat: add DeleteTemplate service"
```

---

## Task 8: ListTemplates service

**Files:**
- Create: `internal/service/list_templates.go`
- Create: `internal/service/list_templates_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/list_templates_test.go`:

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTemplates_AllChannels(t *testing.T) {
	repo := newFakeTemplates()
	sms, _ := domain.NewTemplate(uuid.New(), "a", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	email, _ := domain.NewTemplate(uuid.New(), "b", domain.ChannelEmail, "en", "", "Body", nil, 1, 42, time.Now())
	other, _ := domain.NewTemplate(uuid.New(), "c", domain.ChannelSMS, "en", "", "Body", nil, 1, 99, time.Now())
	repo.tpls[sms.ID] = sms
	repo.tpls[email.ID] = email
	repo.tpls[other.ID] = other

	svc := &ListTemplates{Templates: repo}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42})
	require.NoError(t, err)
	require.Len(t, got, 2)
	ids := map[uuid.UUID]bool{got[0].ID: true, got[1].ID: true}
	assert.True(t, ids[sms.ID])
	assert.True(t, ids[email.ID])
	assert.False(t, ids[other.ID])
}

func TestListTemplates_FilterByChannel(t *testing.T) {
	repo := newFakeTemplates()
	sms, _ := domain.NewTemplate(uuid.New(), "a", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	email, _ := domain.NewTemplate(uuid.New(), "b", domain.ChannelEmail, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[sms.ID] = sms
	repo.tpls[email.ID] = email

	ch := domain.ChannelSMS
	svc := &ListTemplates{Templates: repo}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42, Channel: &ch})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, sms.ID, got[0].ID)
}

func TestListTemplates_Empty(t *testing.T) {
	svc := &ListTemplates{Templates: newFakeTemplates()}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42})
	require.NoError(t, err)
	assert.Empty(t, got)
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestListTemplates ./internal/service/...
```
Expected: compile error

- [ ] **Step 3: Create the service**

Create `internal/service/list_templates.go`:

```go
package service

import (
	"context"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// ListTemplates returns all templates owned by a user, optionally filtered by channel.
type ListTemplates struct {
	Templates port.TemplateRepository
}

type ListTemplatesInput struct {
	OwnerUserID int64
	Channel     *domain.Channel
}

func (u *ListTemplates) Execute(ctx context.Context, in ListTemplatesInput) ([]domain.Template, error) {
	return u.Templates.List(ctx, in.OwnerUserID, in.Channel)
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestListTemplates ./internal/service/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/list_templates.go internal/service/list_templates_test.go
git commit -m "feat: add ListTemplates service"
```

---

## Task 9: DeleteDevice service

**Files:**
- Create: `internal/service/delete_device.go`
- Create: `internal/service/delete_device_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/delete_device_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestDeleteDevice_HappyPath(t *testing.T) {
	users := newFakeUsers()
	users.devices[42] = map[domain.Channel][]domain.Device{
		domain.ChannelPushIOS: {{UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "tok", LastLoggedInAt: time.Now()}},
	}
	svc := &DeleteDevice{Users: users}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "tok",
	})
	require.NoError(t, err)
	require.Empty(t, users.devices[42][domain.ChannelPushIOS])
}

func TestDeleteDevice_NonPushChannel_Rejected(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelEmail, DeviceToken: "tok",
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeleteDevice_EmptyToken_Rejected(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "",
	})
	require.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeleteDevice_NotFound(t *testing.T) {
	svc := &DeleteDevice{Users: newFakeUsers()}
	err := svc.Execute(context.Background(), DeleteDeviceInput{
		UserID: 42, Channel: domain.ChannelPushIOS, DeviceToken: "unknown",
	})
	require.True(t, errors.Is(err, domain.ErrNotFound))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestDeleteDevice ./internal/service/...
```
Expected: compile error

- [ ] **Step 3: Create the service**

Create `internal/service/delete_device.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

// DeleteDevice unregisters a push device identified by channel + token.
type DeleteDevice struct {
	Users port.UserRepository
}

type DeleteDeviceInput struct {
	UserID      int64
	Channel     domain.Channel
	DeviceToken domain.DeviceToken
}

func (u *DeleteDevice) Execute(ctx context.Context, in DeleteDeviceInput) error {
	if !in.Channel.IsPush() {
		return fmt.Errorf("%w: device deletion requires a push channel", domain.ErrInvalidInput)
	}
	if in.DeviceToken.Empty() {
		return fmt.Errorf("%w: device_token required", domain.ErrInvalidInput)
	}
	return u.Users.DeleteDevice(ctx, in.UserID, in.Channel, in.DeviceToken)
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestDeleteDevice ./internal/service/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/delete_device.go internal/service/delete_device_test.go
git commit -m "feat: add DeleteDevice service"
```

---

## Task 10: Migration — add social channels to notification_settings constraint

**Files:**
- Create: `migrations/0006_social_channels.sql`

- [ ] **Step 1: Create the migration**

Create `migrations/0006_social_channels.sql`:

```sql
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
```

- [ ] **Step 2: Commit**

```bash
git add migrations/0006_social_channels.sql
git commit -m "feat: extend notification_settings channel constraint for social channels"
```

---

## Task 11: Extend hydrateRecipient for social channels

**Files:**
- Modify: `internal/service/submit_notification.go`

The `hydrateRecipient` function in `SubmitNotification` needs cases for the 4 new channels so a UserID-only recipient is properly resolved.

- [ ] **Step 1: Add social channel cases to the switch in hydrateRecipient**

In `internal/service/submit_notification.go`, locate the `hydrateRecipient` function and extend the switch statement. The full updated switch body:

```go
func (u *SubmitNotification) hydrateRecipient(ctx context.Context, ch domain.Channel, r *domain.Recipient) error {
	uid := *r.UserID
	switch ch {
	case domain.ChannelEmail:
		if !r.Email.Empty() {
			return nil
		}
		usr, err := u.Users.GetUser(ctx, uid)
		if err != nil {
			return fmt.Errorf("hydrate email: %w", err)
		}
		r.Email = usr.Email
	case domain.ChannelSMS, domain.ChannelWhatsApp:
		if !r.Phone.Empty() {
			return nil
		}
		usr, err := u.Users.GetUser(ctx, uid)
		if err != nil {
			return fmt.Errorf("hydrate phone: %w", err)
		}
		r.Phone = usr.FullPhone()
	case domain.ChannelPushIOS, domain.ChannelPushAndroid:
		if !r.DeviceToken.Empty() {
			return nil
		}
		devices, err := u.Users.DevicesForUser(ctx, uid, ch)
		if err != nil {
			return fmt.Errorf("hydrate push: %w", err)
		}
		if len(devices) == 0 {
			return fmt.Errorf("%w: no registered device for user %d on %s", domain.ErrInvalidInput, uid, ch)
		}
		r.DeviceToken = devices[0].DeviceToken
	case domain.ChannelTelegram, domain.ChannelLine, domain.ChannelFacebookMessenger:
		if r.MessagingID == "" {
			return fmt.Errorf("%w: messaging_id required for %s channel — cannot be resolved from user record", domain.ErrInvalidInput, ch)
		}
	}
	return nil
}
```

- [ ] **Step 2: Run existing submit tests to verify no regression**

```bash
go test -race -run TestSubmit ./internal/service/...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/submit_notification.go
git commit -m "feat: extend hydrateRecipient for social channels"
```

---

## Task 12: MongoDB template adapter — Update, Delete, List

**Files:**
- Modify: `infrastructure/mongodb/templates.go`

- [ ] **Step 1: Append Update, Delete, List to the TemplateRepository**

Add to `infrastructure/mongodb/templates.go` (after the existing `Get` method):

```go
func (r *TemplateRepository) Update(ctx context.Context, t domain.Template) error {
	filter := bson.M{"_id": t.ID.String()}
	update := bson.M{"$set": bson.M{
		"name":       t.Name,
		"subject":    t.Subject,
		"body":       t.Body,
		"media_urls": t.MediaURLs,
		"updated_at": t.UpdatedAt,
	}}
	res, err := r.col.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("mongodb: update template: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.col.DeleteOne(ctx, bson.M{"_id": id.String()})
	if err != nil {
		return fmt.Errorf("mongodb: delete template: %w", err)
	}
	if res.DeletedCount == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	if channel != nil {
		filter["channel"] = string(*channel)
	}
	opts := options.Find().SetSort(bson.D{{Key: "channel", Value: 1}, {Key: "name", Value: 1}})
	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("mongodb: list templates: %w", err)
	}
	defer cursor.Close(ctx)
	var out []domain.Template
	for cursor.Next(ctx) {
		var doc templateDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("mongodb: list templates decode: %w", err)
		}
		out = append(out, domain.Template{
			ID:          uuid.MustParse(doc.ID),
			Name:        doc.Name,
			Channel:     domain.Channel(doc.Channel),
			Locale:      doc.Locale,
			Subject:     doc.Subject,
			Body:        doc.Body,
			MediaURLs:   doc.MediaURLs,
			Version:     doc.Version,
			OwnerUserID: doc.OwnerUserID,
			CreatedAt:   doc.CreatedAt,
			UpdatedAt:   doc.UpdatedAt,
		})
	}
	return out, cursor.Err()
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./infrastructure/mongodb/...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add infrastructure/mongodb/templates.go
git commit -m "feat: add Update/Delete/List to MongoDB TemplateRepository"
```

---

## Task 13: Postgres template adapter — Update, Delete, List (compile stub)

**Files:**
- Modify: `infrastructure/postgres/templates.go`

Note: The `notification_templates` table was dropped in migration 0004. The Postgres adapter exists to satisfy the port interface at compile time. These implementations are consistent with the existing schema (no `media_urls` column) and would work if the table were restored via down-migration.

- [ ] **Step 1: Append Update, Delete, List**

Add to `infrastructure/postgres/templates.go` (after the existing `Get` method):

```go
func (r *TemplateRepository) Update(ctx context.Context, t domain.Template) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE notification_templates SET name=$1, subject=$2, body=$3, updated_at=NOW() WHERE id=$4`,
		t.Name, t.Subject, t.Body, t.ID)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM notification_templates WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *TemplateRepository) List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if channel != nil {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, channel, locale, subject, body, version, owner_user_id, created_at, updated_at
			   FROM notification_templates WHERE owner_user_id=$1 AND channel=$2 ORDER BY name`,
			ownerUserID, string(*channel))
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT id, name, channel, locale, subject, body, version, owner_user_id, created_at, updated_at
			   FROM notification_templates WHERE owner_user_id=$1 ORDER BY channel, name`,
			ownerUserID)
	}
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()
	var out []domain.Template
	for rows.Next() {
		var (
			t  domain.Template
			ch string
		)
		if err := rows.Scan(&t.ID, &t.Name, &ch, &t.Locale, &t.Subject, &t.Body,
			&t.Version, &t.OwnerUserID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list templates scan: %w", err)
		}
		t.Channel = domain.Channel(ch)
		out = append(out, t)
	}
	return out, rows.Err()
}
```

You need to add `"github.com/jackc/pgx/v5"` to the imports if it isn't already there (it is — `pgx.ErrNoRows` is already used).

- [ ] **Step 2: Verify compile**

```bash
go build ./infrastructure/postgres/...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add infrastructure/postgres/templates.go
git commit -m "feat: add Update/Delete/List to Postgres TemplateRepository (compile stub)"
```

---

## Task 14: Redis TemplateCache — Update, Delete, List

**Files:**
- Modify: `infrastructure/redis/template_cache.go`

- [ ] **Step 1: Append Update, Delete, List**

Add to `infrastructure/redis/template_cache.go` (after the existing `set` method):

```go
func (c *TemplateCache) Update(ctx context.Context, t domain.Template) error {
	if err := c.repo.Update(ctx, t); err != nil {
		return err
	}
	if c.cb == nil || c.cb.Allow() {
		c.set(ctx, t)
	}
	return nil
}

func (c *TemplateCache) Delete(ctx context.Context, id uuid.UUID) error {
	if err := c.repo.Delete(ctx, id); err != nil {
		return err
	}
	if c.cb == nil || c.cb.Allow() {
		key := templateCachePrefix + id.String()
		if delErr := c.rdb.Del(ctx, key).Err(); delErr != nil {
			if c.cb != nil && isRedisError(delErr) {
				c.cb.RecordFailure()
			}
		} else if c.cb != nil {
			c.cb.RecordSuccess()
		}
	}
	return nil
}

// List bypasses the cache: list queries are not cached to avoid stale reads after updates.
func (c *TemplateCache) List(ctx context.Context, ownerUserID int64, channel *domain.Channel) ([]domain.Template, error) {
	return c.repo.List(ctx, ownerUserID, channel)
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./infrastructure/redis/...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add infrastructure/redis/template_cache.go
git commit -m "feat: add Update/Delete/List to Redis TemplateCache"
```

---

## Task 15: Postgres users — DeleteDevice

**Files:**
- Modify: `infrastructure/postgres/users.go`

- [ ] **Step 1: Append DeleteDevice**

Add to `infrastructure/postgres/users.go` (after `UpsertDevice`):

```go
func (r *UserRepository) DeleteDevice(ctx context.Context, userID int64, channel domain.Channel, token domain.DeviceToken) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM devices WHERE user_id=$1 AND channel=$2 AND device_token=$3`,
		userID, string(channel), string(token))
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
```

- [ ] **Step 2: Verify compile + full build is clean**

```bash
go build ./...
```
Expected: PASS (all port implementations now satisfy the updated interfaces)

- [ ] **Step 3: Run all unit tests**

```bash
go test -race -count=1 ./...
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add infrastructure/postgres/users.go
git commit -m "feat: add DeleteDevice to Postgres UserRepository"
```

---

## Task 16: Config — social channel env vars and rate limits

**Files:**
- Modify: `internal/platform/config/config.go`

- [ ] **Step 1: Add social provider fields and RateLimit.SocialPerHour**

In `internal/platform/config/config.go`:

1. Add to the `RateLimit` struct:
```go
SocialPerHour int `env:"RATELIMIT_SOCIAL_PER_HOUR" envDefault:"10"`
```

2. Update `RateLimit.AsMap()` to include social channels:
```go
func (r RateLimit) AsMap() map[domain.Channel]int {
	return map[domain.Channel]int{
		domain.ChannelPushIOS:           r.PushPerHour,
		domain.ChannelPushAndroid:       r.PushPerHour,
		domain.ChannelSMS:               r.SMSPerHour,
		domain.ChannelEmail:             r.EmailPerHour,
		domain.ChannelTelegram:          r.SocialPerHour,
		domain.ChannelWhatsApp:          r.SocialPerHour,
		domain.ChannelLine:              r.SocialPerHour,
		domain.ChannelFacebookMessenger: r.SocialPerHour,
	}
}
```

3. Add social provider credential fields to `Config` (after the SendGrid fields):
```go
TelegramBotToken       string `env:"TELEGRAM_BOT_TOKEN"`
WhatsAppPhoneNumberID  string `env:"WHATSAPP_PHONE_NUMBER_ID"`
WhatsAppAccessToken    string `env:"WHATSAPP_ACCESS_TOKEN"`
LineChannelAccessToken string `env:"LINE_CHANNEL_ACCESS_TOKEN"`
FBPageAccessToken      string `env:"FB_PAGE_ACCESS_TOKEN"`
```

- [ ] **Step 2: Verify compile**

```bash
go build ./internal/platform/config/...
```
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/platform/config/config.go
git commit -m "feat: add social channel config fields and rate limits"
```

---

## Task 17: Telegram provider

**Files:**
- Create: `infrastructure/provider/telegram/telegram.go`
- Create: `infrastructure/provider/telegram/telegram_test.go`

- [ ] **Step 1: Write the failing test**

Create `infrastructure/provider/telegram/telegram_test.go`:

```go
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkTelegramNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelTelegram,
		domain.Recipient{UserID: &uid, MessagingID: "123456789"},
		nil, nil, "", "Hello from Telegram!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/botTOKEN/sendMessage")
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	p, err := New(Config{BotToken: "TOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkTelegramNotif(t)))
	require.Equal(t, "123456789", captured["chat_id"])
	require.Equal(t, "Hello from Telegram!", captured["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{BotToken: "TOKEN", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkTelegramNotif(t)), port.ErrTransient))
}

func TestSend_EmptyMessagingID_Error(t *testing.T) {
	p, _ := New(Config{BotToken: "TOKEN"})
	uid := int64(1)
	n, _ := domain.NewNotification(uuid.New(), "e", domain.ChannelTelegram,
		domain.Recipient{UserID: &uid, MessagingID: ""}, nil, nil, "", "body", time.Now())
	require.Error(t, p.Send(context.Background(), n))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestSend ./infrastructure/provider/telegram/...
```
Expected: package not found

- [ ] **Step 3: Create the provider**

Create `infrastructure/provider/telegram/telegram.go`:

```go
// Package telegram implements port.NotificationProvider for the Telegram Bot API.
// Reference: https://core.telegram.org/bots/api#sendmessage
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

type Config struct {
	BotToken string
	BaseURL  string // override for tests; defaults to "https://api.telegram.org"
	Client   *http.Client
	Timeout  time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.BotToken == "" {
		return nil, fmt.Errorf("telegram: BotToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{cfg: cfg, client: c}, nil
}

var _ port.NotificationProvider = (*Provider)(nil)

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.MessagingID == "" {
		return fmt.Errorf("telegram: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id": n.Recipient.MessagingID,
		"text":    n.Body,
	})
	if err != nil {
		return fmt.Errorf("telegram: marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", p.cfg.BaseURL, p.cfg.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: telegram http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("telegram http %d (terminal)", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestSend ./infrastructure/provider/telegram/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infrastructure/provider/telegram/
git commit -m "feat: add Telegram provider skeleton"
```

---

## Task 18: WhatsApp provider

**Files:**
- Create: `infrastructure/provider/whatsapp/whatsapp.go`
- Create: `infrastructure/provider/whatsapp/whatsapp_test.go`

- [ ] **Step 1: Write the failing test**

Create `infrastructure/provider/whatsapp/whatsapp_test.go`:

```go
package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkWhatsAppNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelWhatsApp,
		domain.Recipient{UserID: &uid, Phone: "+15551234567"},
		nil, nil, "", "Hello from WhatsApp!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/PHONE_ID/messages")
		require.Equal(t, "Bearer TOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.1"}]}`))
	}))
	defer srv.Close()

	p, err := New(Config{PhoneNumberID: "PHONE_ID", AccessToken: "TOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkWhatsAppNotif(t)))
	require.Equal(t, "whatsapp", captured["messaging_product"])
	require.Equal(t, "+15551234567", captured["to"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{PhoneNumberID: "P", AccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkWhatsAppNotif(t)), port.ErrTransient))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestSend ./infrastructure/provider/whatsapp/...
```
Expected: package not found

- [ ] **Step 3: Create the provider**

Create `infrastructure/provider/whatsapp/whatsapp.go`:

```go
// Package whatsapp implements port.NotificationProvider using the Meta Cloud API.
// Reference: https://developers.facebook.com/docs/whatsapp/cloud-api/messages
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

type Config struct {
	PhoneNumberID string
	AccessToken   string
	BaseURL       string // override for tests; defaults to "https://graph.facebook.com/v18.0"
	Client        *http.Client
	Timeout       time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.PhoneNumberID == "" || cfg.AccessToken == "" {
		return nil, fmt.Errorf("whatsapp: PhoneNumberID and AccessToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://graph.facebook.com/v18.0"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{cfg: cfg, client: c}, nil
}

var _ port.NotificationProvider = (*Provider)(nil)

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.Phone.Empty() {
		return fmt.Errorf("whatsapp: phone empty")
	}
	payload, err := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                string(n.Recipient.Phone),
		"type":              "text",
		"text":              map[string]string{"body": n.Body},
	})
	if err != nil {
		return fmt.Errorf("whatsapp: marshal payload: %w", err)
	}
	url := fmt.Sprintf("%s/%s/messages", p.cfg.BaseURL, p.cfg.PhoneNumberID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("whatsapp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.AccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: whatsapp http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("whatsapp http %d (terminal)", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestSend ./infrastructure/provider/whatsapp/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infrastructure/provider/whatsapp/
git commit -m "feat: add WhatsApp provider skeleton"
```

---

## Task 19: Line provider

**Files:**
- Create: `infrastructure/provider/line/line.go`
- Create: `infrastructure/provider/line/line_test.go`

- [ ] **Step 1: Write the failing test**

Create `infrastructure/provider/line/line_test.go`:

```go
package line

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkLineNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelLine,
		domain.Recipient{UserID: &uid, MessagingID: "Uf1234567890"},
		nil, nil, "", "Hello from Line!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/bot/message/push", r.URL.Path)
		require.Equal(t, "Bearer LINETOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p, err := New(Config{ChannelAccessToken: "LINETOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkLineNotif(t)))
	require.Equal(t, "Uf1234567890", captured["to"])
	msgs := captured["messages"].([]any)
	require.Len(t, msgs, 1)
	require.Equal(t, "Hello from Line!", msgs[0].(map[string]any)["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{ChannelAccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkLineNotif(t)), port.ErrTransient))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestSend ./infrastructure/provider/line/...
```
Expected: package not found

- [ ] **Step 3: Create the provider**

Create `infrastructure/provider/line/line.go`:

```go
// Package line implements port.NotificationProvider using the LINE Messaging API.
// Reference: https://developers.line.biz/en/reference/messaging-api/#send-push-message
package line

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

type Config struct {
	ChannelAccessToken string
	BaseURL            string // override for tests; defaults to "https://api.line.me"
	Client             *http.Client
	Timeout            time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.ChannelAccessToken == "" {
		return nil, fmt.Errorf("line: ChannelAccessToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.line.me"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{cfg: cfg, client: c}, nil
}

var _ port.NotificationProvider = (*Provider)(nil)

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.MessagingID == "" {
		return fmt.Errorf("line: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"to":       n.Recipient.MessagingID,
		"messages": []map[string]string{{"type": "text", "text": n.Body}},
	})
	if err != nil {
		return fmt.Errorf("line: marshal payload: %w", err)
	}
	url := p.cfg.BaseURL + "/v2/bot/message/push"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("line request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.ChannelAccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: line http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("line http %d (terminal)", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestSend ./infrastructure/provider/line/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infrastructure/provider/line/
git commit -m "feat: add Line provider skeleton"
```

---

## Task 20: Facebook Messenger provider

**Files:**
- Create: `infrastructure/provider/fbmessenger/fbmessenger.go`
- Create: `infrastructure/provider/fbmessenger/fbmessenger_test.go`

- [ ] **Step 1: Write the failing test**

Create `infrastructure/provider/fbmessenger/fbmessenger_test.go`:

```go
package fbmessenger

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mkFBNotif(t *testing.T) *domain.Notification {
	t.Helper()
	uid := int64(1)
	n, err := domain.NewNotification(
		uuid.New(), domain.EventID("evt"), domain.ChannelFacebookMessenger,
		domain.Recipient{UserID: &uid, MessagingID: "987654321"},
		nil, nil, "", "Hello from Messenger!", time.Unix(1700000000, 0))
	require.NoError(t, err)
	return n
}

func TestSend_HappyPath(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/me/messages", r.URL.Path)
		require.Equal(t, "Bearer FBTOKEN", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message_id":"mid.1"}`))
	}))
	defer srv.Close()

	p, err := New(Config{PageAccessToken: "FBTOKEN", BaseURL: srv.URL})
	require.NoError(t, err)
	require.NoError(t, p.Send(context.Background(), mkFBNotif(t)))
	recipient := captured["recipient"].(map[string]any)
	require.Equal(t, "987654321", recipient["id"])
	message := captured["message"].(map[string]any)
	require.Equal(t, "Hello from Messenger!", message["text"])
}

func TestSend_429IsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, _ := New(Config{PageAccessToken: "T", BaseURL: srv.URL})
	require.True(t, errors.Is(p.Send(context.Background(), mkFBNotif(t)), port.ErrTransient))
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestSend ./infrastructure/provider/fbmessenger/...
```
Expected: package not found

- [ ] **Step 3: Create the provider**

Create `infrastructure/provider/fbmessenger/fbmessenger.go`:

```go
// Package fbmessenger implements port.NotificationProvider using the Meta Graph API.
// Reference: https://developers.facebook.com/docs/messenger-platform/send-messages
package fbmessenger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
)

type Config struct {
	PageAccessToken string
	BaseURL         string // override for tests; defaults to "https://graph.facebook.com/v18.0"
	Client          *http.Client
	Timeout         time.Duration
}

type Provider struct {
	cfg    Config
	client *http.Client
}

func New(cfg Config) (*Provider, error) {
	if cfg.PageAccessToken == "" {
		return nil, fmt.Errorf("fbmessenger: PageAccessToken required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://graph.facebook.com/v18.0"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	c := cfg.Client
	if c == nil {
		c = &http.Client{Timeout: cfg.Timeout}
	}
	return &Provider{cfg: cfg, client: c}, nil
}

var _ port.NotificationProvider = (*Provider)(nil)

func (p *Provider) Send(ctx context.Context, n *domain.Notification) error {
	if n.Recipient.MessagingID == "" {
		return fmt.Errorf("fbmessenger: messaging_id empty")
	}
	payload, err := json.Marshal(map[string]any{
		"recipient": map[string]string{"id": n.Recipient.MessagingID},
		"message":   map[string]string{"text": n.Body},
	})
	if err != nil {
		return fmt.Errorf("fbmessenger: marshal payload: %w", err)
	}
	url := p.cfg.BaseURL + "/me/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("fbmessenger request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.PageAccessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", port.ErrTransient, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		return fmt.Errorf("%w: fbmessenger http %d", port.ErrTransient, resp.StatusCode)
	default:
		return fmt.Errorf("fbmessenger http %d (terminal)", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test -race -run TestSend ./infrastructure/provider/fbmessenger/...
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infrastructure/provider/fbmessenger/
git commit -m "feat: add Facebook Messenger provider skeleton"
```

---

## Task 21: Worker — wire social providers in buildProvider

**Files:**
- Modify: `cmd/worker/main.go`

- [ ] **Step 1: Add imports and extend buildProvider switch**

In `cmd/worker/main.go`, add the four new imports:

```go
"github.com/example/notification-engine/infrastructure/provider/fbmessenger"
"github.com/example/notification-engine/infrastructure/provider/line"
"github.com/example/notification-engine/infrastructure/provider/telegram"
"github.com/example/notification-engine/infrastructure/provider/whatsapp"
```

Extend the `switch` in `buildProvider` (add after the `ChannelEmail` case, before `default`):

```go
case domain.ChannelTelegram:
	if cfg.TelegramBotToken == "" {
		return nil, fmt.Errorf("telegram: TELEGRAM_BOT_TOKEN required for PROVIDER_MODE=real")
	}
	return telegram.New(telegram.Config{BotToken: cfg.TelegramBotToken})
case domain.ChannelWhatsApp:
	if cfg.WhatsAppPhoneNumberID == "" || cfg.WhatsAppAccessToken == "" {
		return nil, fmt.Errorf("whatsapp: WHATSAPP_PHONE_NUMBER_ID and WHATSAPP_ACCESS_TOKEN required for PROVIDER_MODE=real")
	}
	return whatsapp.New(whatsapp.Config{PhoneNumberID: cfg.WhatsAppPhoneNumberID, AccessToken: cfg.WhatsAppAccessToken})
case domain.ChannelLine:
	if cfg.LineChannelAccessToken == "" {
		return nil, fmt.Errorf("line: LINE_CHANNEL_ACCESS_TOKEN required for PROVIDER_MODE=real")
	}
	return line.New(line.Config{ChannelAccessToken: cfg.LineChannelAccessToken})
case domain.ChannelFacebookMessenger:
	if cfg.FBPageAccessToken == "" {
		return nil, fmt.Errorf("fbmessenger: FB_PAGE_ACCESS_TOKEN required for PROVIDER_MODE=real")
	}
	return fbmessenger.New(fbmessenger.Config{PageAccessToken: cfg.FBPageAccessToken})
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
go build ./cmd/worker/...
```
Expected: PASS

- [ ] **Step 3: Run all tests**

```bash
go test -race -count=1 ./...
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/worker/main.go
git commit -m "feat: wire social channel providers into worker buildProvider"
```

---

## Task 22: HTTP — UpdateTemplateRequest DTO + UpdateTemplate handler

**Files:**
- Create: `cmd/api/http/dto/update_template_request.go`
- Create: `cmd/api/http/handlers/update_template.go`
- Create: `cmd/api/http/handlers/update_template_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/api/http/handlers/update_template_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTemplate_HappyPath_200(t *testing.T) {
	id := uuid.New()
	expected := domain.Template{ID: id, Name: "new name", Channel: domain.ChannelSMS, Body: "New Body", OwnerUserID: 42, UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{
		Templates: &templateRepo{t: expected},
		Clock:     fixedClock{},
	}}
	body := `{"name":"new name","body":"New Body"}`
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(body)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Equal(t, "new name", got.Name)
}

func TestUpdateTemplate_InvalidID_400(t *testing.T) {
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/templates/bad", bytes.NewBufferString(`{}`)), "id", "bad")
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestUpdateTemplate_NoIdentity_401(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(`{}`)), "id", id.String())
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestUpdateTemplate_NotFound_404(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{
		Templates: &templateRepo{err: domain.ErrNotFound},
		Clock:     fixedClock{},
	}}
	body := `{"name":"n","body":"b"}`
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(body)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}

func TestUpdateTemplate_InvalidJSON_400(t *testing.T) {
	id := uuid.New()
	h := &Handler{UpdateTemplateSvc: &service.UpdateTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodPut, "/v1/templates/"+id.String(), bytes.NewBufferString(`{bad}`)), 42),
		"id", id.String(),
	)
	h.UpdateTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestUpdateTemplate ./cmd/api/http/handlers/...
```
Expected: compile errors (UpdateTemplateSvc undefined in Handler, UpdateTemplate method undefined)

- [ ] **Step 3: Create the DTO**

Create `cmd/api/http/dto/update_template_request.go`:

```go
package dto

type UpdateTemplateRequest struct {
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	MediaURLs []string `json:"media_urls,omitempty"`
}
```

- [ ] **Step 4: Create the handler**

Create `cmd/api/http/handlers/update_template.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UpdateTemplate handles PUT /v1/templates/{id}.
func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	var req dto.UpdateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	t, err := h.UpdateTemplateSvc.Execute(r.Context(), service.UpdateTemplateInput{
		ID: id, Name: req.Name, Subject: req.Subject, Body: req.Body,
		MediaURLs: req.MediaURLs, OwnerUserID: ownerID,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateToView(t))
}
```

- [ ] **Step 5: Add UpdateTemplateSvc to Handler struct**

In `cmd/api/http/handlers/handler.go`, add the field (see Task 26 for full Handler update — add it now to unblock compilation):

Add `UpdateTemplateSvc *service.UpdateTemplate` to the `Handler` struct.

- [ ] **Step 6: Run tests**

```bash
go test -race -run TestUpdateTemplate ./cmd/api/http/handlers/...
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/api/http/dto/update_template_request.go cmd/api/http/handlers/update_template.go cmd/api/http/handlers/update_template_test.go cmd/api/http/handlers/handler.go
git commit -m "feat: add UpdateTemplate handler"
```

---

## Task 23: HTTP — DeleteTemplate handler

**Files:**
- Create: `cmd/api/http/handlers/delete_template.go`
- Create: `cmd/api/http/handlers/delete_template_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/api/http/handlers/delete_template_test.go`:

```go
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDeleteTemplate_HappyPath_204(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{
		Templates: &templateRepo{t: domain.Template{ID: id, OwnerUserID: 42}},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), 42),
		"id", id.String(),
	)
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteTemplate_InvalidID_400(t *testing.T) {
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/templates/bad", nil), "id", "bad")
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestDeleteTemplate_NoIdentity_401(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), "id", id.String())
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestDeleteTemplate_NotFound_404(t *testing.T) {
	id := uuid.New()
	h := &Handler{DeleteTemplateSvc: &service.DeleteTemplate{
		Templates: &templateRepo{err: domain.ErrNotFound},
	}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/templates/"+id.String(), nil), 42),
		"id", id.String(),
	)
	h.DeleteTemplate(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestDeleteTemplate ./cmd/api/http/handlers/...
```
Expected: compile error (DeleteTemplateSvc undefined)

- [ ] **Step 3: Create the handler**

Create `cmd/api/http/handlers/delete_template.go`:

```go
package handlers

import (
	"net/http"

	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DeleteTemplate handles DELETE /v1/templates/{id}.
func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if err := h.DeleteTemplateSvc.Execute(r.Context(), service.DeleteTemplateInput{
		ID: id, OwnerUserID: ownerID,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Add DeleteTemplateSvc to Handler struct**

Add `DeleteTemplateSvc *service.DeleteTemplate` to `Handler` in `cmd/api/http/handlers/handler.go`.

- [ ] **Step 5: Run tests**

```bash
go test -race -run TestDeleteTemplate ./cmd/api/http/handlers/...
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/api/http/handlers/delete_template.go cmd/api/http/handlers/delete_template_test.go cmd/api/http/handlers/handler.go
git commit -m "feat: add DeleteTemplate handler"
```

---

## Task 24: HTTP — ListTemplates handler

**Files:**
- Create: `cmd/api/http/handlers/list_templates.go`
- Create: `cmd/api/http/handlers/list_templates_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/api/http/handlers/list_templates_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTemplates_HappyPath_200(t *testing.T) {
	tpl := domain.Template{ID: uuid.New(), Name: "welcome", Channel: domain.ChannelSMS, Body: "Body", OwnerUserID: 42}
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{t: tpl}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/templates", nil), 42)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string][]dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Contains(t, got, "sms")
	assert.Len(t, got["sms"], 1)
	assert.Equal(t, "welcome", got["sms"][0].Name)
}

func TestListTemplates_FilterByChannel_200(t *testing.T) {
	tpl := domain.Template{ID: uuid.New(), Name: "welcome", Channel: domain.ChannelSMS, Body: "Body", OwnerUserID: 42}
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{t: tpl}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodGet, "/v1/templates?channel=sms", nil), 42,
	)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestListTemplates_InvalidChannel_400(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(
		httptest.NewRequest(http.MethodGet, "/v1/templates?channel=fax", nil), 42,
	)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestListTemplates_NoIdentity_401(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestListTemplates_Empty_200(t *testing.T) {
	h := &Handler{ListTemplatesSvc: &service.ListTemplates{Templates: &templateRepo{}}}
	w := httptest.NewRecorder()
	r := withServiceIdentity(httptest.NewRequest(http.MethodGet, "/v1/templates", nil), 42)
	h.ListTemplates(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	var got map[string][]dto.TemplateView
	require.NoError(t, json.NewDecoder(w.Body).Decode(&got))
	assert.Empty(t, got)
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestListTemplates ./cmd/api/http/handlers/...
```
Expected: compile error

- [ ] **Step 3: Create the handler**

Create `cmd/api/http/handlers/list_templates.go`:

```go
package handlers

import (
	"net/http"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
)

// ListTemplates handles GET /v1/templates.
// Optional query parameter: channel=<channel>
// Response: map[channel][]TemplateView
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	ownerID, err := mw.RequireServiceIdentity(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	var channel *domain.Channel
	if raw := r.URL.Query().Get("channel"); raw != "" {
		ch, err := domain.ParseChannel(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
			return
		}
		channel = &ch
	}
	templates, err := h.ListTemplatesSvc.Execute(r.Context(), service.ListTemplatesInput{
		OwnerUserID: ownerID, Channel: channel,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	grouped := make(map[string][]dto.TemplateView)
	for _, t := range templates {
		grouped[string(t.Channel)] = append(grouped[string(t.Channel)], templateToView(t))
	}
	writeJSON(w, http.StatusOK, grouped)
}
```

- [ ] **Step 4: Add ListTemplatesSvc to Handler struct**

Add `ListTemplatesSvc *service.ListTemplates` to `Handler` in `cmd/api/http/handlers/handler.go`.

- [ ] **Step 5: Run tests**

```bash
go test -race -run TestListTemplates ./cmd/api/http/handlers/...
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/api/http/handlers/list_templates.go cmd/api/http/handlers/list_templates_test.go cmd/api/http/handlers/handler.go
git commit -m "feat: add ListTemplates handler"
```

---

## Task 25: HTTP — DeleteDevice handler

**Files:**
- Create: `cmd/api/http/handlers/delete_device.go`
- Create: `cmd/api/http/handlers/delete_device_test.go`

- [ ] **Step 1: Write the failing tests**

Create `cmd/api/http/handlers/delete_device_test.go`:

```go
package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	"github.com/stretchr/testify/assert"
)

func TestDeleteDevice_HappyPath_204(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{Users: &userRepo{}}}
	w := httptest.NewRecorder()
	body := `{"channel":"push_ios","device_token":"tok"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestDeleteDevice_InvalidID_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/users/bad/devices", nil), "id", "bad")
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_id")
}

func TestDeleteDevice_NoIdentity_401(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", nil), "id", "42")
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assertErrorCode(t, w, "unauthorized")
}

func TestDeleteDevice_InvalidJSON_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(`{bad}`)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_json")
}

func TestDeleteDevice_InvalidChannel_400(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{}}
	w := httptest.NewRecorder()
	body := `{"channel":"fax","device_token":"tok"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assertErrorCode(t, w, "invalid_channel")
}

func TestDeleteDevice_NotFound_404(t *testing.T) {
	h := &Handler{DeleteDeviceSvc: &service.DeleteDevice{Users: &userRepo{err: domain.ErrNotFound}}}
	w := httptest.NewRecorder()
	body := `{"channel":"push_ios","device_token":"unknown"}`
	r := withURLParam(
		withServiceIdentity(httptest.NewRequest(http.MethodDelete, "/v1/users/42/devices", bytes.NewBufferString(body)), 42),
		"id", "42",
	)
	h.DeleteDevice(w, r)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "not_found")
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test -race -run TestDeleteDevice ./cmd/api/http/handlers/...
```
Expected: compile error

- [ ] **Step 3: Create the handler**

Create `cmd/api/http/handlers/delete_device.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/example/notification-engine/cmd/api/http/dto"
	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/service"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
)

// DeleteDevice handles DELETE /v1/users/{id}/devices.
func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}
	if err := mw.RequireUserOwnership(r.Context(), uid); err != nil {
		mapDomainError(w, err)
		return
	}
	var req dto.DeviceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	channel, err := domain.ParseChannel(req.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_channel", err.Error())
		return
	}
	if err := h.DeleteDeviceSvc.Execute(r.Context(), service.DeleteDeviceInput{
		UserID: uid, Channel: channel, DeviceToken: domain.DeviceToken(req.DeviceToken),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Add DeleteDeviceSvc to Handler struct**

Add `DeleteDeviceSvc *service.DeleteDevice` to `Handler` in `cmd/api/http/handlers/handler.go`.

- [ ] **Step 5: Run tests**

```bash
go test -race -run TestDeleteDevice ./cmd/api/http/handlers/...
```
Expected: PASS

- [ ] **Step 6: Run full unit test suite**

```bash
go test -race -count=1 ./...
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/api/http/handlers/delete_device.go cmd/api/http/handlers/delete_device_test.go cmd/api/http/handlers/handler.go
git commit -m "feat: add DeleteDevice handler"
```

---

## Task 26: Wire — final Handler struct, routes, and main.go

**Files:**
- Modify: `cmd/api/http/handlers/handler.go` (finalize all Svc fields)
- Modify: `cmd/api/http/router.go` (add 4 new routes)
- Modify: `cmd/api/main.go` (instantiate and wire 4 new services)

- [ ] **Step 1: Verify Handler struct has all fields**

After Tasks 22–25, `handler.go` should have these fields. Confirm it looks like this:

```go
type Handler struct {
	SubmitSvc          *service.SubmitNotification
	GetSvc             *service.GetNotification
	CreateTemplateSvc  *service.CreateTemplate
	GetTemplateSvc     *service.GetTemplate
	UpdateTemplateSvc  *service.UpdateTemplate
	DeleteTemplateSvc  *service.DeleteTemplate
	ListTemplatesSvc   *service.ListTemplates
	UpdateSettingSvc   *service.UpdateSetting
	RegisterDeviceSvc  *service.RegisterDevice
	DeleteDeviceSvc    *service.DeleteDevice
}
```

- [ ] **Step 2: Add 4 new routes to router.go**

In `cmd/api/http/router.go`, inside the `r.Route("/v1", ...)` block, add:

```go
r.Put("/templates/{id}", h.UpdateTemplate)
r.Delete("/templates/{id}", h.DeleteTemplate)
r.Get("/templates", h.ListTemplates)
r.Delete("/users/{id}/devices", h.DeleteDevice)
```

The full `/v1` block becomes:

```go
r.Route("/v1", func(r chi.Router) {
    r.Post("/notifications", h.SubmitNotification)
    r.Get("/notifications/{id}", h.GetNotification)
    r.Post("/templates", h.CreateTemplate)
    r.Get("/templates/{id}", h.GetTemplate)
    r.Put("/templates/{id}", h.UpdateTemplate)
    r.Delete("/templates/{id}", h.DeleteTemplate)
    r.Get("/templates", h.ListTemplates)
    r.Put("/users/{id}/settings", h.UpdateSetting)
    r.Post("/users/{id}/devices", h.RegisterDevice)
    r.Delete("/users/{id}/devices", h.DeleteDevice)
})
```

- [ ] **Step 3: Wire the 4 new services in cmd/api/main.go**

After the existing `registerDevice := ...` line in `run()`, add:

```go
updateTemplate := &service.UpdateTemplate{Templates: templatesRepo, Clock: clock}
deleteTemplate := &service.DeleteTemplate{Templates: templatesRepo}
listTemplates := &service.ListTemplates{Templates: templatesRepo}
deleteDevice := &service.DeleteDevice{Users: usersRepo}
```

Update the `&handlers.Handler{...}` literal to include the new fields:

```go
h := &handlers.Handler{
    SubmitSvc:         submit,
    GetSvc:            get,
    CreateTemplateSvc: createTemplate,
    GetTemplateSvc:    getTemplate,
    UpdateTemplateSvc: updateTemplate,
    DeleteTemplateSvc: deleteTemplate,
    ListTemplatesSvc:  listTemplates,
    UpdateSettingSvc:  updateSetting,
    RegisterDeviceSvc: registerDevice,
    DeleteDeviceSvc:   deleteDevice,
}
```

- [ ] **Step 4: Build everything**

```bash
go build ./...
```
Expected: PASS

- [ ] **Step 5: Run full test suite**

```bash
go test -race -count=1 ./...
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/api/http/router.go cmd/api/main.go cmd/api/http/handlers/handler.go
git commit -m "feat: wire new services and routes into API composition root"
```

---

## Task 27: Integration tests

**Files:**
- Modify: `test/integration/api_test.go`

- [ ] **Step 1: Add 3 new integration test scenarios**

Append to `test/integration/api_test.go`:

```go
func TestUpdateTemplate_HappyPath_200(t *testing.T) {
	// Create a template first.
	name := "it-upd-" + uuid.NewString()
	createBody := []byte(fmt.Sprintf(`{"name":"%s","channel":"sms","body":"Original body.","version":1}`, name))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/templates", "42", createBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))

	// Update it.
	updateBody := []byte(`{"name":"updated-name","body":"Updated body."}`)
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "PUT", "/v1/templates/"+created.ID, "42", updateBody))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var updated struct {
		Name      string `json:"name"`
		Body      string `json:"body"`
		UpdatedAt string `json:"updated_at"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&updated))
	require.Equal(t, "updated-name", updated.Name)
	require.Equal(t, "Updated body.", updated.Body)
}

func TestListTemplates_GroupedByChannel_200(t *testing.T) {
	ownerID := "43"
	suffix := uuid.NewString()

	// Create 2 SMS and 1 email template under the same owner.
	for i, ch := range []string{"sms", "sms", "email"} {
		body := []byte(fmt.Sprintf(`{"name":"it-list-%d-%s","channel":"%s","body":"Body.","version":1}`, i, suffix, ch))
		resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/templates", ownerID, body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	// List all.
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "GET", "/v1/templates", ownerID, nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var grouped map[string][]struct{ Name string `json:"name"` }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&grouped))
	require.GreaterOrEqual(t, len(grouped["sms"]), 2)
	require.GreaterOrEqual(t, len(grouped["email"]), 1)

	// Filter by channel.
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "GET", "/v1/templates?channel=sms", ownerID, nil))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var smsOnly map[string][]struct{ Name string `json:"name"` }
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&smsOnly))
	require.NotContains(t, smsOnly, "email")
}

func TestDeleteDevice_HappyPath_204(t *testing.T) {
	const token = "integration-delete-test-token"

	// Register the device.
	regBody := []byte(fmt.Sprintf(`{"device_token":"%s","channel":"push_ios"}`, token))
	resp, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "POST", "/v1/users/42/devices", "42", regBody))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Delete it.
	delBody := []byte(fmt.Sprintf(`{"device_token":"%s","channel":"push_ios"}`, token))
	resp2, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "DELETE", "/v1/users/42/devices", "42", delBody))
	require.NoError(t, err)
	resp2.Body.Close()
	require.Equal(t, http.StatusNoContent, resp2.StatusCode)

	// Second delete must return 404.
	resp3, err := http.DefaultClient.Do(signedRequestOnBehalf(t, "DELETE", "/v1/users/42/devices", "42", delBody))
	require.NoError(t, err)
	defer resp3.Body.Close()
	require.Equal(t, http.StatusNotFound, resp3.StatusCode)
}
```

- [ ] **Step 2: Run the integration suite against a live stack**

```bash
make up
# wait for the stack to be healthy, then:
go test -race -count=1 -tags=integration ./test/integration/...
```
Expected: all integration tests PASS

- [ ] **Step 3: Commit**

```bash
git add test/integration/api_test.go
git commit -m "test: add integration tests for template update/list and device delete"
```

---

## Final verification

- [ ] **Run the full test suite one last time**

```bash
go test -race -count=1 ./...
```
Expected: PASS, no race conditions reported.

- [ ] **Build all binaries**

```bash
go build ./...
```
Expected: PASS.
