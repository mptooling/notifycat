package domain

import (
	"context"
	"time"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// StuckFinder returns open PRs (with their messages) idle since before cutoff.
type StuckFinder interface {
	FindStuck(ctx context.Context, cutoff time.Time) ([]PullRequest, error)
}

// MappingLookup resolves a repository to its Slack channel and mentions.
type MappingLookup interface {
	Get(ctx context.Context, repository string) (routingdomain.RepoMapping, error)
}

// DigestResolver looks up the effective digest configuration for a repository.
type DigestResolver interface {
	DigestFor(repository string) routingdomain.DigestConfig
}

// DigestComposer renders the two messages of a channel's stuck-PR digest: a
// parent headline carrying the mentions and PR count, and a threaded list of
// one line per stuck PR. Implementations produce a presentation-neutral Message
// the poster delivers.
type DigestComposer interface {
	StuckDigestParent(mentions []string, count int) Message
	StuckDigestList(prs []StuckPR) Message
}

// DigestPoster posts a composed message to a channel, either as a top-level post
// (PostMessage, returning its id) or threaded under one (PostReply).
type DigestPoster interface {
	PostMessage(ctx context.Context, channel string, msg Message) (string, error)
	PostReply(ctx context.Context, channel, threadTS string, msg Message) (string, error)
}

// ScheduleJob is the unit the scheduler fires on each cron tick; the reporter
// satisfies it. spec is the cron expression that fired, so the job can include
// only the repos scheduled at that spec.
type ScheduleJob interface {
	ReportSchedule(ctx context.Context, spec string) error
}

// DigestReporter runs one stuck-PR digest pass: find open PRs idle since the
// start of today, group them by channel, and post one reminder per channel.
// Report includes every enabled repo; ReportSchedule includes only repos whose
// effective digest is enabled and scheduled at spec.
type DigestReporter interface {
	Report(ctx context.Context) error
	ReportSchedule(ctx context.Context, spec string) error
}

// DigestScheduler runs one cron per distinct schedule spec, calling its job for
// each tick, and blocks until its context is cancelled.
type DigestScheduler interface {
	Run(ctx context.Context) error
}
