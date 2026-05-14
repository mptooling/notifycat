package pullrequest

import (
	"context"

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
}

// RepoMappings reads the per-repository routing to a Slack channel.
type RepoMappings interface {
	Get(ctx context.Context, repository string) (store.RepoMapping, error)
}

// SlackClient is the subset of the slack package's client that handlers use.
type SlackClient interface {
	PostMessage(ctx context.Context, channel, text string) (ts string, err error)
	UpdateMessage(ctx context.Context, channel, ts, text string) error
	DeleteMessage(ctx context.Context, channel, ts string) error
	AddReaction(ctx context.Context, channel, ts, name string) error
}
