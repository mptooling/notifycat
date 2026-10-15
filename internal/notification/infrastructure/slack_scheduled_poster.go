package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

// SlackScheduledPoster implements the notification ScheduledPoster port over
// the Slack client and composer.
type SlackScheduledPoster struct {
	client   *slack.Client
	composer *slack.Composer
}

// NewSlackScheduledPoster wraps the platform Slack client and composer.
func NewSlackScheduledPoster(client *slack.Client, composer *slack.Composer) *SlackScheduledPoster {
	return &SlackScheduledPoster{client: client, composer: composer}
}

// PostScheduledList implements domain.ScheduledPoster.
func (p *SlackScheduledPoster) PostScheduledList(ctx context.Context, channel string, req domain.ScheduledListRequest) error {
	_, err := p.client.PostMessage(ctx, channel, p.composeList(req))
	return err
}

func (p *SlackScheduledPoster) composeList(req domain.ScheduledListRequest) slack.Message {
	prs := make([]slack.PRDetails, 0, len(req.Entries))
	for _, entry := range req.Entries {
		prs = append(prs, prDetails(entry.Repository, entry.PR))
	}
	return p.composer.ScheduledList(req.Mentions, prs)
}

var _ domain.ScheduledPoster = (*SlackScheduledPoster)(nil)
