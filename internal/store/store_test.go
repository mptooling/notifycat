package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/store"
)

// prAbsent reports whether the PR has no row (Messages returns ErrNotFound).
func prAbsent(t *testing.T, repo *store.PullRequests, repository string, prNumber int) bool {
	t.Helper()
	_, err := repo.Messages(context.Background(), repository, prNumber)
	return errors.Is(err, store.ErrNotFound)
}

func TestPullRequests_Touch_BumpsUpdatedAt(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()

	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	if err := store.RawCreateForTest(db, store.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: old}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := repo.Touch(ctx, "o/r", 1); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	// FindStuck reads each PR's updated_at; a far-future cutoff returns ours.
	stuck, err := repo.FindStuck(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(stuck) != 1 || !stuck[0].UpdatedAt.After(old) {
		t.Fatalf("Touch did not bump updated_at: %+v (seed %v)", stuck, old)
	}
}

func TestPullRequests_Touch_MissingIsNoop(t *testing.T) {
	repo := store.NewPullRequests(store.NewTestDB(t))
	if err := repo.Touch(context.Background(), "o/r", 1); err != nil {
		t.Fatalf("Touch missing: %v", err)
	}
}

func TestPullRequests_MarkClosed_ExcludesFromFindStuck(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()

	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	if err := store.RawCreateForTest(db, store.PullRequest{PRNumber: 1, Repository: "o/r", UpdatedAt: old}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := repo.MarkClosed(ctx, "o/r", 1); err != nil {
		t.Fatalf("MarkClosed: %v", err)
	}

	stuck, err := repo.FindStuck(ctx, time.Now())
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(stuck) != 0 {
		t.Fatalf("FindStuck returned %d rows; want 0 (closed PR excluded)", len(stuck))
	}
}

func TestPullRequests_FindStuck_OnlyOpenAndStale(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	stale := now.Add(-48 * time.Hour)
	closedAt := now.Add(-1 * time.Hour)

	seed := []store.PullRequest{
		{PRNumber: 1, Repository: "o/r", UpdatedAt: stale},
		{PRNumber: 2, Repository: "o/r", UpdatedAt: now.Add(-1 * time.Hour)},
		{PRNumber: 3, Repository: "o/r", UpdatedAt: stale, ClosedAt: &closedAt},
	}
	for _, pr := range seed {
		if err := store.RawCreateForTest(db, pr); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	stuck, err := repo.FindStuck(ctx, cutoff)
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(stuck) != 1 || stuck[0].PRNumber != 1 {
		t.Fatalf("FindStuck = %+v; want only the stale open PR (1)", stuck)
	}
}

func TestPullRequests_FindStuck_Empty(t *testing.T) {
	repo := store.NewPullRequests(store.NewTestDB(t))
	stuck, err := repo.FindStuck(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("FindStuck on empty: %v", err)
	}
	if len(stuck) != 0 {
		t.Fatalf("FindStuck on empty returned %d rows; want 0", len(stuck))
	}
}

func TestPullRequests_ListOpen_ExcludesClosed(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	closedAt := now.Add(-1 * time.Hour)
	seed := []store.PullRequest{
		{PRNumber: 2, Repository: "o/r", UpdatedAt: now},
		{PRNumber: 1, Repository: "o/r", UpdatedAt: now},
		{PRNumber: 9, Repository: "o/r", UpdatedAt: now, ClosedAt: &closedAt},
	}
	for _, pr := range seed {
		if err := store.RawCreateForTest(db, pr); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	open, err := repo.ListOpen(ctx)
	if err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("ListOpen returned %d rows; want 2 open", len(open))
	}
	// Ordered by (gh_repository, pr_number).
	if open[0].PRNumber != 1 || open[1].PRNumber != 2 {
		t.Fatalf("ListOpen order = %d,%d; want 1,2", open[0].PRNumber, open[1].PRNumber)
	}
}

func TestPullRequests_DeleteStaleBefore_RemovesOldRows(t *testing.T) {
	db := store.NewTestDB(t)
	repo := store.NewPullRequests(db)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	seed := []store.PullRequest{
		{PRNumber: 1, Repository: "o/r", UpdatedAt: now.Add(-72 * time.Hour)},
		{PRNumber: 2, Repository: "o/r", UpdatedAt: now.Add(-48 * time.Hour)},
		{PRNumber: 3, Repository: "o/r", UpdatedAt: now.Add(-1 * time.Hour)},
	}
	for _, pr := range seed {
		if err := store.RawCreateForTest(db, pr); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	deleted, err := repo.DeleteStaleBefore(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteStaleBefore: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteStaleBefore returned %d; want 2", deleted)
	}
	for _, pr := range []int{1, 2} {
		if !prAbsent(t, repo, "o/r", pr) {
			t.Errorf("PR %d still present after delete", pr)
		}
	}
	if prAbsent(t, repo, "o/r", 3) {
		t.Errorf("fresh PR 3 removed unexpectedly")
	}
}

func TestPullRequests_DeleteStaleBefore_Empty(t *testing.T) {
	repo := store.NewPullRequests(store.NewTestDB(t))
	deleted, err := repo.DeleteStaleBefore(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("DeleteStaleBefore on empty: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("DeleteStaleBefore on empty returned %d; want 0", deleted)
	}
}

func TestMigrate_CreatesPullRequestsAndMessages(t *testing.T) {
	db := store.NewTestDB(t)
	for _, table := range []string{"pull_requests", "messages"} {
		var name string
		err := db.Raw(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name).Error
		if err != nil || name != table {
			t.Fatalf("table %q missing after migrate (got %q, err %v)", table, name, err)
		}
	}
}
