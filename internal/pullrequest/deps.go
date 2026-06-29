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
