package domain

import (
	"context"

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
