// Package rendering implements port.TemplateRenderer using Go's stdlib
// text/template (and html/template for email, where untrusted variables need
// auto-escaping). Compiled templates are cached in-process with a per-entry
// TTL so updates that re-use the same template id propagate without a
// restart.
package rendering

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"io"
	"sync"
	texttemplate "text/template"
	"time"

	"github.com/example/notification-engine/internal/port"
	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
)

// DefaultTTL is the time a compiled template may live in-process before being
// re-fetched from the repository. Override via NewWithTTL.
const DefaultTTL = 5 * time.Minute

type executor interface {
	Execute(wr io.Writer, data any) error
}

type cached struct {
	subject   executor
	body      executor
	version   int
	expiresAt time.Time
}

// Renderer caches compiled templates keyed by template id.
type Renderer struct {
	repo  port.TemplateRepository
	mu    sync.RWMutex
	cache map[uuid.UUID]cached
	ttl   time.Duration
	now   func() time.Time
}

func New(repo port.TemplateRepository) *Renderer {
	return NewWithTTL(repo, DefaultTTL)
}

// NewWithTTL is exposed mainly for tests that want a deterministic TTL.
func NewWithTTL(repo port.TemplateRepository, ttl time.Duration) *Renderer {
	return &Renderer{
		repo:  repo,
		cache: map[uuid.UUID]cached{},
		ttl:   ttl,
		now:   time.Now,
	}
}

var _ port.TemplateRenderer = (*Renderer)(nil)

func (r *Renderer) Render(ctx context.Context, id uuid.UUID, vars map[string]string) (string, string, error) {
	tpl, err := r.lookup(ctx, id)
	if err != nil {
		return "", "", err
	}
	subj, err := exec(tpl.subject, vars)
	if err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}
	body, err := exec(tpl.body, vars)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return subj, body, nil
}

func (r *Renderer) lookup(ctx context.Context, id uuid.UUID) (cached, error) {
	r.mu.RLock()
	t, ok := r.cache[id]
	r.mu.RUnlock()
	if ok && r.now().Before(t.expiresAt) {
		return t, nil
	}
	dbt, err := r.repo.Get(ctx, id)
	if err != nil {
		return cached{}, err
	}
	c, err := compile(dbt)
	if err != nil {
		return cached{}, err
	}
	c.expiresAt = r.now().Add(r.ttl)
	r.mu.Lock()
	r.cache[id] = c
	r.mu.Unlock()
	return c, nil
}

func compile(t domain.Template) (cached, error) {
	if t.Channel == domain.ChannelEmail {
		subj, err := htmltemplate.New("subject").Parse(t.Subject)
		if err != nil {
			return cached{}, err
		}
		body, err := htmltemplate.New("body").Parse(t.Body)
		if err != nil {
			return cached{}, err
		}
		return cached{subject: subj, body: body, version: t.Version}, nil
	}
	subj, err := texttemplate.New("subject").Parse(t.Subject)
	if err != nil {
		return cached{}, err
	}
	body, err := texttemplate.New("body").Parse(t.Body)
	if err != nil {
		return cached{}, err
	}
	return cached{subject: subj, body: body, version: t.Version}, nil
}

func exec(e executor, vars map[string]string) (string, error) {
	if e == nil {
		return "", nil
	}
	var buf bytes.Buffer
	if err := e.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
