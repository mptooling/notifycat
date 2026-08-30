package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func seedPullRequests(t *testing.T, db *gorm.DB, rows ...persistence.PullRequest) {
	t.Helper()

	for _, row := range rows {
		require.NoError(t, persistence.RawCreateForTest(db, row))
	}
}

func TestPullRequests_Touch_BumpsUpdatedAt(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	ctx := context.Background()
	seededAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	seedPullRequests(t, db, persistence.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: seededAt})

	require.NoError(t, repo.Touch(ctx, "o/r", 1))

	// FindStuck reads each PR's updated_at; a far-future cutoff returns ours.
	stuck, err := repo.FindStuck(ctx, time.Now().Add(time.Hour))

	require.NoError(t, err)
	require.Len(t, stuck, 1)
	assert.True(t, stuck[0].UpdatedAt.After(seededAt), "Touch must bump updated_at")
}

func TestPullRequests_Touch_MissingIsNoop(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))

	err := repo.Touch(context.Background(), "o/r", 1)

	assert.NoError(t, err)
}

func TestPullRequests_MarkClosed_ExcludesFromFindStuck(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	ctx := context.Background()
	seededAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	seedPullRequests(t, db, persistence.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: seededAt})

	require.NoError(t, repo.MarkClosed(ctx, "o/r", 1))

	stuck, err := repo.FindStuck(ctx, time.Now())

	require.NoError(t, err)
	assert.Empty(t, stuck, "a closed PR never counts as stuck")
}

func TestPullRequests_FindStuck_OnlyOpenAndStale(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	stale := now.Add(-48 * time.Hour)
	closedAt := now.Add(-1 * time.Hour)
	seedPullRequests(t, db,
		persistence.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: stale},
		persistence.PullRequest{PRNumber: 2, Repository: "o/r", UpdatedAt: now.Add(-1 * time.Hour)},
		persistence.PullRequest{PRNumber: 3, Repository: "o/r", UpdatedAt: stale, ClosedAt: &closedAt},
	)

	stuck, err := repo.FindStuck(ctx, now.Add(-24*time.Hour))

	require.NoError(t, err)
	require.Len(t, stuck, 1)
	assert.Equal(t, 1, stuck[0].PRNumber, "only the stale open PR is stuck")
}

func TestPullRequests_FindStuck_Empty(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))

	stuck, err := repo.FindStuck(context.Background(), time.Now())

	require.NoError(t, err)
	assert.Empty(t, stuck)
}

func TestPullRequests_ListOpen_ExcludesClosed(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	now := time.Now().UTC().Truncate(time.Second)
	closedAt := now.Add(-1 * time.Hour)
	seedPullRequests(t, db,
		persistence.PullRequest{PRNumber: 2, Repository: "o/r", UpdatedAt: now},
		persistence.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: now},
		persistence.PullRequest{PRNumber: 9, Repository: "o/r", UpdatedAt: now, ClosedAt: &closedAt},
	)

	open, err := repo.ListOpen(context.Background())

	require.NoError(t, err)
	require.Len(t, open, 2)
	assert.Equal(t, 1, open[0].PRNumber, "rows come back ordered by (repository, pr_number)")
	assert.Equal(t, 2, open[1].PRNumber)
}

func TestPullRequests_DeleteStaleBefore_RemovesOldRows(t *testing.T) {
	db := persistence.NewTestDB(t)
	repo := persistence.NewPullRequests(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	seedPullRequests(t, db,
		persistence.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: now.Add(-72 * time.Hour)},
		persistence.PullRequest{PRNumber: 2, Repository: "o/r", UpdatedAt: now.Add(-48 * time.Hour)},
		persistence.PullRequest{PRNumber: 3, Repository: "o/r", UpdatedAt: now.Add(-1 * time.Hour)},
	)

	deleted, err := repo.DeleteStaleBefore(ctx, now.Add(-24*time.Hour))

	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	for _, prNumber := range []int{1, 2} {
		_, err := repo.Messages(ctx, "o/r", prNumber)
		assert.ErrorIs(t, err, persistence.ErrNotFound, "stale PR %d should be gone", prNumber)
	}
	_, err = repo.Messages(ctx, "o/r", 3)
	assert.NoError(t, err, "the fresh PR survives")
}

func TestPullRequests_DeleteStaleBefore_Empty(t *testing.T) {
	repo := persistence.NewPullRequests(persistence.NewTestDB(t))

	deleted, err := repo.DeleteStaleBefore(context.Background(), time.Now())

	require.NoError(t, err)
	assert.Zero(t, deleted)
}

func TestMigrate_CreatesPullRequestsAndMessages(t *testing.T) {
	db := persistence.NewTestDB(t)

	for _, table := range []string{"pull_requests", "messages"} {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name).Error

			require.NoError(t, err)
			assert.Equal(t, table, name)
		})
	}
}
