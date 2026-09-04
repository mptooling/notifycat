package domain

import (
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
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
	// Provider is the deployment's git host; it selects the web-URL form the
	// per-PR log lines are reconstructed in. The zero value builds github.com URLs.
	Provider kernel.Provider
}

// TrackedMessage is one posted message: the channel it lives in and the
// messenger's id for the post.
type TrackedMessage struct {
	Channel   string
	MessageID string
}

// TrackedPR is one open PR with every message posted for it. Mapped from the
// store's persistence model at the repository boundary.
type TrackedPR struct {
	Repository string
	PRNumber   int
	Messages   []TrackedMessage
}

// StaleMessage is one audit finding: a stored message sitting in a channel the
// repository is no longer configured to post to.
type StaleMessage struct {
	Repository string
	PRNumber   int
	Channel    string
}

// RelocateSummary tallies one relocate run.
type RelocateSummary struct {
	Scanned int // open PRs holding a message in the source channel
	Moved   int // reposted in the destination and retargeted (would be, in dry-run)
	Merged  int // destination already had a message; only the original was removed
	Dropped int // no destination given; the original was removed
	Errors  int
}

// RelocatorParams bundles everything the relocate use case needs. From is the
// channel to move messages out of; To is the destination, and an empty To means
// drop the messages instead of moving them. Repository, when set, narrows the
// run to one "org/repo". DryRun reports what would change without writing.
type RelocatorParams struct {
	Lister     TrackedLister
	Rows       MessageRows
	Courier    MessageCourier
	Reactions  ReactionPolicy
	Channels   ChannelConfig
	Logger     *slog.Logger
	From       string
	To         string
	Repository string
	DryRun     bool
}
