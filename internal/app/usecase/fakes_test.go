package usecase

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/example/notification-engine/internal/app/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// In-memory fakes used by the use case tests. They implement the application
// ports so use cases can be exercised without any infrastructure.

type fakeNotifications struct {
	mu        sync.Mutex
	byID      map[uuid.UUID]*domain.Notification
	byEvent   map[string]*domain.Notification
	outbox    []fakeOutboxRow
	createErr error
}

type fakeOutboxRow struct {
	notificationID uuid.UUID
	channel        domain.Channel
	payload        []byte
}

func newFakeNotifications() *fakeNotifications {
	return &fakeNotifications{
		byID:    map[uuid.UUID]*domain.Notification{},
		byEvent: map[string]*domain.Notification{},
	}
}

func (f *fakeNotifications) Create(_ context.Context, n *domain.Notification) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byEvent[string(n.EventID)]; exists {
		return domain.ErrAlreadyExists
	}
	cp := *n
	f.byID[n.ID] = &cp
	f.byEvent[string(n.EventID)] = &cp
	return nil
}

func (f *fakeNotifications) UpdateStatus(_ context.Context, id uuid.UUID, s domain.Status, attempt int, lastErr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	n.Status = s
	n.Attempt = attempt
	n.LastError = lastErr
	return nil
}

func (f *fakeNotifications) Get(_ context.Context, id uuid.UUID) (*domain.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (f *fakeNotifications) GetByEventID(_ context.Context, eventID domain.EventID) (*domain.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.byEvent[string(eventID)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (f *fakeNotifications) RecordEvent(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}

func (f *fakeNotifications) ListStuckInFlight(_ context.Context, threshold time.Duration, limit int) ([]*domain.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := time.Now().Add(-threshold)
	var out []*domain.Notification
	for _, n := range f.byID {
		if n.Status == domain.StatusInFlight && n.UpdatedAt.Before(cutoff) {
			cp := *n
			out = append(out, &cp)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeNotifications) SubmitWithOutbox(_ context.Context, n *domain.Notification, payload []byte) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byEvent[string(n.EventID)]; exists {
		return domain.ErrAlreadyExists
	}
	cp := *n
	f.byID[n.ID] = &cp
	f.byEvent[string(n.EventID)] = &cp
	f.outbox = append(f.outbox, fakeOutboxRow{
		notificationID: n.ID, channel: n.Channel, payload: payload,
	})
	return nil
}

func (f *fakeNotifications) Claim(_ context.Context, limit int) ([]port.OutboxItem, port.OutboxTx, error) {
	f.mu.Lock()
	rows := f.outbox
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	f.outbox = f.outbox[len(rows):]
	f.mu.Unlock()

	items := make([]port.OutboxItem, 0, len(rows))
	for i, r := range rows {
		items = append(items, port.OutboxItem{
			ID: int64(i + 1), NotificationID: r.notificationID.String(),
			Channel: r.channel, Payload: r.payload,
		})
	}
	return items, &fakeOutboxTx{}, nil
}

type fakeOutboxTx struct{}

func (fakeOutboxTx) MarkPublished(context.Context, int64) error          { return nil }
func (fakeOutboxTx) MarkFailed(context.Context, int64, int, string) error { return nil }
func (fakeOutboxTx) Commit(context.Context) error                        { return nil }
func (fakeOutboxTx) Rollback(context.Context) error                      { return nil }

type fakeUsers struct {
	users    map[int64]domain.User
	devices  map[int64]map[domain.Channel][]domain.Device
	settings map[int64]map[domain.Channel]domain.Setting
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{
		users:    map[int64]domain.User{},
		devices:  map[int64]map[domain.Channel][]domain.Device{},
		settings: map[int64]map[domain.Channel]domain.Setting{},
	}
}

func (f *fakeUsers) GetUser(_ context.Context, id int64) (domain.User, error) {
	u, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeUsers) DevicesForUser(_ context.Context, userID int64, ch domain.Channel) ([]domain.Device, error) {
	return f.devices[userID][ch], nil
}

func (f *fakeUsers) UpsertDevice(_ context.Context, d domain.Device) error {
	if f.devices[d.UserID] == nil {
		f.devices[d.UserID] = map[domain.Channel][]domain.Device{}
	}
	f.devices[d.UserID][d.Channel] = append(f.devices[d.UserID][d.Channel], d)
	return nil
}

func (f *fakeUsers) GetSetting(_ context.Context, userID int64, ch domain.Channel) (domain.Setting, error) {
	if s, ok := f.settings[userID][ch]; ok {
		return s, nil
	}
	return domain.DefaultSetting(userID, ch), nil
}

func (f *fakeUsers) UpsertSetting(_ context.Context, s domain.Setting) error {
	if f.settings[s.UserID] == nil {
		f.settings[s.UserID] = map[domain.Channel]domain.Setting{}
	}
	f.settings[s.UserID][s.Channel] = s
	return nil
}

type fakeTemplates struct{ tpls map[uuid.UUID]domain.Template }

func newFakeTemplates() *fakeTemplates {
	return &fakeTemplates{tpls: map[uuid.UUID]domain.Template{}}
}

func (f *fakeTemplates) Create(_ context.Context, t domain.Template) error {
	f.tpls[t.ID] = t
	return nil
}

func (f *fakeTemplates) Get(_ context.Context, id uuid.UUID) (domain.Template, error) {
	t, ok := f.tpls[id]
	if !ok {
		return domain.Template{}, domain.ErrNotFound
	}
	return t, nil
}

type fakeRenderer struct{ subject, body string }

func (f *fakeRenderer) Render(_ context.Context, _ uuid.UUID, _ map[string]string) (string, string, error) {
	return f.subject, f.body, nil
}

type fakeLimiter struct {
	allowed bool
	calls   int
}

func (f *fakeLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	f.calls++
	return f.allowed, nil
}

type fakeDeduper struct {
	claimed map[string]bool
}

func newFakeDeduper() *fakeDeduper { return &fakeDeduper{claimed: map[string]bool{}} }

func (f *fakeDeduper) Claim(_ context.Context, eventID string, _ time.Duration) (bool, error) {
	if f.claimed[eventID] {
		return false, nil
	}
	f.claimed[eventID] = true
	return true, nil
}

type fakePublisher struct {
	published []*domain.Notification
	retries   []retryCall
	publishErr error
}

type retryCall struct {
	channel  domain.Channel
	attempt  int
	max      int
	body     []byte
}

func (f *fakePublisher) Publish(_ context.Context, n *domain.Notification) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, n)
	return nil
}

func (f *fakePublisher) Retry(_ context.Context, ch domain.Channel, body []byte, attempt, max int) (bool, error) {
	f.retries = append(f.retries, retryCall{ch, attempt, max, body})
	return attempt >= max, nil
}

func (f *fakePublisher) PublishRaw(_ context.Context, ch domain.Channel, body []byte, attempt int) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, &domain.Notification{Channel: ch, Attempt: attempt, Body: string(body)})
	return nil
}

