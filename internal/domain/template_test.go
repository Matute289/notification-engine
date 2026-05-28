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
