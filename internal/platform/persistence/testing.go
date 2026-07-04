package persistence

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

// NewTestDB returns a *gorm.DB backed by a fresh on-disk SQLite database
// inside t.TempDir, with all migrations applied. The database is closed and
// removed automatically when the test completes.
//
// We use an on-disk file rather than `:memory:` because the goose-sqlite
// driver expects a stable database name to record migration state, and
// multiple in-memory connections in the same process are not always shared.
func NewTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "notifycat-test.db")

	db, err := Open("file:" + dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := MigrateUp(context.Background(), db); err != nil {
		t.Fatalf("store.MigrateUp: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := SQLDB(db)
		if err != nil {
			t.Logf("warning: SQLDB for cleanup: %v", err)
			return
		}
		if err := sqlDB.Close(); err != nil {
			t.Logf("warning: close db: %v", err)
		}
	})
	return db
}

// RawCreateForTest inserts a pull_requests row preserving the caller's
// CreatedAt/UpdatedAt/ClosedAt, bypassing GORM's autoCreate/UpdateTime. Used by
// tests that need to seed PRs with a controlled age and open/closed state. A
// zero CreatedAt defaults to UpdatedAt.
func RawCreateForTest(db *gorm.DB, pr PullRequest) error {
	createdAt := pr.CreatedAt
	if createdAt.IsZero() {
		createdAt = pr.UpdatedAt
	}
	res := db.Exec(
		"INSERT INTO pull_requests (gh_repository, pr_number, created_at, updated_at, closed_at) VALUES (?, ?, ?, ?, ?)",
		pr.Repository, pr.PRNumber, createdAt, pr.UpdatedAt, pr.ClosedAt,
	)
	if res.Error != nil {
		return fmt.Errorf("store: raw insert pull request: %w", res.Error)
	}
	return nil
}