func (f *fakePublisher) Encode(n *domain.Notification) ([]byte, error) {
	// Trivial deterministic encoding for tests.
	return []byte("payload:" + string(n.EventID)), nil
}

type fakeProvider struct {
	err   error
	calls int
}

func (f *fakeProvider) Send(_ context.Context, _ *domain.Notification) error {
	f.calls++
	return f.err
}

type fakeMetrics struct {
	accepted, sent, failed, dead map[string]int
	durations                    []obs
}

type obs struct {
	channel string
	outcome string
	seconds float64
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{
		accepted: map[string]int{},
		sent:     map[string]int{},
		failed:   map[string]int{},
		dead:     map[string]int{},
	}
}

func (m *fakeMetrics) NotificationAccepted(c string)               { m.accepted[c]++ }
func (m *fakeMetrics) NotificationSent(c string)                   { m.sent[c]++ }
func (m *fakeMetrics) NotificationFailed(c string)                 { m.failed[c]++ }
func (m *fakeMetrics) NotificationDeadLettered(c string)           { m.dead[c]++ }
func (m *fakeMetrics) ObserveWorkerDuration(c, o string, s float64) {
	m.durations = append(m.durations, obs{c, o, s})
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

// Compile-time assertions that the fakes satisfy the ports.
var (
	_ port.NotificationRepository   = (*fakeNotifications)(nil)
	_ port.TxNotificationRepository = (*fakeNotifications)(nil)
	_ port.OutboxRepository         = (*fakeNotifications)(nil)
	_ port.UserRepository           = (*fakeUsers)(nil)
	_ port.TemplateRepository       = (*fakeTemplates)(nil)
	_ port.TemplateRenderer         = (*fakeRenderer)(nil)
	_ port.RateLimiter              = (*fakeLimiter)(nil)
	_ port.Deduper                  = (*fakeDeduper)(nil)
	_ port.EventPublisher           = (*fakePublisher)(nil)
	_ port.NotificationProvider     = (*fakeProvider)(nil)
	_ port.MetricsRecorder          = (*fakeMetrics)(nil)
	_ port.Clock                    = fixedClock{}
)

// errBoom is a generic non-domain error used to drive failure paths.
var errBoom = errors.New("boom")
