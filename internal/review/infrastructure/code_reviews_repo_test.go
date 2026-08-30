package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
)

// seedTrackedPR gives a PR a stored message so a code review can reference it.
func seedTrackedPR(t *testing.T, db *gorm.DB, repository string, prNumber int) {
	t.Helper()

	err := persistence.NewPullRequests(db).AddMessage(context.Background(), repository, prNumber, "C001", "ts-1")
	require.NoError(t, err)
}

func TestCodeReviewsRepo_HasActiveReview_FalseWhenNone(t *testing.T) {
	ctx := context.Background()
	db := persistence.NewTestDB(t)
	codeReviews := persistence.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)
	seedTrackedPR(t, db, "octo/widget", 1)

	beforeStart, err := repo.HasActiveReview(ctx, "octo/widget", 1, "U001")
	require.NoError(t, err)
	require.NoError(t, codeReviews.Start(ctx, "octo/widget", 1, "U001", "alice"))
	afterStart, err := repo.HasActiveReview(ctx, "octo/widget", 1, "U001")

	require.NoError(t, err)
	assert.False(t, beforeStart)
	assert.True(t, afterStart)
}

func TestCodeReviewsRepo_Start_DuplicateReturnsErrActiveReviewExists(t *testing.T) {
	ctx := context.Background()
	db := persistence.NewTestDB(t)
	repo := NewCodeReviewsRepo(persistence.NewCodeReviews(db))
	seedTrackedPR(t, db, "octo/widget", 2)
	require.NoError(t, repo.Start(ctx, "octo/widget", 2, "U002", "bob"))

	err := repo.Start(ctx, "octo/widget", 2, "U002", "bob")

	assert.ErrorIs(t, err, reviewdomain.ErrActiveReviewExists)
}

func TestCodeReviewsRepo_GetActive_ReturnsSessionAndErrNoActiveReview(t *testing.T) {
	ctx := context.Background()
	db := persistence.NewTestDB(t)
	codeReviews := persistence.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)

	_, err := repo.GetActive(ctx, "octo/widget", 3)
	assert.ErrorIs(t, err, notificationdomain.ErrNoActiveReview, "no session yet")

	seedTrackedPR(t, db, "octo/widget", 3)
	require.NoError(t, codeReviews.Start(ctx, "octo/widget", 3, "U003", "carol"))
	session, err := repo.GetActive(ctx, "octo/widget", 3)

	require.NoError(t, err)
	assert.Equal(t, "U003", session.SlackUserID)
	assert.Equal(t, "carol", session.SlackUserName)
}

func TestCodeReviewsRepo_Reviewers_MapsAllSessions(t *testing.T) {
	ctx := context.Background()
	db := persistence.NewTestDB(t)
	codeReviews := persistence.NewCodeReviews(db)
	repo := NewCodeReviewsRepo(codeReviews)
	seedTrackedPR(t, db, "octo/widget", 4)
	for _, reviewer := range []notificationdomain.ReviewSession{
		{SlackUserID: "U111", SlackUserName: "alice"},
		{SlackUserID: "U222", SlackUserName: "bob"},
	} {
		require.NoError(t, codeReviews.Start(ctx, "octo/widget", 4, reviewer.SlackUserID, reviewer.SlackUserName))
		require.NoError(t, codeReviews.Finish(ctx, "octo/widget", 4))
	}

	sessions, err := repo.Reviewers(ctx, "octo/widget", 4)

	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "U111", sessions[0].SlackUserID)
	assert.Equal(t, "alice", sessions[0].SlackUserName)
	assert.Equal(t, "U222", sessions[1].SlackUserID)
	assert.Equal(t, "bob", sessions[1].SlackUserName)
}
