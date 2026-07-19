package domain

import (
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	saliencedomain "github.com/mptooling/notifycat/internal/salience/domain"
)

// Message is one posted chat message for a PR: the channel it lives in and the
// platform's id for the post. Mapped from the store's persistence model at the
// repository boundary.
type Message struct {
	Channel   string
	MessageID string
}

// OpenRequest is the intent to post an opened-PR notification. Bot, when
// non-nil, selects the compact dependency-bot template (a policy decision the
// advisor never sees); otherwise the salience decision fields select the
// template: Compact picks the one-line format, Breaking prepends the breaking
// label, ContextBlock appends one muted line. Zero decision fields render the
// standard template byte-identically to pre-salience notifycat.
type OpenRequest struct {
	Repository   string
	PR           kernel.PR
	Mentions     []string
	NewPREmoji   string
	Bot          *BotFormat
	Compact      bool
	Breaking     bool
	ContextBlock string
}

// BotFormat carries the dependency-bot template inputs: the bot's display name
// and whether the PR body reads as a security advisory.
type BotFormat struct {
	Name     string
	Security bool
}

// ClosedRequest is the intent to update a message for a closed/merged PR. Emoji
// is the merged/closed reaction; ReviewerIDs, when non-empty, appends a
// "reviewed by" marker.
type ClosedRequest struct {
	Repository  string
	PR          kernel.PR
	Merged      bool
	Emoji       string
	ReviewerIDs []string
}

// ReviewFinishedRequest is the intent to refresh a message after its active
// review session finishes: the standard message is rebuilt and, when reviewers
// exist, a "reviewed by" marker is appended.
type ReviewFinishedRequest struct {
	Repository  string
	PR          kernel.PR
	ReviewerIDs []string
	NewPREmoji  string
}

// ReviewSession is the notification view of one review session: who is (or was)
// reviewing. Mapped from the review store at the boundary.
type ReviewSession struct {
	SlackUserID   string
	SlackUserName string
}

// OpenHandlerParams bundles the open handler's dependencies.
type OpenHandlerParams struct {
	Store     MessageStore
	Resolver  TargetResolver
	Messenger Messenger
	Advisor   saliencedomain.Advisor
	Logger    *slog.Logger
}

// LifecycleHandlerParams bundles the dependencies shared by the close and
// review-reaction handlers.
type LifecycleHandlerParams struct {
	Store     MessageStore
	Behavior  RepoBehavior
	Messenger Messenger
	Advisor   saliencedomain.Advisor
	Logger    *slog.Logger
	Reviews   ReviewSessions
}
