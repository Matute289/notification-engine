package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Template is a reusable, versioned message body.
type Template struct {
	ID        uuid.UUID
	Name      string
	Channel   Channel
	Locale    string
	Subject   string
	Body      string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTemplate builds a Template, defaulting locale and version when zero.
func NewTemplate(id uuid.UUID, name string, ch Channel, locale, subject, body string, version int, now time.Time) (Template, error) {
	if name == "" {
		return Template{}, fmt.Errorf("%w: template name required", ErrInvalidInput)
	}
	if !ch.Valid() {
		return Template{}, fmt.Errorf("%w: invalid channel %q", ErrInvalidInput, ch)
	}
	if body == "" {
		return Template{}, fmt.Errorf("%w: template body required", ErrInvalidInput)
	}
	if locale == "" {
		locale = "en"
	}
	if version == 0 {
		version = 1
	}
	return Template{
		ID:        id,
		Name:      name,
		Channel:   ch,
		Locale:    locale,
		Subject:   subject,
		Body:      body,
		Version:   version,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
