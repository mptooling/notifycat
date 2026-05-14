package store

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens a SQLite database for production use. The url accepts either a
// raw file path or the "file:..." DSN form used in our config defaults.
//
// GORM's logger is silenced for "record not found" because we treat ErrNotFound
// as a normal return value, not a warning condition.
func Open(url string) (*gorm.DB, error) {
	dsn := stripFilePrefix(url)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(io.Discard, "", 0),
			logger.Config{
				SlowThreshold:             200 * time.Millisecond,
				LogLevel:                  logger.Warn,
				IgnoreRecordNotFoundError: true,
				Colorful:                  false,
			},
		),
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
