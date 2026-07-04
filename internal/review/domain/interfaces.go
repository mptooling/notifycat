package domain

import (
	"context"
	"time"
)

// Recorder records and inspects per-PR review sessions.
type Recorder interface {
	// HasActiveReview reports whether the user already has an in-progress review
	// on the PR (a repeat click is a no-op).
	HasActiveReview(ctx context.Context, repository string, prNumber int, userID string) (bool, error)
	// Start records the user as a reviewer. It returns ErrActiveReviewExists when
	// a concurrent click already recorded them.
	Start(ctx context.Context, repository string, prNumber int, userID, userName string) error
}

// MessageChecker confirms notifycat owns a message for the PR before acting on a
// click.
type MessageChecker interface {
	HasMessages(ctx context.Context, repository string, prNumber int) (bool, error)
}

// MessageDecorator edits the PR message in place, appending an in-review marker
// for the reviewer while passing the original blocks through untouched.
type MessageDecorator interface {
	AppendReviewingMarker(ctx context.Context, message MessageRef, reviewer Reviewer, since time.Time) error
}

// StartReview records a reviewer and decorates the PR message on a verified
// "Start review" click. A duplicate click (same user) is a no-op.
type StartReview interface {
	Handle(ctx context.Context, command StartReviewCommand) error
}
