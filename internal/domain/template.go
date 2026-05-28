package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Template is a reusable, versioned message body.
// MediaURLs holds optional URLs to images or other attachments that providers
// can include when delivering the notification (e.g. MMS, rich push).
type Template struct {
	ID          uuid.UUID
	Name        string
	Channel     Channel
	Locale      string
	Subject     string
	Body        string
	MediaURLs   []string
	Version     int
	OwnerUserID int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// NewTemplate builds a Template, defaulting locale and version when zero.
// ownerUserID must be > 0.
func NewTemplate(id uuid.UUID, name string, ch Channel, locale, subject, body string, mediaURLs []string, version int, ownerUserID int64, now time.Time) (Template, error) {
	if name == "" {
		return Template{}, fmt.Errorf("%w: template name required", ErrInvalidInput)
	}
	if !ch.Valid() {
		return Template{}, fmt.Errorf("%w: invalid channel %q", ErrInvalidInput, ch)
	}
	if body == "" {
		return Template{}, fmt.Errorf("%w: template body required", ErrInvalidInput)
	}
	if ownerUserID <= 0 {
		return Template{}, fmt.Errorf("%w: owner_user_id must be > 0", ErrInvalidInput)
	}
	if locale == "" {
		locale = "en"
	}
	if version == 0 {
		version = 1
	}
	return Template{
		ID:          id,
		Name:        name,
		Channel:     ch,
		Locale:      locale,
		Subject:     subject,
		Body:        body,
		MediaURLs:   mediaURLs,
		Version:     version,
		OwnerUserID: ownerUserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
