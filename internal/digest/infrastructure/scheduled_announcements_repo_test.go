package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestScheduledAnnouncementsRepo_MapsQueuedRows(t *testing.T) {
	scheduled := persistence.NewScheduledNotifications(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, scheduled.Enqueue(ctx, persistence.ScheduledItem{Repository: "acme/api", PRNumber: 42, Channel: "C_ACME"}))
	require.NoError(t, scheduled.Enqueue(ctx, persistence.ScheduledItem{Repository: "acme/api", PRNumber: 42, Channel: "C_OPS"}))
	repo := NewScheduledAnnouncementsRepo(scheduled)

	announced, err := repo.AnnouncedSince(ctx, time.Now())

	require.NoError(t, err)
	assert.Equal(t, []domain.AnnouncedPR{
		{Repository: "acme/api", PRNumber: 42, Channel: "C_ACME"},
		{Repository: "acme/api", PRNumber: 42, Channel: "C_OPS"},
	}, announced)
}
