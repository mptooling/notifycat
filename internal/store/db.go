package store

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens a SQLite database for production use. The url accepts either a
// raw file path or the "file:..." DSN form used in our config defaults.
func Open(url string) (*gorm.DB, error) {
	dsn := stripFilePrefix(url)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := enableForeignKeys(db); err != nil {
		return nil, err
	}
	return db, nil
}

// SQLDB returns the underlying *sql.DB. Useful for the migrate binary which
// hands the connection to goose.
func SQLDB(db *gorm.DB) (*sql.DB, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: extract *sql.DB: %w", err)
	}
	return sqlDB, nil
}

func enableForeignKeys(db *gorm.DB) error {
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		return fmt.Errorf("store: enable foreign keys: %w", err)
	}
	return nil
}

func stripFilePrefix(url string) string {
	if rest, ok := strings.CutPrefix(url, "file:"); ok {
		return rest
	}
	return url
}
