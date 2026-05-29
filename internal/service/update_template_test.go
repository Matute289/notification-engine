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
