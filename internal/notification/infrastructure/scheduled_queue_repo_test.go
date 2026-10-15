package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestScheduledQueueRepo_RoundTrip(t *testing.T) {
	queue := NewScheduledQueueRepo(persistence.NewScheduledNotifications(persistence.NewTestDB(t)))
	ctx := context.Background()
	pullRequest := samplePR()
	require.NoError(t, queue.Enqueue(ctx, domain.ScheduledEntry{
		Repository: "acme/api",
		PR:         pullRequest,
		Channel:    "C_ACME",
		Mentions:   []string{"<@U1>"},
	}))

	pending, err := queue.Pending(ctx)

	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.NotZero(t, pending[0].ID)
	assert.NotZero(t, pending[0].QueuedAt)
	assert.Equal(t, "acme/api", pending[0].Repository)
	assert.Equal(t, kernel.PR{Number: 42, Title: "Fix", URL: pullRequest.URL, Author: "alice"}, pending[0].PR)
	assert.Equal(t, "C_ACME", pending[0].Channel)
	assert.Equal(t, []string{"<@U1>"}, pending[0].Mentions)
}

func TestScheduledQueueRepo_MarkSent(t *testing.T) {
	queue := NewScheduledQueueRepo(persistence.NewScheduledNotifications(persistence.NewTestDB(t)))
	ctx := context.Background()
	require.NoError(t, queue.Enqueue(ctx, domain.ScheduledEntry{Repository: "acme/api", PR: samplePR(), Channel: "C_ACME"}))
	pending, err := queue.Pending(ctx)
	require.NoError(t, err)

	require.NoError(t, queue.MarkSent(ctx, []uint{pending[0].ID}, time.Now()))

	remaining, err := queue.Pending(ctx)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}
