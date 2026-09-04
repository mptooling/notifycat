package persistence_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestPullRequests_ListOpenWithMessages(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_OLD", "100.1"))
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_EXTRA", "100.2"))
	require.NoError(t, repo.AddMessage(ctx, "acme/web", 8, "C_OLD", "200.1"))
	require.NoError(t, repo.MarkClosed(ctx, "acme/web", 8))

	open, err := repo.ListOpenWithMessages(ctx)

	require.NoError(t, err)
	require.Len(t, open, 1, "a closed PR has nothing left to relocate")
	assert.Equal(t, "acme/api", open[0].Repository)
	assert.Len(t, open[0].Messages, 2)
}

func TestPullRequests_MoveMessage(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_OLD", "100.1"))

	require.NoError(t, repo.MoveMessage(ctx, "acme/api", 7, "C_OLD", "C_NEW", "300.3"))

	messages, err := repo.Messages(ctx, "acme/api", 7)
	require.NoError(t, err)
	require.Len(t, messages, 1, "the row is retargeted, not duplicated")
	assert.Equal(t, "C_NEW", messages[0].Channel)
	assert.Equal(t, "300.3", messages[0].MessageID)
}

func TestPullRequests_MoveMessage_UnknownRowIsNotFound(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_OLD", "100.1"))

	err := repo.MoveMessage(ctx, "acme/api", 7, "C_GONE", "C_NEW", "300.3")

	require.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestPullRequests_RemoveMessage(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_OLD", "100.1"))
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_KEEP", "100.2"))

	require.NoError(t, repo.RemoveMessage(ctx, "acme/api", 7, "C_OLD"))

	messages, err := repo.Messages(ctx, "acme/api", 7)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "C_KEEP", messages[0].Channel, "only the named channel's row goes")
}

func TestPullRequests_RemoveMessage_UnknownRowIsNotFound(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/api", 7, "C_OLD", "100.1"))

	err := repo.RemoveMessage(ctx, "acme/api", 7, "C_GONE")

	require.ErrorIs(t, err, persistence.ErrNotFound)
}
