package domain

import (
	"log/slog"
	"time"
)

// PullRequest is the digest's view of one tracked PR: the fields the reporter
// reads to build a reminder. It is mapped from the store's persistence model at
// the repository boundary, so no gorm-tagged type crosses a port.
type PullRequest struct {
	Repository string
	PRNumber   int
	UpdatedAt  time.Time
	Messages   []MessageRef
}

// MessageRef is one posted message for a PR: the channel it lives in and the
// messenger's id for the post.
type MessageRef struct {
	Channel   string
	MessageID string
}

// StuckPR is one line of a channel's digest list: the PR, its web URL, and how
// many whole days it has sat idle.
type StuckPR struct {
	Repository string
	Number     int
	URL        string
	IdleDays   int
}

// Message is a presentation-neutral rendered message that crosses the composer
// and poster ports. The application treats it as opaque — it only carries a
// composer's output to the poster; the Slack adapter maps it to and from the
// platform message type at the boundary.
type Message struct {
	Blocks   []Block
	Fallback string
}

// Block is one rendered block of a Message. The digest emits only section
// blocks (Type == BlockTypeSection, Text set).
type Block struct {
	Type string
	Text *TextObject
}

// TextObject is a block's text payload.
type TextObject struct {
	Type string
	Text string
}

// ReporterParams bundles everything the stuck-PR reporter needs. Now supplies
// the clock (time.Now in production, a fixed clock in tests); TZ is the timezone
// the "start of day" cutoff is computed in (nil defaults to UTC) and must match
// the scheduler's timezone so a digest fires and cuts off in the same zone.
type ReporterParams struct {
	Finder   StuckFinder
	Mappings MappingLookup
	Poster   DigestPoster
	Composer DigestComposer
	Digests  DigestResolver
	Logger   *slog.Logger
	TZ       *time.Location
	Now      func() time.Time
}

// SchedulerParams bundles everything the digest scheduler needs. Specs is a list
// of standard 5-field cron expressions; TZ is the timezone every spec is
// interpreted in (nil defaults to UTC).
type SchedulerParams struct {
	Specs  []string
	Job    ScheduleJob
	Logger *slog.Logger
	TZ     *time.Location
}
