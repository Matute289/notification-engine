package rendering

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type countingRepo struct {
	calls int32
	tpl   domain.Template
}

func (c *countingRepo) Create(_ context.Context, _ domain.Template) error { return nil }
func (c *countingRepo) Get(_ context.Context, _ uuid.UUID) (domain.Template, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.tpl, nil
}
func (c *countingRepo) Update(_ context.Context, _ domain.Template) error { return nil }
func (c *countingRepo) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (c *countingRepo) List(_ context.Context, _ int64, _ *domain.Channel) ([]domain.Template, error) {
	return nil, nil
}

func newRepo(body string) *countingRepo {
	return &countingRepo{
		tpl: domain.Template{
			ID:      uuid.New(),
			Channel: domain.ChannelSMS,
			Subject: "",
			Body:    body,
			Version: 1,
		},
	}
}

func TestRenderer_CachesWithinTTL(t *testing.T) {
	repo := newRepo("hi {{.Name}}")
	r := NewWithTTL(repo, time.Hour)

	for i := 0; i < 5; i++ {
		_, body, err := r.Render(context.Background(), repo.tpl.ID, map[string]string{"Name": "x"})
		require.NoError(t, err)
		require.Equal(t, "hi x", body)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls), "should cache compiled template")
}

func TestRenderer_RefetchesAfterTTL(t *testing.T) {
	repo := newRepo("v1 {{.Name}}")
	r := NewWithTTL(repo, time.Minute)
	now := time.Now()
	r.now = func() time.Time { return now }

	_, _, err := r.Render(context.Background(), repo.tpl.ID, map[string]string{"Name": "x"})
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&repo.calls))

	// Update the underlying template and advance time past the TTL.
	repo.tpl.Body = "v2 {{.Name}}"
	now = now.Add(2 * time.Minute)

	_, body, err := r.Render(context.Background(), repo.tpl.ID, map[string]string{"Name": "y"})
	require.NoError(t, err)
	require.Equal(t, "v2 y", body)
	require.Equal(t, int32(2), atomic.LoadInt32(&repo.calls), "TTL expiry should trigger re-fetch")
}
