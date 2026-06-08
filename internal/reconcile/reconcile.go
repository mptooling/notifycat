// Package reconcile backfills the closed_at column on slack_messages rows whose
// PR is no longer open on GitHub. It exists for the one-time migration to
// stuck-PR digest tracking: rows created before the close handler recorded
// closed_at all have closed_at = NULL and look open to the digest, including
// PRs that were already merged. Running it once (it is idempotent) drops that
// backlog out of the digest.
package reconcile

import (
	"context"
	"log/slog"

	"github.com/mptooling/notifycat/internal/store"
)

// OpenLister returns the rows not yet marked closed.
type OpenLister interface {
	ListOpen(ctx context.Context) ([]store.SlackMessage, error)
}

// Closer marks a PR's row closed.
type Closer interface {
	MarkClosed(ctx context.Context, repository string, prNumber int) error
}

// PRChecker reports whether a PR is still open on GitHub.
type PRChecker interface {
	IsOpen(ctx context.Context, repository string, prNumber int) (bool, error)
}

// Summary tallies one reconcile run.
type Summary struct {
	Checked   int
	Closed    int // marked closed (in dry-run: would be marked)
	StillOpen int
	Errors    int
}

// Reconciler marks rows closed whose PR GitHub reports as no longer open.
type Reconciler struct {
	lister  OpenLister
	checker PRChecker
	closer  Closer
	logger  *slog.Logger
	dryRun  bool
}

// NewReconciler constructs a Reconciler. When dryRun is true it reports what it
// would close without writing.
func NewReconciler(lister OpenLister, checker PRChecker, closer Closer, logger *slog.Logger, dryRun bool) *Reconciler {
	return &Reconciler{lister: lister, checker: checker, closer: closer, logger: logger, dryRun: dryRun}
}

// Run checks every not-yet-closed row against GitHub and marks the closed ones.
// Per-PR errors are logged and counted, never fatal — a row we cannot confirm
// is left untouched (so a token-scope miss never wrongly hides an open PR), and
// re-running is safe.
func (r *Reconciler) Run(ctx context.Context) (Summary, error) {
	rows, err := r.lister.ListOpen(ctx)
	if err != nil {
		return Summary{}, err
	}

	var s Summary
	for _, row := range rows {
		s.Checked++
		open, err := r.checker.IsOpen(ctx, row.Repository, row.PRNumber)
		if err != nil {
			s.Errors++
			r.logger.Warn("reconcile: state check failed; leaving row untouched",
				slog.String("repository", row.Repository),
				slog.Int("pr", row.PRNumber),
				slog.Any("err", err))
			continue
		}
		if open {
			s.StillOpen++
			continue
		}

		if r.dryRun {
			s.Closed++
			r.logger.Info("reconcile: would mark closed (dry-run)",
				slog.String("repository", row.Repository),
				slog.Int("pr", row.PRNumber))
			continue
		}
		if err := r.closer.MarkClosed(ctx, row.Repository, row.PRNumber); err != nil {
			s.Errors++
			r.logger.Error("reconcile: mark closed failed",
				slog.String("repository", row.Repository),
				slog.Int("pr", row.PRNumber),
				slog.Any("err", err))
			continue
		}
		s.Closed++
		r.logger.Info("reconcile: marked closed",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber))
	}
	return s, nil
}
