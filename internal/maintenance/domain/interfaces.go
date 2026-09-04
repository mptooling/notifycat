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

// TrackedLister lists open PRs together with the channels their messages
// currently live in. It is the relocate use case's read port.
type TrackedLister interface {
	ListOpenWithMessages(ctx context.Context) ([]TrackedPR, error)
}

// MessageRows retargets or drops a PR's stored message row. It never touches
// the messenger — only the database row that records where a message lives.
type MessageRows interface {
	MoveMessage(ctx context.Context, repository string, prNumber int, fromChannel, toChannel, messageID string) error
	RemoveMessage(ctx context.Context, repository string, prNumber int, channel string) error
}

// MessageCourier carries a posted message between channels. Repost reads the
// original, rewrites it as a mention-free "moved" message and posts it to
// toChannel, returning the new message id; it reports ErrMessageGone when the
// original is no longer there. CopyReactions re-adds the source message's
// reactions, restricted to allowed, so a relocated message keeps its review
// state without inheriting ad-hoc human reactions the bot cannot attribute.
type MessageCourier interface {
	Repost(ctx context.Context, from TrackedMessage, toChannel string) (string, error)
	CopyReactions(ctx context.Context, from, to TrackedMessage, allowed []string) error
	Delete(ctx context.Context, message TrackedMessage) error
}

// ReactionPolicy lists the reaction emoji a repository's notifications use, so
// a relocated message carries those and nothing else.
type ReactionPolicy interface {
	AllowedReactions(ctx context.Context, repository string) ([]string, error)
}

// ChannelConfig reports every channel a repository is currently configured to
// post to — its base fan-out plus any `paths:` channels. A stored message in a
// channel absent from this set is stale routing.
type ChannelConfig interface {
	ConfiguredChannels(repository string) []string
}

// ErrMessageGone marks an original message the messenger no longer has: it was
// deleted by hand, or the bot lost sight of the channel. There is nothing to
// carry over, so the relocator drops the row instead of failing the run.
var ErrMessageGone = errors.New("relocate: message no longer exists")

// Relocator moves stored messages from one channel to another after an
// operator repoints a repository. Per PR it either moves the message (repost in
// the new channel, retarget the row, carry the reactions over, delete the
// original), merges (the PR already has a message in the destination, so only
// the original goes), or drops (no destination given). A per-PR failure is
// logged and counted, never fatal, and the run is idempotent: the row is
// retargeted immediately after the repost, so a re-run skips what already
// moved. Audit reports rows sitting in channels the config no longer mentions.
type Relocator interface {
	Run(ctx context.Context) (RelocateSummary, error)
	Audit(ctx context.Context) ([]StaleMessage, error)
}
