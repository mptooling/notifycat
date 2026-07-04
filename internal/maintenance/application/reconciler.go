package application

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

// Reconciler is the PR reconcile use case; see domain.Reconciler.
type Reconciler struct {
	lister  domain.OpenLister
	checker domain.PRChecker
	closer  domain.Closer
	deleter domain.Deleter
	logger  *slog.Logger
	dryRun  bool
}

// NewReconciler constructs the PR reconcile use case from its domain params.
func NewReconciler(params domain.ReconcilerParams) *Reconciler {
	return &Reconciler{
		lister:  params.Lister,
		checker: params.Checker,
		closer:  params.Closer,
		deleter: params.Deleter,
		logger:  params.Logger,
		dryRun:  params.DryRun,
	}
}

// Run implements domain.Reconciler.
func (r *Reconciler) Run(ctx context.Context) (domain.Summary, error) {
	rows, err := r.lister.ListOpen(ctx)
	if err != nil {
		return domain.Summary{}, err
	}

	var s domain.Summary
	for _, row := range rows {
		s.Checked++
		url := prURL(row.Repository, row.PRNumber)

		open, err := r.checker.IsOpen(ctx, row.Repository, row.PRNumber)
		switch {
		case errors.Is(err, domain.ErrPRNotFound):
			r.removeNotFound(ctx, row, url, err, &s)
		case errors.Is(err, domain.ErrPRDraft):
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
func (r *Reconciler) markClosed(ctx context.Context, row domain.PRRow, url string, s *domain.Summary) {
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
func (r *Reconciler) removeNotFound(ctx context.Context, row domain.PRRow, url string, cause error, s *domain.Summary) {
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
func (r *Reconciler) removeDraft(ctx context.Context, row domain.PRRow, url string, s *domain.Summary) {
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
