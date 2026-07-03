package domain

import (
	"context"
	"errors"
	"time"
)

// StaleMessageDeleter deletes tracked-PR rows whose message predates a cutoff.
// It is the persistence port the cleanup use case drives; the maintenance
// infrastructure layer satisfies it over the store.
type StaleMessageDeleter interface {
	DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// StaleMessageCleaner prunes stale slack_messages rows on a fixed cadence: one
// pass immediately on start, then one every Interval, until its context is
// cancelled. It deletes only database rows — never the Slack messages
// themselves. A pass's error is logged and swallowed so the next tick retries;
// Run always returns nil (so callers can compose it with other long-running
// goroutines without special-casing).
type StaleMessageCleaner interface {
	Run(ctx context.Context) error
}

// ErrPRNotFound marks a PR that GitHub reports as 404 — deleted, or in a repo
// that was renamed or is no longer accessible. The reconciler treats it
// distinctly from other API errors: rather than leave the row untouched, it
// removes the PR from the digest (a 404 will never resolve on its own).
var ErrPRNotFound = errors.New("reconcile: pull request not found")

// ErrPRDraft marks a draft PR. A draft must never stay in the database
// (regardless of open/closed state), so the reconciler deletes its row the same
// way the live converted_to_draft webhook does — the difference being this
// catches drafts in the pre-tracking backlog.
var ErrPRDraft = errors.New("reconcile: pull request is a draft")

// OpenLister lists the tracked PR rows not yet marked closed.
type OpenLister interface {
	ListOpen(ctx context.Context) ([]PRRow, error)
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

// Reconciler resolves every not-yet-closed tracked PR against GitHub: a
// merged/closed PR is marked closed, a 404 is removed from the digest, and a
// draft is deleted outright (a draft must never stay in the database). Per-PR
// errors are logged and counted, never fatal — an unconfirmable row is left
// untouched so a token-scope miss never wrongly hides an open PR, and
// re-running is safe. Run reports the tallies in a Summary.
type Reconciler interface {
	Run(ctx context.Context) (Summary, error)
}
