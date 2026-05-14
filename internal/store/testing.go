package store

import (
	"context"
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
