package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsRoot returns the embedded migrations subtree rooted at the SQL
// files (goose looks at the root of the fs.FS it is given).
func migrationsRoot() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		// fs.Sub only errors for invalid paths; "migrations" is always valid here.
		panic(fmt.Sprintf("store: bug: fs.Sub(migrations): %v", err))
	}
	return sub
}

// MigrateUp applies all pending migrations.
func MigrateUp(ctx context.Context, db *gorm.DB) error {
	return runGoose(db, func(g *goose.Provider) error {
		_, err := g.Up(ctx)
		return err
	})
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, db *gorm.DB) error {
	return runGoose(db, func(g *goose.Provider) error {
		_, err := g.Down(ctx)
		return err
	})
}

// MigrateStatus returns a human-readable list of each migration and whether
// it has been applied.
func MigrateStatus(ctx context.Context, db *gorm.DB) (string, error) {
	var out string
	err := runGoose(db, func(g *goose.Provider) error {
		results, err := g.Status(ctx)
		if err != nil {
			return err
		}
		for _, r := range results {
			state := "pending"
			if r.State == goose.StateApplied {
				state = "applied"
			}
			out += fmt.Sprintf("%-8s  %s\n", state, r.Source.Path)
		}
		return nil
	})
	return out, err
}

func runGoose(db *gorm.DB, fn func(*goose.Provider) error) error {
	sqlDB, err := SQLDB(db)
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, migrationsRoot(),
		goose.WithVerbose(false))
	if err != nil {
		return fmt.Errorf("store: goose provider: %w", err)
	}
	if err := fn(provider); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}
