package domain

import (
	"log/slog"
	"time"
)

// CleanerParams bundles everything the stale-message cleaner needs. TTL is the
// maximum age a row may reach before it becomes eligible for deletion;
// Interval is the cadence between passes (use the Interval constant in
// production); Now supplies the clock (time.Now in production, a fixed clock in
// tests).
type CleanerParams struct {
	Deleter  StaleMessageDeleter
	TTL      time.Duration
	Interval time.Duration
	Logger   *slog.Logger
	Now      func() time.Time
}

// PRRow is the maintenance view of one tracked PR: the fields the reconciler
// reads to check and resolve it. It is mapped from the store's persistence
// model at the repository boundary, so no gorm-tagged type crosses a port.
type PRRow struct {
	Repository string
	PRNumber   int
}

// Summary tallies one reconcile run.
type Summary struct {
	Checked   int
	Closed    int // marked closed (in dry-run: would be marked)
	Removed   int // PR 404s or is a draft; dropped from the digest (would be, in dry-run)
	StillOpen int
	Errors    int
}

// ReconcilerParams bundles everything the reconciler needs. DryRun reports what
// would change without writing.
type ReconcilerParams struct {
	Lister  OpenLister
	Checker PRChecker
	Closer  Closer
	Deleter Deleter
	Logger  *slog.Logger
	DryRun  bool
}
