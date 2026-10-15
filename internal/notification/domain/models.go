package domain

import (
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
)

// OpenHandlerParams are the OpenHandler's dependencies. Queue receives the PRs
// of repos on the scheduled notification strategy.
type OpenHandlerParams struct {
	Store     MessageStore
	Resolver  TargetResolver
	Messenger Messenger
	Queue     ScheduledQueue
	Logger    *slog.Logger
}

// ScheduledEntry is one queued PR announcement for one channel. PR carries the
// fields the list shows: Number, Title, URL, Author.
type ScheduledEntry struct {
	ID         uint
	Repository string
	PR         kernel.PR
	Channel    string
	Mentions   []string
	QueuedAt   time.Time
}

// ScheduledListRequest is the intent to post one channel's list of queued PRs,
// pinging Mentions once.
type ScheduledListRequest struct {
	Mentions []string
	Entries  []ScheduledEntry
}

// ScheduledSenderParams are the ScheduledSender's dependencies. Behavior gives
// each repo's current schedule; Now stamps the sent entries (nil = time.Now).
type ScheduledSenderParams struct {
	Queue    ScheduledQueue
	Behavior RepoBehavior
	Poster   ScheduledPoster
	Logger   *slog.Logger
	Now      func() time.Time
}

// ScheduledRunnerParams are the ScheduledRunner's dependencies. Specs are
// standard 5-field cron specs read in TZ (nil = UTC); Calendar may be nil.
type ScheduledRunnerParams struct {
	Specs    []string
	Sender   ScheduledSender
	Calendar ScheduleCalendar
	TZ       *time.Location
	Logger   *slog.Logger
	Now      func() time.Time
}

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
	Repository string
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
