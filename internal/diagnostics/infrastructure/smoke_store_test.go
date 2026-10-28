package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

const smokePullRequestNumber = 1761650000

func TestStoreSmokeCleanup_DeletePRRemovesTheFeedPullRequest(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()
	pullRequests := persistence.NewPullRequests(db)
	feedPullRequests := persistence.NewFeedPullRequests(db)
	updatedAt := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	require.NoError(t, pullRequests.AddMessage(ctx, "Acme/API", smokePullRequestNumber, "C123ABCDE", "1700000000.000100"))
	require.NoError(t, feedPullRequests.Upsert(ctx, persistence.FeedPullRequest{
		Repository: "acme/api", Number: smokePullRequestNumber, HeadSHA: "smoke", UpdatedAt: updatedAt,
	}))
	require.NoError(t, feedPullRequests.Upsert(ctx, persistence.FeedPullRequest{
		Repository: "acme/api", Number: 7, HeadSHA: "real", UpdatedAt: updatedAt,
	}))
	cleanup := NewStoreSmokeCleanup(pullRequests, feedPullRequests)

	err := cleanup.DeletePR(ctx, "Acme/API", smokePullRequestNumber)

	require.NoError(t, err)
	_, messagesErr := pullRequests.Messages(ctx, "Acme/API", smokePullRequestNumber)
	assert.ErrorIs(t, messagesErr, persistence.ErrNotFound)
	remaining, listErr := feedPullRequests.ChangedSince(ctx, []string{"acme/api"}, updatedAt.Add(-time.Hour))
	require.NoError(t, listErr)
	require.Len(t, remaining, 1, "the smoke pull request never stays in an agent's feed")
	assert.Equal(t, 7, remaining[0].Number)
}

func TestStoreSmokeCleanup_DeletePRWithoutRowsIsNoOp(t *testing.T) {
	db := persistence.NewTestDB(t)
	cleanup := NewStoreSmokeCleanup(persistence.NewPullRequests(db), persistence.NewFeedPullRequests(db))

	err := cleanup.DeletePR(context.Background(), "acme/api", smokePullRequestNumber)

	assert.NoError(t, err)
}
