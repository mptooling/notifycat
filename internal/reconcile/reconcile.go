// Package reconcile backfills the closed_at column on slack_messages rows whose
// PR is no longer open on GitHub. It exists for the one-time migration to
// stuck-PR digest tracking: rows created before the close handler recorded
// closed_at all have closed_at = NULL and look open to the digest, including
// PRs that were already merged. Running it once (it is idempotent) drops that
// backlog out of the digest.
package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/mptooling/notifycat/internal/store"
)

// OpenLister returns the rows not yet marked closed.
type OpenLister interface {
	ListOpen(ctx context.Context) ([]store.PullRequest, error)
}

// Closer marks a PR's row closed.
type Closer interface {
	MarkClosed(ctx context.Context, repository string, prNumber int) error
}

// Deleter removes a PR's row entirely. Used for drafts, which must not stay in
// the database at all (a draft is not review-ready; the row is recreated by the
// open webhook if it is later marked ready_for_review).
type Deleter interface {
	Delete(ctx context.Context, repository string, prNumber int) error
}

// PRChecker reports whether a PR is still open on GitHub.
type PRChecker interface {
	IsOpen(ctx context.Context, repository string, prNumber int) (bool, error)
}

// Summary tallies one reconcile run.
type Summary struct {
	Checked   int
	Closed    int // marked closed (in dry-run: would be marked)
	Removed   int // PR 404s or is a draft; dropped from the digest (would be, in dry-run)
	StillOpen int
	Errors    int
}

// Reconciler marks rows closed whose PR GitHub reports as no longer open.
type Reconciler struct {
	lister  OpenLister
	checker PRChecker
	closer  Closer
	deleter Deleter
	logger  *slog.Logger
	dryRun  bool
}

// NewReconciler constructs a Reconciler. When dryRun is true it reports what it
// would change without writing.
func NewReconciler(lister OpenLister, checker PRChecker, closer Closer, deleter Deleter, logger *slog.Logger, dryRun bool) *Reconciler {
	return &Reconciler{lister: lister, checker: checker, closer: closer, deleter: deleter, logger: logger, dryRun: dryRun}
}

// Run checks every not-yet-closed row against GitHub and resolves it. Per-PR
// errors are logged and counted, never fatal — a row we cannot confirm is left
// untouched (so a token-scope miss never wrongly hides an open PR), and
// re-running is safe. Two states are dropped rather than left to nag: a 404
// (the PR is gone for good, marked closed) and a draft (deleted outright — a
// draft must never stay in the database, mirroring the converted_to_draft
// webhook).
func (r *Reconciler) Run(ctx context.Context) (Summary, error) {
	rows, err := r.lister.ListOpen(ctx)
	if err != nil {
		return Summary{}, err
	}

	var s Summary
	for _, row := range rows {
		s.Checked++
		url := prURL(row.Repository, row.PRNumber)

		open, err := r.checker.IsOpen(ctx, row.Repository, row.PRNumber)
		switch {
		case errors.Is(err, ErrPRNotFound):
			r.removeNotFound(ctx, row, url, err, &s)
		case errors.Is(err, ErrPRDraft):
			r.removeDraft(ctx, row, url, &s)
		case err != nil:
			s.Errors++
			r.logger.Warn("reconcile: state check failed; leaving row untouched",
				slog.String("repository", row.Repository),
				slog.Int("pr", row.PRNumber),
				slog.String("url", url),
				slog.Any("err", err))
		case open:
			s.StillOpen++
		default:
			r.markClosed(ctx, row, url, &s)
		}
	}
	return s, nil
}

// markClosed records a PR that GitHub reports as merged/closed, so the digest
// skips it. Honours dry-run and counts the outcome on s.
func (r *Reconciler) markClosed(ctx context.Context, row store.PullRequest, url string, s *Summary) {
	if r.dryRun {
		s.Closed++
		r.logger.Info("reconcile: would mark closed (dry-run)",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url))
		return
	}
	if err := r.closer.MarkClosed(ctx, row.Repository, row.PRNumber); err != nil {
		s.Errors++
		r.logger.Error("reconcile: mark closed failed",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url),
			slog.Any("err", err))
		return
	}
	s.Closed++
	r.logger.Info("reconcile: marked closed",
		slog.String("repository", row.Repository),
		slog.Int("pr", row.PRNumber),
		slog.String("url", url))
}

// removeNotFound drops a PR that GitHub 404s so it stops appearing in the
// digest. It reuses MarkClosed (a closed row is excluded from the digest), but
// logs at WARN with the underlying cause and the PR link so an operator can
// tell a genuinely-gone PR from a wrongly-removed one (e.g. a token-scope 404)
// and navigate to check.
func (r *Reconciler) removeNotFound(ctx context.Context, row store.PullRequest, url string, cause error, s *Summary) {
	if r.dryRun {
		s.Removed++
		r.logger.Warn("reconcile: pull request not found; would remove from digest (dry-run)",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url),
			slog.Any("err", cause))
		return
	}
	if err := r.closer.MarkClosed(ctx, row.Repository, row.PRNumber); err != nil {
		s.Errors++
		r.logger.Error("reconcile: remove not-found PR failed",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url),
			slog.Any("err", err))
		return
	}
	s.Removed++
	r.logger.Warn("reconcile: pull request not found; removed from digest",
		slog.String("repository", row.Repository),
		slog.Int("pr", row.PRNumber),
		slog.String("url", url),
		slog.Any("err", cause))
}

// removeDraft deletes a draft PR's row outright: a draft must never stay in the
// database, regardless of its open/closed state. This mirrors the live
// converted_to_draft webhook (DraftHandler), and means a later ready_for_review
// re-announces the PR from a clean slate. Logs at INFO since a draft is a
// normal state, not a fault.
func (r *Reconciler) removeDraft(ctx context.Context, row store.PullRequest, url string, s *Summary) {
	if r.dryRun {
		s.Removed++
		r.logger.Info("reconcile: pull request is a draft; would delete row (dry-run)",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url))
		return
	}
	if err := r.deleter.Delete(ctx, row.Repository, row.PRNumber); err != nil {
		s.Errors++
		r.logger.Error("reconcile: delete draft PR failed",
			slog.String("repository", row.Repository),
			slog.Int("pr", row.PRNumber),
			slog.String("url", url),
			slog.Any("err", err))
		return
	}
	s.Removed++
	r.logger.Info("reconcile: pull request is a draft; deleted row",
		slog.String("repository", row.Repository),
		slog.Int("pr", row.PRNumber),
		slog.String("url", url))
}

// prURL reconstructs the github.com web URL for a PR from repo + number so the
// per-PR log lines are navigable. Assumes github.com (GitHub Enterprise hosts
// are not handled here); mirrors the digest's own URL construction.
func prURL(repository string, number int) string {
	return "https://github.com/" + repository + "/pull/" + strconv.Itoa(number)
}
