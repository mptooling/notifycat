package domain

import "github.com/mptooling/notifycat/internal/kernel"

// Message is one posted chat message for a PR: the channel it lives in and the
// platform's id for the post. Mapped from the store's persistence model at the
// repository boundary.
type Message struct {
	Channel   string
	MessageID string
}

// OpenRequest is the intent to post an opened-PR notification. Bot, when
// non-nil, selects the compact dependency-bot template; otherwise the standard
// template is rendered with NewPREmoji.
type OpenRequest struct {
	PR         kernel.PR
	Mentions   []string
	NewPREmoji string
	Bot        *BotFormat
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
	PR          kernel.PR
	Merged      bool
	Emoji       string
	ReviewerIDs []string
}

// ReviewFinishedRequest is the intent to refresh a message after its active
// review session finishes: the standard message is rebuilt and, when reviewers
// exist, a "reviewed by" marker is appended.
type ReviewFinishedRequest struct {
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
