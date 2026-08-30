package persistence_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// seedPR inserts a tracked PR (via the normal open path) so a code review has a
// parent row to reference.
func seedPR(t *testing.T, db *gorm.DB, repository string, prNumber int) {
	t.Helper()

	err := persistence.NewPullRequests(db).AddMessage(context.Background(), repository, prNumber, "C0", "1.1")
	require.NoError(t, err)
}

// reviewsOnSeededPR returns a code-review repo for a PR that is already tracked.
func reviewsOnSeededPR(t *testing.T, db *gorm.DB) *persistence.CodeReviews {
	t.Helper()

	seedPR(t, db, "acme/web", 7)
	return persistence.NewCodeReviews(db)
}

func TestCodeReviews_SameUserSecondStartConflicts(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))

	err := reviews.Start(ctx, "acme/web", 7, "U1", "Ada")

	assert.ErrorIs(t, err, persistence.ErrActiveReviewExists)
}

func TestCodeReviews_DifferentUsersReviewConcurrently(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))

	err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo")

	assert.NoError(t, err, "a second, different reviewer is allowed")
}

func TestCodeReviews_FinishAllowsRestart(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))
	require.NoError(t, reviews.Finish(ctx, "acme/web", 7))

	err := reviews.Start(ctx, "acme/web", 7, "U2", "Bo")

	assert.NoError(t, err)
}

func TestCodeReviews_GetActive(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))

	active, err := reviews.GetActive(ctx, "acme/web", 7)

	require.NoError(t, err)
	assert.Equal(t, "U1", active.SlackUserID)
	assert.Equal(t, "Ada", active.SlackUserName)
	assert.Nil(t, active.FinishedAt)

	require.NoError(t, reviews.Finish(ctx, "acme/web", 7))
	_, err = reviews.GetActive(ctx, "acme/web", 7)
	assert.ErrorIs(t, err, persistence.ErrNotFound, "a finished review is no longer active")
}

func TestCodeReviews_GetActiveNotFound(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))

	_, err := reviews.GetActive(context.Background(), "acme/web", 7)

	assert.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestCodeReviews_ActiveForUser(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))

	got, err := reviews.ActiveForUser(ctx, "acme/web", 7, "U1")

	require.NoError(t, err)
	assert.Equal(t, "U1", got.SlackUserID)

	_, err = reviews.ActiveForUser(ctx, "acme/web", 7, "U2")
	assert.ErrorIs(t, err, persistence.ErrNotFound, "another user has no active review")

	require.NoError(t, reviews.Finish(ctx, "acme/web", 7))
	_, err = reviews.ActiveForUser(ctx, "acme/web", 7, "U1")
	assert.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestCodeReviews_FinishNoActiveIsNoop(t *testing.T) {
	reviews := reviewsOnSeededPR(t, persistence.NewTestDB(t))

	err := reviews.Finish(context.Background(), "acme/web", 7)

	assert.NoError(t, err)
}

func TestCodeReviews_StartUnknownPRNotFound(t *testing.T) {
	reviews := persistence.NewCodeReviews(persistence.NewTestDB(t))

	err := reviews.Start(context.Background(), "acme/web", 999, "U1", "Ada")

	assert.ErrorIs(t, err, persistence.ErrNotFound)
}

func TestCodeReviews_CascadeDeletedWithPR(t *testing.T) {
	db := persistence.NewTestDB(t)
	reviews := reviewsOnSeededPR(t, db)
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))

	require.NoError(t, persistence.NewPullRequests(db).Delete(ctx, "acme/web", 7))

	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM code_reviews").Scan(&count).Error)
	assert.Zero(t, count)
}

func TestCodeReviews_Reviewers(t *testing.T) {
	db := persistence.NewTestDB(t)
	reviews := reviewsOnSeededPR(t, db)
	seedPR(t, db, "acme/web", 8)
	ctx := context.Background()
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U1", "Ada"))
	require.NoError(t, reviews.Finish(ctx, "acme/web", 7))
	require.NoError(t, reviews.Start(ctx, "acme/web", 7, "U2", "Bo"))
	require.NoError(t, reviews.Start(ctx, "acme/web", 8, "U3", "Cy"))

	got, err := reviews.Reviewers(ctx, "acme/web", 7)

	require.NoError(t, err)
	require.Len(t, got, 2, "PR 8's reviewer must not leak into PR 7")
	assert.Equal(t, "U1", got[0].SlackUserID, "reviews come back in started_at ASC order")
	assert.Equal(t, "U2", got[1].SlackUserID)
}

func TestCodeReviews_Reviewers_UntrackedPR(t *testing.T) {
	reviews := persistence.NewCodeReviews(persistence.NewTestDB(t))

	got, err := reviews.Reviewers(context.Background(), "acme/web", 999)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCodeReviews_Migration00008DownRestoresSingleActiveIndex(t *testing.T) {
	db := persistence.NewTestDB(t)
	ctx := context.Background()
	const indexSQL = "SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_code_reviews_active'"

	var afterUp string
	require.NoError(t, db.Raw(indexSQL).Scan(&afterUp).Error)
	require.Contains(t, afterUp, "slack_user_id", "all migrations applied means a per-(PR,user) index")

	require.NoError(t, persistence.MigrateDown(ctx, db))

	var afterDown string
	require.NoError(t, db.Raw(indexSQL).Scan(&afterDown).Error)
	assert.NotContains(t, afterDown, "slack_user_id", "00008 Down restores the single-active index")
}
