package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/example/notification-engine/internal/port"
	mw "github.com/example/notification-engine/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// notifRepo is a configurable fake for port.TxNotificationRepository.
type notifRepo struct {
	submitErr      error
	getResult      *domain.Notification
	getErr         error
	getByEvtResult *domain.Notification
	getByEvtErr    error
}

func (r *notifRepo) Create(_ context.Context, _ *domain.Notification) error { return r.submitErr }
func (r *notifRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ domain.Status, _ int, _ string) error {
	return nil
}
func (r *notifRepo) Get(_ context.Context, _ uuid.UUID) (*domain.Notification, error) {
	return r.getResult, r.getErr
}
func (r *notifRepo) GetByEventID(_ context.Context, _ domain.EventID) (*domain.Notification, error) {
	return r.getByEvtResult, r.getByEvtErr
}
func (r *notifRepo) RecordEvent(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}
func (r *notifRepo) ListStuckInFlight(_ context.Context, _ time.Duration, _ int) ([]*domain.Notification, error) {
	return nil, nil
}
func (r *notifRepo) SubmitWithOutbox(_ context.Context, _ *domain.Notification, _ []byte) error {
	return r.submitErr
}

var (
	_ port.NotificationRepository   = (*notifRepo)(nil)
	_ port.TxNotificationRepository = (*notifRepo)(nil)
)

// userRepo is a configurable fake for port.UserRepository.
type userRepo struct {
	setting domain.Setting
	err     error
}

func (r *userRepo) GetUser(_ context.Context, _ int64) (domain.User, error) {
	return domain.User{}, r.err
}
func (r *userRepo) DevicesForUser(_ context.Context, _ int64, _ domain.Channel) ([]domain.Device, error) {
	return nil, r.err
}
func (r *userRepo) UpsertDevice(_ context.Context, _ domain.Device) error { return r.err }
func (r *userRepo) GetSetting(_ context.Context, _ int64, _ domain.Channel) (domain.Setting, error) {
	if r.err != nil {
		return domain.Setting{}, r.err
	}
	return r.setting, nil
}
func (r *userRepo) UpsertSetting(_ context.Context, _ domain.Setting) error { return r.err }
func (r *userRepo) DeleteDevice(_ context.Context, _ int64, _ domain.Channel, _ domain.DeviceToken) error {
	return r.err
}

var _ port.UserRepository = (*userRepo)(nil)

// templateRepo is a configurable fake for port.TemplateRepository.
type templateRepo struct {
	t   domain.Template
	err error
}

func (r *templateRepo) Create(_ context.Context, _ domain.Template) error { return r.err }
func (r *templateRepo) Get(_ context.Context, _ uuid.UUID) (domain.Template, error) {
	return r.t, r.err
}
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

var _ port.TemplateRepository = (*templateRepo)(nil)

// nopRenderer always returns empty strings without error.
type nopRenderer struct{}

func (nopRenderer) Render(_ context.Context, _ uuid.UUID, _ map[string]string) (string, string, error) {
	return "", "", nil
}

var _ port.TemplateRenderer = nopRenderer{}

// nopPublisher succeeds and encodes trivially.
type nopPublisher struct{}

func (nopPublisher) Publish(_ context.Context, _ *domain.Notification) error { return nil }
func (nopPublisher) Retry(_ context.Context, _ domain.Channel, _ []byte, _, _ int) (bool, error) {
	return false, nil
}
func (nopPublisher) PublishRaw(_ context.Context, _ domain.Channel, _ []byte, _ int) error {
	return nil
}
func (nopPublisher) Encode(_ *domain.Notification) ([]byte, error) { return []byte("p"), nil }

var _ port.EventPublisher = nopPublisher{}

// limiter is a configurable fake for port.RateLimiter.
type limiter struct{ allowed bool }

func (l *limiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	return l.allowed, nil
}

var _ port.RateLimiter = (*limiter)(nil)

// deduper is a configurable fake for port.Deduper.
// claimed=true means the event is fresh (not seen before).
// claimed=false means it was already claimed (duplicate).
type deduper struct{ claimed bool }

func (d *deduper) Claim(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return d.claimed, nil
}

var _ port.Deduper = (*deduper)(nil)

// nopMetrics drops all metric calls.
type nopMetrics struct{}

func (nopMetrics) NotificationAccepted(_ string)                    {}
func (nopMetrics) NotificationSent(_ string)                        {}
func (nopMetrics) NotificationFailed(_ string)                      {}
func (nopMetrics) NotificationDeadLettered(_ string)                {}
func (nopMetrics) ObserveWorkerDuration(_, _ string, _ float64)     {}

var _ port.MetricsRecorder = nopMetrics{}

// fixedClock returns a deterministic time.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

var _ port.Clock = fixedClock{}

// withURLParam injects a chi URL parameter into the request context.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withServiceIdentity injects a service identity with the given onBehalfOfUserID
// into the request context. Pass onBehalfOfUserID=0 to leave OnBehalfOfUserID nil.
func withServiceIdentity(r *http.Request, onBehalfOfUserID int64) *http.Request {
	id := mw.Identity{Subject: "test-app", Kind: "service"}
	if onBehalfOfUserID != 0 {
		id.OnBehalfOfUserID = &onBehalfOfUserID
	}
	return r.WithContext(mw.ContextWithIdentity(r.Context(), id))
}
