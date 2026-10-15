package domain

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// Messenger delivers notification messages to the chat platform. It receives
// domain intent — which notification, for which PR, with which data — and its
// adapter owns the message shaping (which blocks/markers). The infra Slack
// messenger is the only implementation.
type Messenger interface {
	PostOpen(ctx context.Context, channel string, req OpenRequest) (messageID string, err error)
	UpdateClosed(ctx context.Context, channel, messageID string, req ClosedRequest) error
	UpdateReviewFinished(ctx context.Context, channel, messageID string, req ReviewFinishedRequest) error
	AddReaction(ctx context.Context, channel, messageID, emoji string) error
	Delete(ctx context.Context, channel, messageID string) error
	// HasThreadReplies reports whether anyone has replied in the message's
	// thread — the discussion a Delete would take with it.
	HasThreadReplies(ctx context.Context, channel, messageID string) (bool, error)
}

// MessageStore persists tracked PRs and their per-channel chat messages.
type MessageStore interface {
	AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error
	Messages(ctx context.Context, repository string, prNumber int) ([]Message, error)
	Touch(ctx context.Context, repository string, prNumber int) error
	MarkClosed(ctx context.Context, repository string, prNumber int) error
	Delete(ctx context.Context, repository string, prNumber int) error
}

// RepoBehavior resolves a repository's per-repo behavioral config (reactions,
// review flags). Close/draft/reaction handlers need it but not the per-channel
// targets.
type RepoBehavior interface {
	Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
}

// TargetResolver resolves the open fan-out: per-repo behavior plus the
// per-channel targets a newly opened PR is announced to.
type TargetResolver interface {
	ResolveTargets(ctx context.Context, repository string, prNumber int) (routingdomain.RepoMapping, []routingdomain.Target, error)
}

// ReviewSessions is the review-session view the reaction and close handlers use.
// The review domain satisfies it in a later phase; the code-reviews store
// satisfies it today (via an infra adapter). GetActive returns ErrNoActiveReview
// when the PR has no in-progress session.
type ReviewSessions interface {
	GetActive(ctx context.Context, repository string, prNumber int) (ReviewSession, error)
	Finish(ctx context.Context, repository string, prNumber int) error
	Reviewers(ctx context.Context, repository string, prNumber int) ([]ReviewSession, error)
}

// ScheduledQueue holds the new PRs of repos on the scheduled notification
// strategy until a tick posts them. Enqueue tracks the PR (so close and draft
// events find it) and is idempotent per (PR, channel), also after the send: a
// webhook redelivery never lists a PR twice in a channel. Pending returns the
// unsent entries of open PRs, oldest first.
type ScheduledQueue interface {
	Enqueue(ctx context.Context, entry ScheduledEntry) error
	Pending(ctx context.Context) ([]ScheduledEntry, error)
	MarkSent(ctx context.Context, ids []uint, sentAt time.Time) error
}

// ScheduledPoster posts one channel's list of queued PRs. The list message
// holds many PRs, so it is never stored, updated, reacted on, or deleted.
type ScheduledPoster interface {
	PostScheduledList(ctx context.Context, channel string, req ScheduledListRequest) error
}

// ScheduleCalendar answers whether a tick falls on a day the team is off, and
// names the reason (weekend, holiday). A skipped tick keeps the queue.
type ScheduleCalendar interface {
	SkipReason(now time.Time) (reason string, skip bool)
}

// ScheduledSender is the scheduled-strategy send use case: for one cron spec
// it posts, per channel, one list of the queued PRs whose repo sends on that
// spec, then marks them sent. A channel whose post fails keeps its entries for
// the next tick.
type ScheduledSender interface {
	SendSchedule(ctx context.Context, spec string) error
}

// ScheduledRunner fires the ScheduledSender on every configured spec until ctx
// is cancelled, skipping the days the calendar marks off.
type ScheduledRunner interface {
	Run(ctx context.Context) error
}

// Handler is implemented by each PR-lifecycle use case. Applicable inspects an
// event and returns true if Handle should run; the dispatcher runs the first
// applicable handler and skips the rest (handlers are mutually exclusive).
type Handler interface {
	Applicable(event kernel.Event) bool
	Handle(ctx context.Context, event kernel.Event) error
}

// EventDispatcher routes an incoming event to the first applicable handler.
type EventDispatcher interface {
	Dispatch(ctx context.Context, event kernel.Event) error
}
