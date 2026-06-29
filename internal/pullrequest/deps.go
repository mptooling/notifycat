package pullrequest

import (
	"context"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// The interfaces below are the narrow views of external dependencies that
// the handlers need. They live with the consumers so tests can supply small
// hand-rolled fakes without pulling in GORM or making live HTTP calls.

// SlackMessages reads and writes the per-PR Slack message TS record.
type SlackMessages interface {
	Save(ctx context.Context, m store.SlackMessage) error
	Get(ctx context.Context, repository string, prNumber int) (store.SlackMessage, error)
	Delete(ctx context.Context, repository string, prNumber int) error
	// Touch records review/comment activity (bumps updated_at) so the stuck-PR
	// digest can tell idle PRs from active ones.
	Touch(ctx context.Context, repository string, prNumber int) error
	// MarkClosed records that the PR is merged/closed so the digest skips it.
	MarkClosed(ctx context.Context, repository string, prNumber int) error
}

// Messenger is the subset of a chat messenger the handlers use. Slack is the
// only implementation today (slack.Client satisfies it).
type Messenger interface {
	PostMessage(ctx context.Context, channel string, msg slack.Message) (messageID string, err error)
	UpdateMessage(ctx context.Context, channel, messageID string, msg slack.Message) error
	DeleteMessage(ctx context.Context, channel, messageID string) error
	AddReaction(ctx context.Context, channel, messageID, name string) error
}

// PullRequestStore persists tracked PRs and their per-channel messages.
type PullRequestStore interface {
	AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error
	Messages(ctx context.Context, repository string, prNumber int) ([]store.Message, error)
	Touch(ctx context.Context, repository string, prNumber int) error
	MarkClosed(ctx context.Context, repository string, prNumber int) error
	Delete(ctx context.Context, repository string, prNumber int) error
}

// RepoBehavior resolves a repository's per-repo behavioral config (reactions,
// review flags). Close/draft/review need it but not the per-channel targets.
type RepoBehavior interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// TargetResolver resolves the open fan-out: per-repo behavior + per-channel targets.
type TargetResolver interface {
	ResolveTargets(ctx context.Context, repository string, prNumber int) (store.RepoMapping, []store.Target, error)
}
