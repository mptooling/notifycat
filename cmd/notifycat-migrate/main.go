// Command notifycat-migrate applies the embedded database migrations.
//
// Usage:
//
//	notifycat-migrate up       — apply all pending migrations
//	notifycat-migrate down     — roll back the last migration
//	notifycat-migrate status   — print which migrations are applied
//
// The database URL is read from DATABASE_URL (default: file:./data/notifycat.db).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-migrate:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "up":
		return store.MigrateUp(ctx, db)
	case "down":
		return store.MigrateDown(ctx, db)
	case "status":
		out, err := store.MigrateStatus(ctx, db)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage())
	}
}

func usageError() error { return fmt.Errorf("missing subcommand\n%s", usage()) }

func usage() string {
	return "usage: notifycat-migrate <up|down|status>"
}
