package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestPullRequests_AddMessageIsIdempotentPerChannel(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()

	require.NoError(t, repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1"))
	require.NoError(t, repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1"))
	require.NoError(t, repo.AddMessage(ctx, "acme/web", 7, "C0B", "200.1"))

	messages, err := repo.Messages(ctx, "acme/web", 7)

	require.NoError(t, err)
	assert.Len(t, messages, 2, "one row per channel, the repeat is deduped")
}

func TestPullRequests_MessagesNotFound(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))

	_, err := repo.Messages(context.Background(), "acme/web", 999)

	assert.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestPullRequests_DeleteCascadesMessages(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1"))

	require.NoError(t, repo.Delete(ctx, "acme/web", 7))

	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM messages").Scan(&count).Error)
	assert.Zero(t, count)
}

// The digest routes from config, so FindStuck deliberately does not preload a
// PR's messages — the query stays a single table read.
func TestPullRequests_FindStuckDoesNotPreloadMessages(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, repo.AddMessage(ctx, "acme/web", 7, "C0A", "100.1"))
	require.NoError(t, repo.Touch(ctx, "acme/web", 7))

	stuck, err := repo.FindStuck(ctx, time.Now().Add(time.Hour))

	require.NoError(t, err)
	require.Len(t, stuck, 1)
	assert.Empty(t, stuck[0].Messages)
}
