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
