package service

import (
	"context"
	"testing"
	"time"

	"github.com/example/notification-engine/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTemplates_AllChannels(t *testing.T) {
	repo := newFakeTemplates()
	sms, _ := domain.NewTemplate(uuid.New(), "a", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	email, _ := domain.NewTemplate(uuid.New(), "b", domain.ChannelEmail, "en", "", "Body", nil, 1, 42, time.Now())
	other, _ := domain.NewTemplate(uuid.New(), "c", domain.ChannelSMS, "en", "", "Body", nil, 1, 99, time.Now())
	repo.tpls[sms.ID] = sms
	repo.tpls[email.ID] = email
	repo.tpls[other.ID] = other

	svc := &ListTemplates{Templates: repo}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42})
	require.NoError(t, err)
	require.Len(t, got, 2)
	ids := map[uuid.UUID]bool{got[0].ID: true, got[1].ID: true}
	assert.True(t, ids[sms.ID])
	assert.True(t, ids[email.ID])
	assert.False(t, ids[other.ID])
}

func TestListTemplates_FilterByChannel(t *testing.T) {
	repo := newFakeTemplates()
	sms, _ := domain.NewTemplate(uuid.New(), "a", domain.ChannelSMS, "en", "", "Body", nil, 1, 42, time.Now())
	email, _ := domain.NewTemplate(uuid.New(), "b", domain.ChannelEmail, "en", "", "Body", nil, 1, 42, time.Now())
	repo.tpls[sms.ID] = sms
	repo.tpls[email.ID] = email

	ch := domain.ChannelSMS
	svc := &ListTemplates{Templates: repo}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42, Channel: &ch})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, sms.ID, got[0].ID)
}

func TestListTemplates_Empty(t *testing.T) {
	svc := &ListTemplates{Templates: newFakeTemplates()}
	got, err := svc.Execute(context.Background(), ListTemplatesInput{OwnerUserID: 42})
	require.NoError(t, err)
	assert.Empty(t, got)
}
