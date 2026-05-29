package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplate_InvalidOwnerUserID(t *testing.T) {
	_, err := NewTemplate(
		uuid.New(), "welcome", ChannelSMS, "en", "", "Hello!", nil, 1, 0, time.Now(),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidInput), "expected ErrInvalidInput, got: %v", err)
}

func TestNewTemplate_ValidOwnerUserID(t *testing.T) {
	tpl, err := NewTemplate(
		uuid.New(), "welcome", ChannelSMS, "en", "", "Hello!", nil, 1, 1, time.Now(),
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), tpl.OwnerUserID)
	assert.Equal(t, "welcome", tpl.Name)
}

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
