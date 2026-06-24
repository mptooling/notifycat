// Command notifycat-reconcile is a one-time maintenance tool that marks
// slack_messages rows closed when their PR is no longer open on GitHub.
//
// It exists for the migration to stuck-PR digest tracking: rows created before
// the close handler recorded closed_at all have closed_at = NULL and look open
// to the digest — including PRs that were already merged. Run it once (it is
// idempotent) to drop that backlog out of the digest.
//
// Usage:
//
//	notifycat-reconcile            — mark closed PRs from their GitHub state
//	notifycat-reconcile -dry-run   — report what would change, write nothing
//
// It reads PR state from GitHub, so GITHUB_TOKEN must be set with read access
// to the repos, and DATABASE_URL must point at the same database the server
// uses.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/reconcile"
	"github.com/mptooling/notifycat/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-reconcile:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("notifycat-reconcile", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.GitHubToken.Reveal() == "" {
		return fmt.Errorf("GITHUB_TOKEN is required: the reconcile reads PR state from GitHub")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()

	messages := store.NewSlackMessages(db)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	gh := github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	rec := reconcile.NewReconciler(messages, reconcile.NewGitHubChecker(gh), messages, messages, logger, *dryRun)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	summary, err := rec.Run(ctx)
	if err != nil {
		return err
	}

	mode := "applied"
	if *dryRun {
		mode = "dry-run"
	}
	fmt.Printf("reconcile (%s): checked=%d closed=%d removed=%d still_open=%d errors=%d\n",
		mode, summary.Checked, summary.Closed, summary.Removed, summary.StillOpen, summary.Errors)
	if summary.Errors > 0 {
		return fmt.Errorf("%d PR(s) could not be checked; resolve (e.g. token scope) and re-run — it is idempotent", summary.Errors)
	}
	return nil
}
